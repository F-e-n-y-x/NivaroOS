package jobs

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// The owner's side of phone backups (web UI "Phones"), the device
// config/settings routes, retention and the stale warning.

// device-changed "change" values.
const (
	DeviceChangeSessionStarted = "session_started"
	DeviceChangeSessionEnded   = "session_ended"
	DeviceChangeSettings       = "settings"
	DeviceChangePhoneSettings  = "phone_settings"
	DeviceChangeDestination    = "destination"
	DeviceChangeMoved          = "moved"
	DeviceChangeMoveFailed     = "move_failed"
	DeviceChangeRevoked        = "revoked"
	DeviceChangeRelinked       = "relinked"
	DeviceChangeRemoved        = "removed"
	DeviceChangeVerified       = "verified"
	DeviceChangeFilesRemoved   = "files_removed"
	DeviceChangeImported       = "imported"
	DeviceChangeEnrolled       = "enrolled"
)

func (s *Service) deviceChanged(id, change string) {
	s.pub.publish(EventDeviceChanged, map[string]string{PropDeviceID: id, PropChange: change})
}

// pfail is fail plus the phone-side errors.
func (s *Service) pfail(w http.ResponseWriter, err error) {
	var fe *fieldError
	switch {
	case errors.As(err, &fe):
		writeValidation(w, map[string]string{fe.field: fe.code})
	case errors.Is(err, errNoFolder):
		writeError(w, http.StatusNotFound, ErrorBody{ErrorCode: ErrNotFound, Detail: "nothing is stored for this phone yet"})
	default:
		s.fail(w, err)
	}
}

// adminDevice loads the device of an admin route.
func (s *Service) adminDevice(w http.ResponseWriter, r *http.Request) (DeviceRow, bool) {
	if !requireAdmin(w, r) {
		return DeviceRow{}, false
	}
	d, err := s.store.GetDevice(r.PathValue("id"))
	if err != nil {
		s.pfail(w, err)
		return d, false
	}
	return d, true
}

// deviceView is the list/detail view of a device, with its backup
// summary.
func (s *Service) deviceView(d DeviceRow) Device {
	out := d.toDevice()
	var snap PhoneSnapshotRow
	if s.store.db.Where("device_id = ?", d.ID).Order("taken_at DESC").Take(&snap).Error == nil {
		t := snap.TakenAt
		out.LastBackupAt = &t
	}
	var run RunRow
	if s.store.db.Where("job_id = ? AND kind = ?", d.ID, string(KindDevice)).Order("id DESC").Take(&run).Error == nil {
		out.LastStatus = RunStatus(run.Status)
	}
	out.SizeBytes = s.deviceSize(d.ID, "")
	return out
}

func (s *Service) deviceSize(id string, cat PhoneCategory) int64 {
	var total int64
	for _, m := range []interface{}{&PhoneFileRow{}, &PhoneVersionRow{}, &PhoneExportRow{}} {
		var n struct{ N int64 }
		q := s.store.db.Model(m).Select("COALESCE(SUM(size), 0) AS n").Where("device_id = ?", id)
		if cat != "" {
			q = q.Where("category = ?", string(cat))
		}
		if q.Scan(&n).Error == nil {
			total += n.N
		}
	}
	return total
}

// ---------------------------------------------------------------------
// Device routes: config and the phone's own settings

func (s *Service) phoneLimits() PhoneLimits {
	return PhoneLimits{
		ChunkSize: PhoneChunkSize, MaxUploadSize: PhoneMaxUploadSize, CheckBatch: PhoneCheckBatch, DeletedBatch: PhoneDeletedBatch,
		ItemKeysBatch: PhoneItemKeysBatch, MaxOpenUploads: PhoneMaxOpenUploads, MaxOpenSessions: PhoneMaxOpenSessions,
		MaxFinishErrors: PhoneMaxFinishErrors, SessionIdleSec: int(PhoneSessionIdle / time.Second),
		UploadExpirySec: int(PhoneUploadExpiry / time.Second), CheckPerMinute: PhoneCheckPerMinute,
	}
}

func phoneCategoryInfos() []PhoneCategoryInfo {
	out := make([]PhoneCategoryInfo, 0, len(PhoneCategories))
	for _, c := range PhoneCategories {
		i := c.info()
		out = append(out, PhoneCategoryInfo{Category: c, Kind: i.kind, Incremental: i.incremental, Named: i.named, Extension: i.ext})
	}
	return out
}

