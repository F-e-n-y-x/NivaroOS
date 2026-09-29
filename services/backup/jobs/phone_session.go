package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// Sessions (mobile plan §5.4): the phone opens a session, asks which
// files the server lacks (check), uploads those, reports deletions and
// finishes. Every session is a run of the device's "job" (kind device,
// job_id = device id), so it shows in Activity like any other backup.

// NewSessionID is "ses_" + 16 hex.
func NewSessionID() string { return "ses_" + randomHex(8) }

func (r PhoneSessionRow) toAPI() PhoneSession {
	return PhoneSession{
		ID: r.ID, RunID: r.RunID, Categories: nonNil(splitCats(r.Categories)), Reason: r.Reason, Status: r.Status,
		StartedAt: r.StartedAt, LastActivity: r.LastActivity, EndedAt: r.EndedAt, SnapshotID: r.SnapshotID,
		Counts: PhoneSessionCounts{
			Uploaded: r.Uploaded, Linked: r.Linked, Unchanged: r.Unchanged, Deleted: r.DeletedMark,
			Exports: r.Exports, Bytes: r.Bytes, Errors: r.Errors,
		},
	}
}

// phoneNow is the time stamp of index changes: UTC, strictly after the
// previous one, so "at or before a snapshot" is never ambiguous.
func (s *Service) phoneNow() time.Time {
	s.phone.mu.Lock()
	defer s.phone.mu.Unlock()
	t := s.now().UTC()
	if !t.After(s.phone.last) {
		t = s.phone.last.Add(time.Microsecond)
	}
	t = t.Truncate(time.Microsecond)
	if !t.After(s.phone.last) {
		t = s.phone.last.Add(time.Microsecond)
	}
	s.phone.last = t
	return t
}

// ---------------------------------------------------------------------
// Paths

// cleanPhonePath checks a phone path (relative, slash separated): no
// empty, "." or ".." segment, no NUL, backslash or control character,
// valid UTF-8, at most 4096 bytes and 255 per segment.
func cleanPhonePath(p string) (string, bool) {
	if p == "" || len(p) > phoneMaxPathBytes || !utf8.ValidString(p) || strings.HasPrefix(p, "/") {
		return "", false
	}
	for _, r := range p {
		if r == 0 || r == '\\' || unicode.IsControl(r) {
			return "", false
		}
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." || len(seg) > phoneMaxSegmentBytes {
			return "", false
		}
	}
	return p, true
}

// cleanPhoneDir is cleanPhonePath for a folder, "" for the top.
func cleanPhoneDir(p string) (string, bool) {
	p = strings.Trim(p, "/")
	if p == "" {
		return "", true
	}
	return cleanPhonePath(p)
}

func phoneDirOf(p string) string {
	d := path.Dir(p)
	if d == "." {
		return ""
	}
	return d
}

// ---------------------------------------------------------------------
// Handlers

func (s *Service) handleDeviceSessionStart(w http.ResponseWriter, r *http.Request) {
	d := deviceOf(r)
	var req SessionStartRequest
	if !decode(w, r, &req) {
		return
	}
	fe := fieldErrors{}
	seen := map[PhoneCategory]bool{}
	var cats []PhoneCategory
	for _, c := range req.Categories {
		if !c.valid() || seen[c] {
			fe.add("categories", FieldInvalid)
			continue
		}
		seen[c] = true
		cats = append(cats, c)
	}
	if len(req.Categories) == 0 {
		fe.add("categories", FieldRequired)
	}
	if len(req.Reason) > 64 || req.ExpectBytes < 0 {
		fe.add("reason", FieldInvalid)
	}
	if len(fe) > 0 {
		writeValidation(w, fe)
		return
	}
	sort.Slice(cats, func(i, j int) bool { return cats[i] < cats[j] })
	lock := s.phone.devLock(d.ID)
	lock.Lock()
	defer lock.Unlock()
	d, err := s.store.GetDevice(d.ID)
	if err != nil {
		s.pfail(w, err)
		return
	}
	if d.MoveState == MoveStateMoving {
		s.pfail(w, errConflict("the backups of this phone are being moved to another place"))
		return
	}
	root, err := s.writableRoot(r.Context(), &d)
	if err != nil {
		s.pfail(w, err)
		return
	}
	if err := needSpace(root, req.ExpectBytes); err != nil {
		s.pfail(w, err)
		return
	}
	var open []PhoneSessionRow
	if err := s.store.db.Where("device_id = ? AND status = ?", d.ID, SessionOpen).Find(&open).Error; err != nil {
		s.pfail(w, err)
		return
	}
	key := joinCats(cats)
	for _, o := range open {
		if o.Categories == key {
			now := s.now().UTC()
			o.LastActivity = now
			_ = s.store.db.Model(&PhoneSessionRow{}).Where("id = ?", o.ID).Update("last_activity", now).Error
			out := o.toAPI()
			out.Resumed = true
			writeOK(w, http.StatusOK, out)
			return
		}
	}
	if len(open) >= PhoneMaxOpenSessions {
		s.pfail(w, errConflict("%d backups of this phone are open already", len(open)))
		return
	}
	now := s.now().UTC()
	run := RunRow{
		ID: NewRunID(now), JobID: d.ID, Kind: string(KindDevice), Trigger: string(RunByDevice),
		Status: string(StatusRunning), Phase: string(PhaseTransfer), Attempt: 1, QueuedAt: now, StartedAt: &now,
	}
	ses := PhoneSessionRow{
		ID: NewSessionID(), DeviceID: d.ID, RunID: run.ID, Categories: key, Reason: req.Reason,
		Status: SessionOpen, StartedAt: now, LastActivity: now, ExpectBytes: req.ExpectBytes,
	}
	err = s.store.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&run).Error; err != nil {
			return err
		}
		return tx.Create(&ses).Error
	})
	if err != nil {
		s.pfail(w, fmt.Errorf("store: start session: %w", err))
		return
	}
	s.setLive(run.ID, &LiveStats{TotalBytes: req.ExpectBytes})
	s.pub.publish(EventRunBegin, runEventProps(run))
	s.deviceChanged(d.ID, DeviceChangeSessionStarted)
	writeOK(w, http.StatusCreated, ses.toAPI())
}

// sessionOf loads an open session of the requesting device.
func (s *Service) sessionOf(d DeviceRow, sid string) (PhoneSessionRow, error) {
	var ses PhoneSessionRow
	err := s.store.db.Where("id = ? AND device_id = ?", sid, d.ID).Take(&ses).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ses, fmt.Errorf("session %s: %w", sid, errNoRecord)
	}
	if err != nil {
		return ses, err
	}
	if ses.Status != SessionOpen {
		return ses, engine.Errorf(ErrSessionClosed, "session %s is %s; start a new one", sid, ses.Status)
	}
	return ses, nil
}

// sessionCat checks that a category is part of the session.
func sessionCat(ses PhoneSessionRow, c PhoneCategory) bool {
	return c.valid() && hasCat(splitCats(ses.Categories), c)
}

// bumpSession adds to a session's counters and marks it active.
func (s *Service) bumpSession(sid string, add map[string]int64) {
	cols := map[string]interface{}{"last_activity": s.now().UTC()}
	for k, v := range add {
		if v != 0 {
			cols[k] = gorm.Expr(k+" + ?", v)
		}
	}
	if err := s.store.db.Model(&PhoneSessionRow{}).Where("id = ?", sid).UpdateColumns(cols).Error; err != nil {
		log.Printf("backup: session %s: %v", sid, err)
	}
}

func (s *Service) handleDeviceCheck(w http.ResponseWriter, r *http.Request) {
	d := deviceOf(r)
	var req CheckRequest
	if !decode(w, r, &req) {
		return
	}
	if !s.phone.checks.allow(d.ID, s.now()) {
		writeError(w, http.StatusTooManyRequests, ErrorBody{ErrorCode: ErrRateLimited, Detail: fmt.Sprintf("at most %d checks a minute", PhoneCheckPerMinute)})
		return
	}
	ses, err := s.sessionOf(d, r.PathValue("sid"))
	if err != nil {
		s.pfail(w, err)
		return
	}
	if !sessionCat(ses, req.Category) || !req.Category.isFiles() {
		writeValidation(w, map[string]string{"category": string(FieldInvalid)})
		return
	}
	if len(req.Items) > PhoneCheckBatch {
		writeValidation(w, map[string]string{"items": string(FieldTooMany)})
		return
	}
	lock := s.phone.devLock(d.ID)
	lock.Lock()
	defer lock.Unlock()
	res, counts, err := s.check(r.Context(), d, ses, req)
	if err != nil {
		s.pfail(w, err)
		return
	}
	s.bumpSession(ses.ID, counts)
	writeOK(w, http.StatusOK, res)
}