func (s *Service) handleDeviceConfig(w http.ResponseWriter, r *http.Request) {
	d, ok := s.readDevice(w, r)
	if !ok {
		return
	}
	var ex int64
	s.store.db.Model(&PhoneExcludedRow{}).Where("device_id = ?", d.ID).Count(&ex)
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	writeOK(w, http.StatusOK, DeviceConfig{
		Device: s.deviceView(d), Destination: s.destStatus(ctx, &d), Settings: d.settings(), Limits: s.phoneLimits(),
		Categories: phoneCategoryInfos(), Excluded: ex, ServerTime: s.now().UTC().Truncate(time.Second),
	})
}

func (s *Service) handleDevicePhoneSettings(w http.ResponseWriter, r *http.Request) {
	d := deviceOf(r)
	var req PhoneSettings
	if !decode(w, r, &req) {
		return
	}
	fe := fieldErrors{}
	for c, cs := range req.Categories {
		if !c.valid() {
			fe.add("categories", FieldInvalid)
		}
		if len(cs.Schedule) > 128 || len(cs.Conditions) > 8 {
			fe.add("categories", FieldInvalid)
		}
	}
	if len(req.AppVersion) > 64 || len(req.OSVersion) > 64 || len(req.Model) > 128 {
		fe.add("app_version", FieldInvalid)
	}
	if len(fe) > 0 {
		writeValidation(w, fe)
		return
	}
	now := s.now().UTC().Truncate(time.Second)
	req.ReportedAt = &now
	if req.Categories == nil {
		req.Categories = map[PhoneCategory]PhoneCategorySettings{}
	}
	if err := s.updateDevice(d.ID, map[string]interface{}{"phone_settings": mustJSON(req)}); err != nil {
		s.pfail(w, err)
		return
	}
	s.deviceChanged(d.ID, DeviceChangePhoneSettings)
	writeOK(w, http.StatusOK, req)
}

// ---------------------------------------------------------------------
// Owner routes

func (s *Service) deviceDetail(ctx context.Context, d DeviceRow) (DeviceDetail, error) {
	out := DeviceDetail{
		Device: s.deviceView(d), Destination: s.destStatus(ctx, &d), Settings: d.settings(), Phone: d.phoneSettings(),
		Categories: []DeviceCategoryStatus{}, OpenSessions: []PhoneSession{}, LastVerify: lastVerify(d),
	}
	var snaps []PhoneSnapshotRow
	if err := s.store.db.Where("device_id = ?", d.ID).Order("taken_at DESC").Find(&snaps).Error; err != nil {
		return out, err
	}
	out.Snapshots = int64(len(snaps))
	var sessions []PhoneSessionRow
	if err := s.store.db.Where("device_id = ?", d.ID).Order("started_at DESC").Limit(200).Find(&sessions).Error; err != nil {
		return out, err
	}
	for _, ses := range sessions {
		if ses.Status == SessionOpen {
			out.OpenSessions = append(out.OpenSessions, ses.toAPI())
		}
	}
	runStatus := map[string]RunStatus{}
	if len(sessions) > 0 {
		ids := make([]string, 0, len(sessions))
		for _, ses := range sessions {
			ids = append(ids, ses.RunID)
		}
		var runs []RunRow
		s.store.db.Select("id", "status").Where("id IN ?", ids).Find(&runs)
		for _, r := range runs {
			runStatus[r.ID] = RunStatus(r.Status)
		}
	}
	for _, c := range PhoneCategories {
		st := DeviceCategoryStatus{Category: c, Kind: c.info().kind, SizeBytes: s.deviceSize(d.ID, c)}
		for _, snap := range snaps {
			if hasCat(splitCats(snap.Categories), c) {
				t := snap.TakenAt
				st.LastBackupAt = &t
				break
			}
		}
		for _, ses := range sessions {
			if ses.Status != SessionOpen && hasCat(splitCats(ses.Categories), c) {
				st.LastStatus, st.LastRunID = runStatus[ses.RunID], ses.RunID
				break
			}
		}
		if c.isFiles() {
			s.store.db.Model(&PhoneFileRow{}).Where("device_id = ? AND category = ? AND deleted_on_device_at IS NULL", d.ID, string(c)).Count(&st.Files)
			s.store.db.Model(&PhoneFileRow{}).Where("device_id = ? AND category = ? AND deleted_on_device_at IS NOT NULL", d.ID, string(c)).Count(&st.DeletedFiles)
		} else {
			s.store.db.Model(&PhoneExportRow{}).Where("device_id = ? AND category = ?", d.ID, string(c)).Count(&st.Exports)
			if c.info().incremental {
				s.store.db.Model(&PhoneItemRow{}).Where("device_id = ? AND category = ?", d.ID, string(c)).Count(&st.Items)
			} else {
				var e PhoneExportRow
				if s.store.db.Where("device_id = ? AND category = ?", d.ID, string(c)).Order("taken_at DESC").Take(&e).Error == nil {
					st.Items = e.Items
				}
			}
		}
		out.Categories = append(out.Categories, st)
	}
	s.store.db.Model(&PhoneExcludedRow{}).Where("device_id = ?", d.ID).Count(&out.Excluded)
	switch d.MoveState {
	case MoveStateMoving:
		s.phone.mu.Lock()
		if mv := s.phone.moves[d.ID]; mv != nil {
			cp := *mv
			out.Move = &cp
		}
		s.phone.mu.Unlock()
		if out.Move == nil {
			out.Move = &DeviceMove{State: MoveStateMoving, TargetPath: d.MoveTargetPath}
		}
	case MoveStateFailed:
		mv := &DeviceMove{State: MoveStateFailed, TargetPath: d.MoveTargetPath, Error: d.MoveError}
		if d.MoveStartedAt != nil {
			mv.StartedAt = *d.MoveStartedAt
		}
		if d.MoveTarget != "" {
			if ep, ok := (DeviceRow{Dest: d.MoveTarget}).destEndpoint(); ok {
				mv.Target = &ep
			}
		}
		out.Move = mv
	}
	return out, nil
}

func (s *Service) handleDeviceGet(w http.ResponseWriter, r *http.Request) {
	d, ok := s.adminDevice(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	det, err := s.deviceDetail(ctx, d)
	if err != nil {
		s.pfail(w, err)
		return
	}
	writeOK(w, http.StatusOK, det)
}

func validDeviceSettings(st DeviceSettings, fe fieldErrors) {
	if st.KeepLast < 1 || st.KeepLast > 1000 {
		fe.add("settings.keep_last", FieldOutOfRange)
	}
	if st.KeepDays < 0 || st.KeepDays > 3650 {
		fe.add("settings.keep_days", FieldOutOfRange)
	}
	if st.DeletedPurgeDays < 0 || st.DeletedPurgeDays > 3650 {
		fe.add("settings.deleted_purge_days", FieldOutOfRange)
	}
	if st.StaleDays < 0 || st.StaleDays > 365 {
		fe.add("settings.stale_days", FieldOutOfRange)
	}
}

func (s *Service) handleDeviceUpdate(w http.ResponseWriter, r *http.Request) {
	d, ok := s.adminDevice(w, r)
	if !ok || s.rateLimited(w, r) {
		return
	}
	var req DeviceUpdateRequest
	if !decode(w, r, &req) {
		return
	}
	fe := fieldErrors{}
	var name string
	if req.Name != nil {
		n, ok := validDeviceName(*req.Name)
		if !ok {
			fe.add("name", FieldInvalid)
		}
		name = n
	}
	if req.Settings != nil {
		validDeviceSettings(*req.Settings, fe)
	}
	if len(fe) > 0 {
		writeValidation(w, fe)
		return
	}
	if req.Settings != nil {
		if err := s.updateDevice(d.ID, map[string]interface{}{"settings": mustJSON(*req.Settings)}); err != nil {
			s.pfail(w, err)
			return
		}
		s.deviceChanged(d.ID, DeviceChangeSettings)
	}
	if req.Name != nil && name != d.Name {
		if err := s.renameDevice(r.Context(), d, name); err != nil {
			s.pfail(w, err)
			return
		}
		s.deviceChanged(d.ID, DeviceChangeSettings)
	}
	s.audit(r, "device_update", "", "device="+d.ID)
	d, _ = s.store.GetDevice(d.ID)
	s.pruneDevice(d.ID)
	det, err := s.deviceDetail(r.Context(), d)
	if err != nil {
		s.pfail(w, err)
		return
	}
	writeOK(w, http.StatusOK, det)
}

// renameDevice renames a phone and its folder (in the same location).
func (s *Service) renameDevice(ctx context.Context, d DeviceRow, name string) error {
	lock := s.phone.devLock(d.ID)
	lock.Lock()
	defer lock.Unlock()
	if n, err := s.openSessionCount(d.ID); err != nil {
		return err
	} else if n > 0 {
		return errConflict("%s is backing up right now; rename it when it has finished", d.Name)
	}
	if d.MoveState == MoveStateMoving {
		return errConflict("the backups of %s are being moved", d.Name)
	}
	rows, err := s.store.ListDevices()
	if err != nil {
		return err
	}
	var taken []string
	for _, o := range rows {
		if o.ID != d.ID {
			taken = append(taken, o.Folder)
		}
	}
	stem := deviceFolderStem(name)
	if stem == "" {
		stem = d.ID
	}
	folder := uniqueDeviceFolder(stem, taken)
	cols := map[string]interface{}{"name": name}
	if folder != d.Folder {
		root, err := s.phoneRoot(ctx, &d, false)
		has, herr := s.deviceHasData(d.ID)
		if herr != nil {
			return herr
		}
		switch {
		case err == nil:
			newRoot := filepath.Join(filepath.Dir(root), folder)
			if _, err := os.Lstat(newRoot); err == nil {
				return errConflict("a folder %s is in the way", newRoot)
			}
			if err := os.Rename(root, newRoot); err != nil {
				return engine.Errorf(ErrIOError, "renaming %s: %v", root, err)
			}
			cols["dest_path"] = newRoot
		case errors.Is(err, errNoFolder):
		case has:
			return err // offline with backups: the folder can't follow
		}
		cols["folder"] = folder
		cols["default_dest"] = path.Join(s.deviceRoot(), folder)
	}
	if err := s.updateDevice(d.ID, cols); err != nil {
		return err
	}
	s.phone.forgetRoot(d.ID)
	return nil
}

func (s *Service) handleDeviceDestination(w http.ResponseWriter, r *http.Request) {
	d, ok := s.adminDevice(w, r)
	if !ok || s.rateLimited(w, r) {
		return
	}
	var req DeviceDestRequest
	if !decode(w, r, &req) {
		return
	}
	fe := fieldErrors{}
	if req.Location != nil {
		normalizeEndpoint(req.Location, "location", fe)
		if !localKind(req.Location.Kind) {
			fe.add("location.kind", FieldInvalid)
		}
	}
	if req.Mode != "" && req.Mode != DestModeMove && req.Mode != DestModeFresh {
		fe.add("mode", FieldInvalid)
	}
	if len(fe) > 0 {
		writeValidation(w, fe)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	nd, err := s.changeDest(ctx, d, req.Location, req.Mode)
	if err != nil {
		s.pfail(w, err)
		return
	}
	loc := "default"
	if req.Location != nil {
		loc = fmt.Sprintf("%s:%s/%s", req.Location.Kind, req.Location.RefID, req.Location.SubPath)
	}
	s.audit(r, "device_destination", "", fmt.Sprintf("device=%s location=%s mode=%s", d.ID, loc, req.Mode))
	s.deviceChanged(d.ID, DeviceChangeDestination)
	det, err := s.deviceDetail(ctx, nd)
	if err != nil {
		s.pfail(w, err)
		return
	}
	status := http.StatusOK
	if nd.MoveState == MoveStateMoving {
		status = http.StatusAccepted
	}
	writeOK(w, status, det)
}

// closeSessions ends a device's open sessions as cancelled.
func (s *Service) closeSessions(id string) {
	var open []PhoneSessionRow
	s.store.db.Where("device_id = ? AND status = ?", id, SessionOpen).Find(&open)
	lock := s.phone.devLock(id)
	for _, ses := range open {
		lock.Lock()
		if _, _, err := s.endSession(ses, StatusCancelled, nil); err != nil {
			log.Printf("backup: close session %s: %v", ses.ID, err)
		}
		lock.Unlock()
	}
}

func (s *Service) handleDeviceRevokeToken(w http.ResponseWriter, r *http.Request) {
	d, ok := s.adminDevice(w, r)
	if !ok {
		return
	}
	now := s.now().UTC().Truncate(time.Second)
	if err := s.updateDevice(d.ID, map[string]interface{}{
		"token_hash": "", "old_token_hash": "", "old_valid_until": nil, "rotation_nonce": "", "revoked_at": &now,
	}); err != nil {
		s.pfail(w, err)
		return
	}
	s.closeSessions(d.ID)
	s.audit(r, "device_revoke", "", "device="+d.ID)
	s.deviceChanged(d.ID, DeviceChangeRevoked)
	d, _ = s.store.GetDevice(d.ID)
	writeOK(w, http.StatusOK, s.deviceView(d))
}

func (s *Service) handleDeviceNewToken(w http.ResponseWriter, r *http.Request) {
	d, ok := s.adminDevice(w, r)
	if !ok || s.rateLimited(w, r) {
		return
	}
	tok, hash := newDeviceToken()
	now := s.now().UTC().Truncate(time.Second)
	if err := s.updateDevice(d.ID, map[string]interface{}{
		"token_hash": hash, "token_issued_at": now, "token_used": false, "old_token_hash": "", "old_valid_until": nil,
		"rotation_nonce": "", "revoked_at": nil,
	}); err != nil {
		s.pfail(w, err)
		return
	}
	s.audit(r, "device_token", "", "device="+d.ID)
	s.deviceChanged(d.ID, DeviceChangeRelinked)
	d, _ = s.store.GetDevice(d.ID)
	writeOK(w, http.StatusOK, DeviceEnrollment{Device: s.deviceView(d), Token: tok})
}

// handleDeviceRemove is DELETE /devices/:id: the device, its credential
// and its index go; with purge_data=1 its backup folder too (refused
// while its drive is missing).
func (s *Service) handleDeviceRemove(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	id := r.PathValue("id")
	d, err := s.store.GetDevice(id)
	if err != nil {
		s.pfail(w, err)
		return
	}
	purge := parseBoolQ(r.URL.Query().Get("purge_data"))
	if d.MoveState == MoveStateMoving {
		s.pfail(w, errConflict("the backups of %s are being moved", d.Name))
		return
	}
	s.closeSessions(id)
	lock := s.phone.devLock(id)
	lock.Lock()
	defer lock.Unlock()
	if purge {
		root, err := s.phoneRoot(r.Context(), &d, false)
		switch {
		case err == nil:
			if err := s.checkMarker(root, d, false); err != nil {
				s.pfail(w, err)
				return
			}
			if err := os.RemoveAll(root); err != nil {
				s.pfail(w, engine.Errorf(ErrIOError, "removing %s: %v", root, err))
				return
			}
		case errors.Is(err, errNoFolder):
		default:
			s.pfail(w, err)
			return
		}
	}
	if err := s.store.db.Transaction(func(tx *gorm.DB) error { return dropPhoneIndex(tx, id, true) }); err != nil {
		s.pfail(w, err)
		return
	}
	if err := s.store.DeleteDevice(id); err != nil {
		s.pfail(w, err)
		return
	}
	s.phone.forgetRoot(id)
	s.audit(r, "device_remove", "", fmt.Sprintf("device=%s purge_data=%v", id, purge))
	s.deviceChanged(id, DeviceChangeRemoved)
	writeOK(w, http.StatusOK, struct{}{})
}

func (s *Service) handleDeviceExcludedUpdate(w http.ResponseWriter, r *http.Request) {
	d, ok := s.adminDevice(w, r)
	if !ok {
		return
	}
	var req ExcludedUpdate
	if !decode(w, r, &req) {
		return
	}
	fe := fieldErrors{}
	if len(req.Add)+len(req.Remove) > 10000 {
		fe.add("add", FieldTooMany)
	}
	for _, a := range req.Add {
		if !validSHA(strings.ToLower(a.SHA256)) || len(a.Path) > phoneMaxPathBytes {
			fe.add("add", FieldInvalid)
		}
	}
	if len(fe) > 0 {
		writeValidation(w, fe)
		return
	}
	now := s.now().UTC()
	err := s.store.db.Transaction(func(tx *gorm.DB) error {
		for _, a := range req.Add {
			row := PhoneExcludedRow{DeviceID: d.ID, SHA256: strings.ToLower(a.SHA256), Path: a.Path, AddedAt: now}
			if err := tx.Save(&row).Error; err != nil {
				return err
			}
		}
		for _, sha := range req.Remove {
			if err := tx.Where("device_id = ? AND sha256 = ?", d.ID, strings.ToLower(sha)).Delete(&PhoneExcludedRow{}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.pfail(w, err)
		return
	}
	s.deviceChanged(d.ID, DeviceChangeSettings)
	s.handleDeviceExcludedList(w, r)
}

// FilesRemoved answers DELETE /devices/:id/files.
type FilesRemoved struct {
	Files    int64 `json:"files"`
	Versions int64 `json:"versions"`
	Bytes    int64 `json:"bytes"`
}

// handleDeviceFilesDelete removes a file (or a folder) from a phone's
// backup, with its versions, and excludes its contents so the phone
// doesn't send them again.
func (s *Service) handleDeviceFilesDelete(w http.ResponseWriter, r *http.Request) {
	d, ok := s.adminDevice(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	cat := PhoneCategory(q.Get("category"))
	p, okPath := cleanPhonePath(strings.Trim(q.Get("path"), "/"))
	if !cat.valid() || !cat.isFiles() || !okPath {
		writeValidation(w, map[string]string{"path": string(ErrPathNotAllowed)})
		return
	}
	lock := s.phone.devLock(d.ID)
	lock.Lock()
	defer lock.Unlock()
	root, err := s.writableRoot(r.Context(), &d)
	if err != nil {
		s.pfail(w, err)
		return
	}
	prefix := p + "/"
	where := "device_id = ? AND category = ? AND (path = ? OR substr(path, 1, ?) = ?)"
	args := []interface{}{d.ID, string(cat), p, utf8.RuneCountInString(prefix), prefix}
	var files []PhoneFileRow
	var vers []PhoneVersionRow
	if err := s.store.db.Where(where, args...).Find(&files).Error; err != nil {
		s.pfail(w, err)
		return
	}
	if err := s.store.db.Where(where, args...).Find(&vers).Error; err != nil {
		s.pfail(w, err)
		return
	}
	if len(files) == 0 && len(vers) == 0 {
		s.pfail(w, fmt.Errorf("%s/%s: %w", cat, p, errNoRecord))
		return
	}
	out := FilesRemoved{}
	now := s.now().UTC()
	excl := map[string]string{}
	for _, f := range files {
		_ = os.Remove(filepath.Join(root, filepath.FromSlash(f.Stored)))
		out.Files++
		out.Bytes += f.Size
		excl[f.SHA256] = f.Path
	}
	for _, v := range vers {
		_ = os.Remove(filepath.Join(root, filepath.FromSlash(v.Stored)))
		out.Versions++
		out.Bytes += v.Size
		excl[v.SHA256] = v.Path
	}
	err = s.store.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where(where, args...).Delete(&PhoneFileRow{}).Error; err != nil {
			return err
		}
		if err := tx.Where(where, args...).Delete(&PhoneVersionRow{}).Error; err != nil {
			return err
		}
		for sha, pp := range excl {
			if sha == "" {
				continue
			}
			if err := tx.Save(&PhoneExcludedRow{DeviceID: d.ID, SHA256: sha, Path: pp, AddedAt: now}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.pfail(w, err)
		return
	}
	s.audit(r, "device_files_delete", "", fmt.Sprintf("device=%s category=%s path=%q files=%d", d.ID, cat, p, out.Files))
	s.deviceChanged(d.ID, DeviceChangeFilesRemoved)
	writeOK(w, http.StatusOK, out)
}

func (s *Service) handleDeviceDownloadCreate(w http.ResponseWriter, r *http.Request) {
	d, ok := s.adminDevice(w, r)
	if !ok {
		return
	}
	var req DeviceDownloadRequest
	if !decode(w, r, &req) {
		return
	}
	switch {
	case req.ExportID != "":
		if _, err := s.exportRow(d.ID, req.ExportID); err != nil {
			s.pfail(w, err)
			return
		}
	case req.Full:
		if !req.Category.valid() || !req.Category.info().incremental {
			writeValidation(w, map[string]string{"category": string(FieldInvalid)})
			return
		}
	default:
		p, okPath := cleanPhonePath(req.Path)
		if !req.Category.valid() || !req.Category.isFiles() || !okPath {
			writeValidation(w, map[string]string{"path": string(ErrPathNotAllowed)})
			return
		}
		_, _, t, err := s.snapshotAt(d.ID, req.Snapshot)
		if err != nil {
			s.pfail(w, err)
			return
		}
		if _, err := s.itemAt(d.ID, req.Category, p, t); err != nil {
			s.pfail(w, err)
			return
		}
		req.Path = p
	}
	writeOK(w, http.StatusOK, s.downloads.issueDevice(d.ID, req, clientIP(r), s.now()))
}

// serveDeviceDownload is GET /downloads/<token> for a device grant.
func (s *Service) serveDeviceDownload(w http.ResponseWriter, r *http.Request, deviceID string, req DeviceDownloadRequest) {
	d, err := s.store.GetDevice(deviceID)
	if err != nil {
		s.pfail(w, err)
		return
	}
	if req.Full {
		s.serveFull(w, r, d, req.Category, req.Snapshot, true)
		return
	}
	root, err := s.readRoot(r.Context(), &d)
	if err != nil {
		s.pfail(w, err)
		return
	}
	if req.ExportID != "" {
		row, err := s.exportRow(d.ID, req.ExportID)
		if err != nil {
			s.pfail(w, err)
			return
		}
		s.serveStored(w, r, root, row.Stored, path.Base(row.Stored), row.SHA256, PhoneCategory(row.Category).info().contentType, true)
		return
	}
	_, _, t, err := s.snapshotAt(d.ID, req.Snapshot)
	if err != nil {
		s.pfail(w, err)
		return
	}
	it, err := s.itemAt(d.ID, req.Category, req.Path, t)
	if err != nil {
		s.pfail(w, err)
		return
	}
	s.serveStored(w, r, root, it.Stored, path.Base(it.Path), it.SHA256, "", true)
	s.audit(r, "device_download", "", fmt.Sprintf("device=%s category=%s", d.ID, req.Category))
}

// ---------------------------------------------------------------------
// Retention and staleness

// keptSnapshots applies the keep rules to snapshots (newest first).
func keptSnapshots(snaps []PhoneSnapshotRow, st DeviceSettings, now time.Time) map[string]bool {
	kept := map[string]bool{}
	for i, sn := range snaps {
		if i < st.KeepLast {
			kept[sn.ID] = true
		}
	}
	if st.KeepDays > 0 {
		cutoff := now.Add(-time.Duration(st.KeepDays) * 24 * time.Hour)
		days := map[string]bool{}
		for _, sn := range snaps {
			if sn.TakenAt.Before(cutoff) {
				continue
			}
			day := sn.TakenAt.Local().Format("2006-01-02")
			if !days[day] {
				days[day] = true
				kept[sn.ID] = true
			}
		}
	}
	return kept
}

// pruneDevice drops snapshots past the keep rules, then the versions and
// full exports only they referred to, and files deleted on the phone
// longer ago than deleted_purge_days.
func (s *Service) pruneDevice(id string) {
	d, err := s.store.GetDevice(id)
	if err != nil {
		return
	}
	lock := s.phone.devLock(id)
	lock.Lock()
	defer lock.Unlock()
	if n, _ := s.openSessionCount(id); n > 0 {
		return // a session in flight may still refer to what would go
	}
	root, err := s.phoneRoot(s.ctx, &d, false)
	if err != nil {
		return // offline or nothing stored: nothing to prune now
	}
	if err := s.checkRootNow(d, root); err != nil {
		return
	}
	st := d.settings()
	now := s.now().UTC()
	var snaps []PhoneSnapshotRow
	if err := s.store.db.Where("device_id = ?", id).Order("taken_at DESC").Find(&snaps).Error; err != nil {
		return
	}
	kept := keptSnapshots(snaps, st, now)
	var keepTimes []time.Time
	var drop []string
	for _, sn := range snaps {
		if kept[sn.ID] {
			keepTimes = append(keepTimes, sn.TakenAt)
		} else {
			drop = append(drop, sn.ID)
		}
	}
	if len(drop) > 0 {
		s.store.db.Where("id IN ?", drop).Delete(&PhoneSnapshotRow{})
	}
	sort.Slice(keepTimes, func(i, j int) bool { return keepTimes[i].Before(keepTimes[j]) })
	inRange := func(from, until time.Time) bool {
		i := sort.Search(len(keepTimes), func(i int) bool { return !keepTimes[i].Before(from) })
		return i < len(keepTimes) && keepTimes[i].Before(until)
	}
	// Versions: kept while a kept snapshot falls in [Since, Until).
	var vers []PhoneVersionRow
	s.store.db.Where("device_id = ?", id).Find(&vers)
	for _, v := range vers {
		if inRange(v.Since, v.Until) {
			continue
		}
		_ = os.Remove(filepath.Join(root, filepath.FromSlash(v.Stored)))
		s.store.db.Delete(&PhoneVersionRow{}, "id = ?", v.ID)
	}
	// Full exports: kept while in effect at a kept snapshot, or newest.
	var exps []PhoneExportRow
	s.store.db.Where("device_id = ?", id).Order("category, name, taken_at DESC").Find(&exps)
	var prev *PhoneExportRow
	for i := range exps {
		e := exps[i]
		c := PhoneCategory(e.Category)
		newest := prev == nil || prev.Category != e.Category || prev.Name != e.Name
		until := time.Unix(math.MaxInt32, 0)
		if !newest {
			until = prev.TakenAt
		}
		prev = &exps[i]
		if c.info().incremental || newest || inRange(e.TakenAt, until) {
			continue
		}
		_ = os.Remove(filepath.Join(root, filepath.FromSlash(e.Stored)))
		s.store.db.Delete(&PhoneExportRow{}, "id = ?", e.ID)
	}
	// Files deleted on the phone.
	if st.DeletedPurgeDays > 0 {
		cutoff := now.Add(-time.Duration(st.DeletedPurgeDays) * 24 * time.Hour)
		var gone []PhoneFileRow
		s.store.db.Where("device_id = ? AND deleted_on_device_at IS NOT NULL AND deleted_on_device_at < ?", id, cutoff).Find(&gone)
		for _, f := range gone {
			_ = os.Remove(filepath.Join(root, filepath.FromSlash(f.Stored)))
			s.store.db.Where("device_id = ? AND category = ? AND path = ?", f.DeviceID, f.Category, f.Path).Delete(&PhoneFileRow{})
			var fv []PhoneVersionRow
			s.store.db.Where("device_id = ? AND category = ? AND path = ?", f.DeviceID, f.Category, f.Path).Find(&fv)
			for _, v := range fv {
				_ = os.Remove(filepath.Join(root, filepath.FromSlash(v.Stored)))
				s.store.db.Delete(&PhoneVersionRow{}, "id = ?", v.ID)
			}
		}
	}
	removeEmptyDirs(filepath.Join(root, phoneVersionsDir))
}

// removeEmptyDirs removes the empty folders below dir (and dir).
func removeEmptyDirs(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			removeEmptyDirs(filepath.Join(dir, e.Name()))
		}
	}
	_ = os.Remove(dir) // fails while not empty
}

// checkDeviceStale warns, at most once a day per phone, when a phone
// hasn't finished a backup for stale_days.
func (s *Service) checkDeviceStale() {
	rows, err := s.store.ListDevices()
	if err != nil {
		return
	}
	now := s.now().UTC()
	for _, d := range rows {
		st := d.settings()
		if st.StaleDays <= 0 || d.RevokedAt != nil {
			continue
		}
		since := d.CreatedAt
		var snap PhoneSnapshotRow
		if s.store.db.Where("device_id = ?", d.ID).Order("taken_at DESC").Take(&snap).Error == nil {
			since = snap.TakenAt
		} else {
			continue // never backed up: the app's setup, not a lapse
		}
		if now.Sub(since) < time.Duration(st.StaleDays)*24*time.Hour {
			continue
		}
		if d.StaleNotifiedAt != nil && now.Sub(*d.StaleNotifiedAt) < staleRepeat {
			continue
		}
		stamp := now
		if err := s.updateDevice(d.ID, map[string]interface{}{"stale_notified_at": &stamp}); err != nil {
			continue
		}
		days := int(math.Floor(now.Sub(since).Hours() / 24))
		s.notify(Notification{
			Message: Message{Key: "backup.notify.device_stale", Args: map[string]interface{}{"device": d.Name, "days": days}},
			Level:   NotifyLevelWarning, WindowKind: "app",
			WindowProps: map[string]interface{}{"section": "phones", "deviceId": d.ID},
		})
	}
}