// check answers one batch (under the device lock).
func (s *Service) check(ctx context.Context, d DeviceRow, ses PhoneSessionRow, req CheckRequest) (CheckResult, map[string]int64, error) {
	out := CheckResult{Items: make([]CheckAnswer, 0, len(req.Items))}
	counts := map[string]int64{}
	cat := string(req.Category)
	paths := make([]string, 0, len(req.Items))
	for _, it := range req.Items {
		if p, ok := cleanPhonePath(it.Path); ok {
			paths = append(paths, p)
		}
	}
	rows := map[string]PhoneFileRow{}
	for _, chunk := range chunks(paths, 400) {
		var found []PhoneFileRow
		if err := s.store.db.Where("device_id = ? AND category = ? AND path IN ?", d.ID, cat, chunk).Find(&found).Error; err != nil {
			return out, nil, err
		}
		for _, f := range found {
			rows[f.Path] = f
		}
	}
	excluded := map[string]bool{}
	var shas []string
	for _, it := range req.Items {
		if validSHA(it.SHA256) {
			shas = append(shas, strings.ToLower(it.SHA256))
		}
	}
	if len(shas) > 0 {
		var ex []PhoneExcludedRow
		if err := s.store.db.Where("device_id = ? AND sha256 IN ?", d.ID, shas).Find(&ex).Error; err != nil {
			return out, nil, err
		}
		for _, e := range ex {
			excluded[e.SHA256] = true
		}
	}
	var root string
	var clear []string
	for _, it := range req.Items {
		ans := CheckAnswer{Path: it.Path}
		p, ok := cleanPhonePath(it.Path)
		sha := strings.ToLower(it.SHA256)
		if !ok || it.Size < 0 || it.Size > PhoneMaxUploadSize || (sha != "" && !validSHA(sha)) {
			ans.Status = CheckInvalid
			out.Items = append(out.Items, ans)
			continue
		}
		row, have := rows[p]
		switch {
		case sha != "" && excluded[sha]:
			ans.Status = CheckExcluded
		case have && !row.Damaged && sha != "" && row.SHA256 == sha:
			ans.Status = CheckHave
		case have && !row.Damaged && sha == "" && row.Size == it.Size && row.MTime == it.MTime:
			ans.Status = CheckHave
		case sha == "":
			ans.Status = CheckNeedHash
		default:
			ans.Status = CheckNeed
		}
		if ans.Status == CheckHave {
			counts["unchanged"]++
			if row.DeletedOnDeviceAt != nil {
				clear = append(clear, p)
			}
			if row.MTime != it.MTime && sha != "" {
				_ = s.store.db.Model(&PhoneFileRow{}).Where("device_id = ? AND category = ? AND path = ?", d.ID, cat, p).Update("m_time", it.MTime).Error
			}
		}
		if ans.Status == CheckNeed {
			if root == "" {
				r, err := s.writableRoot(ctx, &d)
				if err != nil {
					return out, nil, err
				}
				root = r
			}
			linked, err := s.linkElsewhere(root, d, ses, req.Category, p, CheckItem{Path: p, Size: it.Size, MTime: it.MTime, SHA256: sha})
			if err != nil {
				return out, nil, err
			}
			if linked {
				ans.Status = CheckHaveElsewhere
				counts["linked"]++
			} else {
				var up PhoneUploadRow
				if err := s.store.db.Where("device_id = ? AND category = ? AND path = ? AND sha256 = ? AND length = ? AND dest_gen = ?",
					d.ID, cat, p, sha, it.Size, d.DestGen).Order("updated_at DESC").Take(&up).Error; err == nil {
					ans.UploadID, ans.Offset = up.ID, up.Offset
				}
			}
		}
		out.Items = append(out.Items, ans)
	}
	for _, chunk := range chunks(clear, 400) {
		if err := s.store.db.Model(&PhoneFileRow{}).Where("device_id = ? AND category = ? AND path IN ?", d.ID, cat, chunk).
			Update("deleted_on_device_at", nil).Error; err != nil {
			return out, nil, err
		}
	}
	return out, counts, nil
}

func chunks(in []string, n int) [][]string {
	var out [][]string
	for len(in) > 0 {
		k := n
		if len(in) < k {
			k = len(in)
		}
		out = append(out, in[:k])
		in = in[k:]
	}
	return out
}

func validSHA(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// linkElsewhere places a file whose content the backup already holds
// under another path (a renamed or moved photo) without an upload: a
// hard link to that copy, or a copy when links aren't possible.
func (s *Service) linkElsewhere(root string, d DeviceRow, ses PhoneSessionRow, cat PhoneCategory, p string, it CheckItem) (bool, error) {
	var stored string
	var f PhoneFileRow
	if err := s.store.db.Where("device_id = ? AND sha256 = ? AND size = ? AND damaged = ?", d.ID, it.SHA256, it.Size, false).Take(&f).Error; err == nil {
		stored = f.Stored
	} else {
		var v PhoneVersionRow
		if err := s.store.db.Where("device_id = ? AND sha256 = ? AND size = ?", d.ID, it.SHA256, it.Size).Take(&v).Error; err == nil {
			stored = v.Stored
		}
	}
	if stored == "" {
		return false, nil
	}
	src := filepath.Join(root, filepath.FromSlash(stored))
	fi, err := os.Lstat(src)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() != it.Size {
		return false, nil
	}
	tmp, err := s.uploadTemp(root, "link_"+randomHex(8))
	if err != nil {
		return false, err
	}
	if err := os.Link(src, tmp); err != nil {
		if err := copyFile(src, tmp, fi.ModTime()); err != nil {
			_ = os.Remove(tmp)
			return false, engine.Errorf(ErrIOError, "copying %s: %v", stored, err)
		}
	}
	meta := fileMeta{Size: it.Size, MTime: it.MTime, SHA256: it.SHA256, MediaID: f.MediaID, TakenAt: f.TakenAt}
	if err := s.placeFile(root, d, cat, p, tmp, meta, true); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	return true, nil
}

func (s *Service) handleDeviceDeleted(w http.ResponseWriter, r *http.Request) {
	d := deviceOf(r)
	var req DeletedRequest
	if !decode(w, r, &req) {
		return
	}
	ses, err := s.sessionOf(d, r.PathValue("sid"))
	if err != nil {
		s.pfail(w, err)
		return
	}
	if !sessionCat(ses, req.Category) || !req.Category.isFiles() {
		writeValidation(w, map[string]string{"category": string(FieldInvalid)})
		return
	}
	if len(req.Paths) > PhoneDeletedBatch {
		writeValidation(w, map[string]string{"paths": string(FieldTooMany)})
		return
	}
	var paths []string
	for _, p := range req.Paths {
		if c, ok := cleanPhonePath(p); ok {
			paths = append(paths, c)
		}
	}
	now := s.phoneNow()
	res := DeletedResult{}
	for _, chunk := range chunks(paths, 400) {
		q := s.store.db.Model(&PhoneFileRow{}).Where("device_id = ? AND category = ? AND path IN ? AND deleted_on_device_at IS NULL", d.ID, string(req.Category), chunk).
			Update("deleted_on_device_at", now)
		if q.Error != nil {
			s.pfail(w, q.Error)
			return
		}
		res.Marked += int(q.RowsAffected)
	}
	res.Unknown = len(req.Paths) - res.Marked
	s.bumpSession(ses.ID, map[string]int64{"deleted_mark": int64(res.Marked)})
	writeOK(w, http.StatusOK, res)
}

func (s *Service) handleDeviceCheckItems(w http.ResponseWriter, r *http.Request) {
	d := deviceOf(r)
	var req ItemKeysRequest
	if !decode(w, r, &req) {
		return
	}
	ses, err := s.sessionOf(d, r.PathValue("sid"))
	if err != nil {
		s.pfail(w, err)
		return
	}
	if !sessionCat(ses, req.Category) || !req.Category.info().incremental {
		writeValidation(w, map[string]string{"category": string(FieldInvalid)})
		return
	}
	if len(req.Keys) > PhoneItemKeysBatch {
		writeValidation(w, map[string]string{"keys": string(FieldTooMany)})
		return
	}
	have := map[string]bool{}
	for _, chunk := range chunks(req.Keys, 400) {
		var rows []PhoneItemRow
		if err := s.store.db.Where("device_id = ? AND category = ? AND key IN ?", d.ID, string(req.Category), chunk).Find(&rows).Error; err != nil {
			s.pfail(w, err)
			return
		}
		for _, k := range rows {
			have[k.Key] = true
		}
	}
	out := ItemKeysResult{New: []string{}}
	seen := map[string]bool{}
	for _, k := range req.Keys {
		if !have[k] && !seen[k] {
			seen[k] = true
			out.New = append(out.New, k)
		}
	}
	s.bumpSession(ses.ID, nil)
	writeOK(w, http.StatusOK, out)
}

func (s *Service) handleDeviceFinish(w http.ResponseWriter, r *http.Request) {
	d := deviceOf(r)
	var req FinishRequest
	if !decode(w, r, &req) {
		return
	}
	switch req.Status {
	case StatusSuccess, StatusPartial, StatusFailed, StatusCancelled:
	default:
		writeValidation(w, map[string]string{"status": string(FieldInvalid)})
		return
	}
	if len(req.Errors) > PhoneMaxFinishErrors {
		req.Errors = req.Errors[:PhoneMaxFinishErrors]
	}
	ses, err := s.sessionOf(d, r.PathValue("sid"))
	if err != nil {
		s.pfail(w, err)
		return
	}
	lock := s.phone.devLock(d.ID)
	lock.Lock()
	ses, snap, err := s.endSession(ses, req.Status, req.Errors)
	lock.Unlock()
	if err != nil {
		s.pfail(w, err)
		return
	}
	var pending int64
	s.store.db.Model(&PhoneUploadRow{}).Where("session_id = ?", ses.ID).Count(&pending)
	out := FinishResult{Session: ses.toAPI(), PendingUploads: int(pending)}
	if snap != nil {
		ps := snap.toAPI()
		out.Snapshot = &ps
		s.pruneDevice(d.ID)
	}
	writeOK(w, http.StatusOK, out)
}

// endSession closes a session and its run. status is the run's final
// status; a success or partial one makes a snapshot. Callers hold the
// device lock.
func (s *Service) endSession(ses PhoneSessionRow, status RunStatus, reports []PhoneErrorReport) (PhoneSessionRow, *PhoneSnapshotRow, error) {
	// Reload: counters moved since the caller read it.
	if err := s.store.db.Where("id = ?", ses.ID).Take(&ses).Error; err != nil {
		return ses, nil, err
	}
	if ses.Status != SessionOpen {
		return ses, nil, engine.Errorf(ErrSessionClosed, "session %s is %s already", ses.ID, ses.Status)
	}
	now := s.phoneNow()
	errs := ses.Errors + int64(len(reports))
	if status == StatusSuccess && errs > 0 {
		status = StatusPartial
	}
	switch status {
	case StatusSuccess, StatusPartial:
		ses.Status = SessionFinished
	case StatusCancelled:
		ses.Status = SessionCancelled
	case StatusInterrupted:
		ses.Status = SessionInterrupted
	default:
		ses.Status = SessionFinished
	}
	ses.EndedAt, ses.Errors = &now, errs
	var snap *PhoneSnapshotRow
	if status == StatusSuccess || status == StatusPartial {
		snap = &PhoneSnapshotRow{
			ID: "snap_" + randomHex(8), DeviceID: ses.DeviceID, SessionID: ses.ID, RunID: ses.RunID, TakenAt: now,
			Categories: ses.Categories, Status: string(status), Uploaded: ses.Uploaded, Linked: ses.Linked,
			Unchanged: ses.Unchanged, Deleted: ses.DeletedMark, Exports: ses.Exports, Bytes: ses.Bytes, Errors: errs,
		}
		ses.SnapshotID = snap.ID
	}
	err := s.store.db.Transaction(func(tx *gorm.DB) error {
		if snap != nil {
			if err := tx.Create(snap).Error; err != nil {
				return err
			}
		}
		return tx.Save(&ses).Error
	})
	if err != nil {
		return ses, nil, fmt.Errorf("store: end session: %w", err)
	}
	if err := s.endDeviceRun(ses, status, reports); err != nil {
		log.Printf("backup: session %s: %v", ses.ID, err)
	}
	if status == StatusSuccess || status == StatusPartial {
		_ = s.updateDevice(ses.DeviceID, map[string]interface{}{"stale_notified_at": nil})
	}
	s.deviceChanged(ses.DeviceID, DeviceChangeSessionEnded)
	return ses, snap, nil
}

// endDeviceRun writes a session's outcome to its run.
func (s *Service) endDeviceRun(ses PhoneSessionRow, status RunStatus, reports []PhoneErrorReport) error {
	run, err := s.store.GetRun(ses.RunID)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	run.Status, run.Phase, run.EndedAt = string(status), "", &now
	run.FilesAdded, run.FilesDeleted = ses.Uploaded+ses.Linked, ses.DeletedMark
	run.FilesSkipped, run.FilesErrored = ses.Unchanged, ses.Errors
	run.BytesTransferred, run.BytesTotal = ses.Bytes, ses.Bytes
	var msg Message
	switch status {
	case StatusSuccess:
		if run.FilesAdded == 0 && run.FilesDeleted == 0 && ses.Exports == 0 {
			msg = Message{Key: "backup.run.summary.ok_nothing"}
		} else {
			msg = Message{Key: "backup.run.summary.ok", Args: map[string]interface{}{"added": run.FilesAdded, "changed": 0, "deleted": run.FilesDeleted, "bytes": ses.Bytes}}
		}
	case StatusPartial:
		msg = Message{Key: "backup.run.summary.partial", Args: map[string]interface{}{"added": run.FilesAdded, "changed": 0, "errors": ses.Errors}}
	case StatusCancelled:
		run.ErrorCode = string(ErrCancelledByUser)
		msg = Message{Key: "backup.run.summary.cancelled", Args: map[string]interface{}{"reason_key": errorReasonKey(ErrCancelledByUser)}}
	case StatusInterrupted:
		run.ErrorCode = string(ErrInterrupted)
		msg = Message{Key: "backup.run.summary.interrupted"}
	default:
		run.ErrorCode = string(ErrDeviceFailed)
		msg = Message{Key: "backup.run.summary.failed", Args: map[string]interface{}{"reason_key": errorReasonKey(ErrDeviceFailed)}}
	}
	run.Summary = EncodeMessage(msg)
	if len(reports) > 0 {
		lp := logPath(s.store.DataDir(), run.JobID, run.ID)
		fe := make([]engine.FileError, 0, len(reports))
		for _, rep := range reports {
			detail := rep.Message
			if len(detail) > 500 {
				detail = detail[:500]
			}
			fe = append(fe, engine.FileError{Path: path.Join(string(rep.Category), rep.Path), Code: ErrDeviceFailed, Detail: detail})
		}
		if raw, err := json.Marshal(fe); err == nil {
			if err := os.MkdirAll(filepath.Dir(lp), 0o700); err == nil {
				if writeFileAtomic(fileErrorsPath(lp), raw, 0o600) == nil {
					run.LogPath = lp
				}
			}
		}
	}
	if err := s.store.SaveRun(run); err != nil {
		return err
	}
	s.setLive(run.ID, nil)
	s.pub.publish(EventRunEnd, endProps(run, LiveStats{Bytes: ses.Bytes, TotalBytes: ses.Bytes, Files: run.FilesAdded, Errors: ses.Errors}))
	return nil
}

// cancelDeviceRun is POST /runs/:id/cancel on a session's run.
func (s *Service) cancelDeviceRun(run RunRow) (RunRow, error) {
	var ses PhoneSessionRow
	if err := s.store.db.Where("run_id = ?", run.ID).Take(&ses).Error; err != nil {
		return run, fmt.Errorf("session of run %s: %w", run.ID, errNoRecord)
	}
	lock := s.phone.devLock(ses.DeviceID)
	lock.Lock()
	_, _, err := s.endSession(ses, StatusCancelled, nil)
	lock.Unlock()
	if err != nil {
		return run, err
	}
	return s.store.GetRun(run.ID)
}

// sweepSessions closes sessions the phone left idle for 30 minutes
// (network gone, app killed) as interrupted. Their uploads stay
// resumable in a new session.
func (s *Service) sweepSessions() {
	var rows []PhoneSessionRow
	cutoff := s.now().UTC().Add(-PhoneSessionIdle)
	if err := s.store.db.Where("status = ? AND last_activity < ?", SessionOpen, cutoff).Find(&rows).Error; err != nil {
		log.Printf("backup: sweep sessions: %v", err)
		return
	}
	for _, ses := range rows {
		lock := s.phone.devLock(ses.DeviceID)
		lock.Lock()
		if _, _, err := s.endSession(ses, StatusInterrupted, nil); err != nil {
			log.Printf("backup: sweep session %s: %v", ses.ID, err)
		}
		lock.Unlock()
	}
}

// phoneLoop is the phone housekeeping: idle sessions, expired uploads,
// stale phones, retention.
func (s *Service) phoneLoop(ctx context.Context) {
	every := phoneLoopDefault
	if m := s.cfg.Timings.Maintenance; m > 0 && m < every {
		every = m
	}
	s.recoverMoves()
	lastPrune := time.Time{}
	for ctx.Err() == nil {
		s.sweepSessions()
		s.purgeUploads()
		s.checkDeviceStale()
		if s.now().Sub(lastPrune) >= 6*time.Hour {
			if rows, err := s.store.ListDevices(); err == nil {
				for _, d := range rows {
					s.pruneDevice(d.ID)
				}
			}
			lastPrune = s.now()
		}
		sleepCtx(ctx, every)
	}
}
