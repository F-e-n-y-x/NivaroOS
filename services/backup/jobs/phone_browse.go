package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// Browsing, downloading and restoring a phone's backup. Every read takes
// a snapshot: an id, or "latest" (the backup as it is now, including
// what an open session added). A snapshot is a point in time; what a
// path held then is the current entry when it arrived at or before it,
// otherwise the version valid then.

const snapLatest = "latest"

func (r PhoneSnapshotRow) toAPI() PhoneSnapshot {
	return PhoneSnapshot{
		ID: r.ID, SessionID: r.SessionID, RunID: r.RunID, TakenAt: r.TakenAt, Categories: nonNil(splitCats(r.Categories)),
		Status: RunStatus(r.Status),
		Counts: PhoneSessionCounts{Uploaded: r.Uploaded, Linked: r.Linked, Unchanged: r.Unchanged, Deleted: r.Deleted, Exports: r.Exports, Bytes: r.Bytes, Errors: r.Errors},
	}
}

// snapshotAt resolves a snapshot id ("" or "latest": now). t is nil for
// now.
func (s *Service) snapshotAt(deviceID, id string) (string, time.Time, *time.Time, error) {
	if id == "" || id == snapLatest {
		return snapLatest, s.now().UTC(), nil, nil
	}
	var snap PhoneSnapshotRow
	err := s.store.db.Where("id = ? AND device_id = ?", id, deviceID).Take(&snap).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", time.Time{}, nil, fmt.Errorf("snapshot %s: %w", id, errNoRecord)
	}
	if err != nil {
		return "", time.Time{}, nil, err
	}
	t := snap.TakenAt
	return snap.ID, snap.TakenAt, &t, nil
}

// phoneItem is what a path held at a snapshot.
type phoneItem struct {
	Category          string
	Path, Dir, Stored string
	Size, MTime       int64
	SHA256            string
	MediaID, TakenAt  int64
	DeletedOnDeviceAt *time.Time
	Version           bool
}

func fileItem(f PhoneFileRow) phoneItem {
	return phoneItem{Category: f.Category, Path: f.Path, Dir: f.Dir, Stored: f.Stored, Size: f.Size, MTime: f.MTime, SHA256: f.SHA256,
		MediaID: f.MediaID, TakenAt: f.TakenAt, DeletedOnDeviceAt: f.DeletedOnDeviceAt}
}

func versionItem(v PhoneVersionRow) phoneItem {
	return phoneItem{Category: v.Category, Path: v.Path, Dir: v.Dir, Stored: v.Stored, Size: v.Size, MTime: v.MTime, SHA256: v.SHA256,
		MediaID: v.MediaID, TakenAt: v.TakenAt, DeletedOnDeviceAt: v.DeletedOnDeviceAt, Version: true}
}

// visible applies the deleted-on-phone rule at t.
func (it phoneItem) visible(t *time.Time, includeDeleted bool) bool {
	if includeDeleted || it.DeletedOnDeviceAt == nil {
		return true
	}
	return t != nil && it.DeletedOnDeviceAt.After(*t)
}

// itemsWhere lists the items at t matching a condition on the file and
// version tables.
func (s *Service) itemsWhere(deviceID string, t *time.Time, where string, args []interface{}, order string, limit int) ([]phoneItem, error) {
	q := s.store.db.Model(&PhoneFileRow{}).Where("device_id = ?", deviceID).Where(where, args...)
	if t != nil {
		q = q.Where("content_since <= ?", *t)
	}
	if order != "" {
		q = q.Order(order)
	}
	if limit > 0 {
		q = q.Limit(limit)
	}
	var files []PhoneFileRow
	if err := q.Find(&files).Error; err != nil {
		return nil, err
	}
	out := make([]phoneItem, 0, len(files))
	for _, f := range files {
		out = append(out, fileItem(f))
	}
	if t == nil {
		return out, nil
	}
	vq := s.store.db.Model(&PhoneVersionRow{}).Where("device_id = ?", deviceID).Where(where, args...).
		Where("since <= ? AND until > ?", *t, *t)
	if order != "" {
		vq = vq.Order(order)
	}
	if limit > 0 {
		vq = vq.Limit(limit)
	}
	var vers []PhoneVersionRow
	if err := vq.Find(&vers).Error; err != nil {
		return nil, err
	}
	for _, v := range vers {
		out = append(out, versionItem(v))
	}
	return out, nil
}

// itemAt is what (category, path) held at t.
func (s *Service) itemAt(deviceID string, cat PhoneCategory, p string, t *time.Time) (phoneItem, error) {
	items, err := s.itemsWhere(deviceID, t, "category = ? AND path = ?", []interface{}{string(cat), p}, "", 0)
	if err != nil {
		return phoneItem{}, err
	}
	if len(items) == 0 {
		return phoneItem{}, fmt.Errorf("%s/%s in this snapshot: %w", cat, p, errNoRecord)
	}
	return items[0], nil
}

// readDevice is the device a read route is about: the device itself, or
// any device for an admin.
func (s *Service) readDevice(w http.ResponseWriter, r *http.Request) (DeviceRow, bool) {
	if d := deviceOf(r); d.ID != "" {
		return d, true
	}
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

// readRoot is the device folder for a read: offline is an error; a
// folder not made yet is "" (nothing to read).
func (s *Service) readRoot(ctx context.Context, d *DeviceRow) (string, error) {
	root, err := s.phoneRoot(ctx, d, false)
	if errors.Is(err, errNoFolder) {
		return "", nil
	}
	return root, err
}

func (s *Service) handleDeviceSnapshots(w http.ResponseWriter, r *http.Request) {
	d, ok := s.readDevice(w, r)
	if !ok {
		return
	}
	var rows []PhoneSnapshotRow
	if err := s.store.db.Where("device_id = ?", d.ID).Order("taken_at DESC").Limit(1000).Find(&rows).Error; err != nil {
		s.pfail(w, err)
		return
	}
	out := make([]PhoneSnapshot, len(rows))
	for i, row := range rows {
		out[i] = row.toAPI()
	}
	writeOK(w, http.StatusOK, out)
}

func (s *Service) handleDeviceBrowse(w http.ResponseWriter, r *http.Request) {
	d, ok := s.readDevice(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	cat := PhoneCategory(q.Get("category"))
	dir, okDir := cleanPhoneDir(q.Get("path"))
	fe := fieldErrors{}
	if !cat.valid() || !cat.isFiles() {
		fe.add("category", FieldInvalid)
	}
	if !okDir {
		fe.add("path", ErrPathNotAllowed)
	}
	if len(fe) > 0 {
		writeValidation(w, fe)
		return
	}
	incl := parseBoolQ(q.Get("include_deleted"))
	sid, takenAt, t, err := s.snapshotAt(d.ID, r.PathValue("snap"))
	if err != nil {
		s.pfail(w, err)
		return
	}
	out := PhoneBrowseResult{Snapshot: sid, TakenAt: takenAt, Category: cat, Path: dir, Entries: []PhoneEntry{}}
	files, err := s.itemsWhere(d.ID, t, "category = ? AND dir = ?", []interface{}{string(cat), dir}, "path", phoneBrowsePageMax+1)
	if err != nil {
		s.pfail(w, err)
		return
	}
	// Sub-folders: the next segment of every deeper item.
	prefix := ""
	where, args := "category = ? AND dir <> ''", []interface{}{string(cat)}
	if dir != "" {
		prefix = dir + "/"
		where, args = "category = ? AND substr(dir, 1, ?) = ?", []interface{}{string(cat), utf8.RuneCountInString(prefix), prefix}
	}
	deeper, err := s.dirsWhere(d.ID, t, incl, where, args)
	if err != nil {
		s.pfail(w, err)
		return
	}
	subs := map[string]bool{}
	for _, dd := range deeper {
		rest := strings.TrimPrefix(dd, prefix)
		if rest == dd && prefix != "" || rest == "" {
			continue
		}
		name, _, _ := strings.Cut(rest, "/")
		subs[name] = true
	}
	names := make([]string, 0, len(subs))
	for n := range subs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		out.Entries = append(out.Entries, PhoneEntry{Name: n, Path: path.Join(dir, n), Dir: true})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	for _, f := range files {
		if !f.visible(t, incl) {
			continue
		}
		out.Entries = append(out.Entries, PhoneEntry{
			Name: path.Base(f.Path), Path: f.Path, Size: f.Size, MTime: f.MTime, SHA256: f.SHA256, TakenAt: f.TakenAt,
			MediaID: f.MediaID, DeletedOnDeviceAt: f.DeletedOnDeviceAt, Version: f.Version,
		})
	}
	if len(out.Entries) > phoneBrowsePageMax {
		out.Entries, out.Truncated = out.Entries[:phoneBrowsePageMax], true
	}
	writeOK(w, http.StatusOK, out)
}

// dirsWhere are the distinct folders of the items at t.
func (s *Service) dirsWhere(deviceID string, t *time.Time, incl bool, where string, args []interface{}) ([]string, error) {
	var out []string
	q := s.store.db.Model(&PhoneFileRow{}).Where("device_id = ?", deviceID).Where(where, args...)
	if t != nil {
		q = q.Where("content_since <= ?", *t)
		if !incl {
			q = q.Where("deleted_on_device_at IS NULL OR deleted_on_device_at > ?", *t)
		}
	} else if !incl {
		q = q.Where("deleted_on_device_at IS NULL")
	}
	if err := q.Distinct().Pluck("dir", &out).Error; err != nil {
		return nil, err
	}
	if t == nil {
		return out, nil
	}
	var vd []string
	vq := s.store.db.Model(&PhoneVersionRow{}).Where("device_id = ?", deviceID).Where(where, args...).Where("since <= ? AND until > ?", *t, *t)
	if !incl {
		vq = vq.Where("deleted_on_device_at IS NULL OR deleted_on_device_at > ?", *t)
	}
	if err := vq.Distinct().Pluck("dir", &vd).Error; err != nil {
		return nil, err
	}
	return append(out, vd...), nil
}

// serveStored streams one stored file (Range works).
func (s *Service) serveStored(w http.ResponseWriter, r *http.Request, root, stored, name, sha, contentType string, attach bool) {
	if root == "" {
		writeError(w, http.StatusNotFound, ErrorBody{ErrorCode: ErrNotFound, Detail: "nothing is stored for this phone yet"})
		return
	}
	abs := filepath.Join(root, filepath.FromSlash(stored))
	if !pathInside(abs, root) {
		writeError(w, http.StatusNotFound, ErrorBody{ErrorCode: ErrNotFound})
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		writeError(w, http.StatusNotFound, ErrorBody{ErrorCode: ErrNotFound, Detail: "the stored file is missing (run a check)"})
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		writeError(w, http.StatusNotFound, ErrorBody{ErrorCode: ErrNotFound})
		return
	}
	if contentType == "" {
		contentType = mime.TypeByExtension(strings.ToLower(path.Ext(name)))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	if sha != "" {
		w.Header().Set("ETag", `"`+sha+`"`)
		w.Header().Set("X-NivaroOS-SHA256", sha)
	}
	disp := "inline"
	if attach {
		disp = "attachment"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disp, map[string]string{"filename": name}))
	http.ServeContent(w, r, "", fi.ModTime(), f)
}

func (s *Service) handleDeviceFileContent(w http.ResponseWriter, r *http.Request) {
	d, ok := s.readDevice(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	cat := PhoneCategory(q.Get("category"))
	p, okPath := cleanPhonePath(q.Get("path"))
	if !cat.valid() || !cat.isFiles() || !okPath {
		writeValidation(w, map[string]string{"path": string(ErrPathNotAllowed)})
		return
	}
	_, _, t, err := s.snapshotAt(d.ID, q.Get("snapshot"))
	if err != nil {
		s.pfail(w, err)
		return
	}
	it, err := s.itemAt(d.ID, cat, p, t)
	if err != nil {
		s.pfail(w, err)
		return
	}
	root, err := s.readRoot(r.Context(), &d)
	if err != nil {
		s.pfail(w, err)
		return
	}
	w.Header().Set("X-NivaroOS-MTime", strconv.FormatInt(it.MTime, 10))
	s.serveStored(w, r, root, it.Stored, path.Base(it.Path), it.SHA256, "", false)
}

func (s *Service) handleDeviceExports(w http.ResponseWriter, r *http.Request) {
	d, ok := s.readDevice(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	cat := PhoneCategory(q.Get("category"))
	if cat != "" && (!cat.valid() || cat.isFiles()) {
		writeValidation(w, map[string]string{"category": string(FieldInvalid)})
		return
	}
	var rows []PhoneExportRow
	var err error
	if parseBoolQ(q.Get("all")) {
		qq := s.store.db.Where("device_id = ?", d.ID)
		if cat != "" {
			qq = qq.Where("category = ?", string(cat))
		}
		err = qq.Order("taken_at DESC").Find(&rows).Error
	} else {
		var t *time.Time
		if _, _, t, err = s.snapshotAt(d.ID, q.Get("snapshot")); err == nil {
			rows, err = s.exportsAt(d.ID, cat, t)
		}
	}
	if err != nil {
		s.pfail(w, err)
		return
	}
	out := make([]PhoneExport, len(rows))
	for i, row := range rows {
		out[i] = row.toAPI()
	}
	writeOK(w, http.StatusOK, out)
}

func (s *Service) exportRow(deviceID, id string) (PhoneExportRow, error) {
	var row PhoneExportRow
	err := s.store.db.Where("id = ? AND device_id = ?", id, deviceID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return row, fmt.Errorf("export %s: %w", id, errNoRecord)
	}
	return row, err
}

func (s *Service) handleDeviceExportContent(w http.ResponseWriter, r *http.Request) {
	d, ok := s.readDevice(w, r)
	if !ok {
		return
	}
	row, err := s.exportRow(d.ID, r.PathValue("eid"))
	if err != nil {
		s.pfail(w, err)
		return
	}
	root, err := s.readRoot(r.Context(), &d)
	if err != nil {
		s.pfail(w, err)
		return
	}
	ct := PhoneCategory(row.Category).info().contentType
	if row.Encrypted {
		ct = "application/octet-stream"
	}
	s.serveStored(w, r, root, row.Stored, path.Base(row.Stored), row.SHA256, ct, false)
}

func (s *Service) handleDeviceExportFull(w http.ResponseWriter, r *http.Request) {
	d, ok := s.readDevice(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	cat := PhoneCategory(q.Get("category"))
	if !cat.valid() || !cat.info().incremental {
		writeValidation(w, map[string]string{"category": string(FieldInvalid)})
		return
	}
	s.serveFull(w, r, d, cat, q.Get("snapshot"), false)
}

func (s *Service) serveFull(w http.ResponseWriter, r *http.Request, d DeviceRow, cat PhoneCategory, snapshot string, attach bool) {
	_, _, t, err := s.snapshotAt(d.ID, snapshot)
	if err != nil {
		s.pfail(w, err)
		return
	}
	rows, err := s.exportsAt(d.ID, cat, t)
	if err != nil {
		s.pfail(w, err)
		return
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].TakenAt.Before(rows[j].TakenAt) })
	root, err := s.readRoot(r.Context(), &d)
	if err != nil {
		s.pfail(w, err)
		return
	}
	if root == "" {
		rows = nil
	}
	name := cat.info().prefix + "-full.xml"
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Cache-Control", "no-store")
	disp := "inline"
	if attach {
		disp = "attachment"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disp, map[string]string{"filename": name}))
	if err := writeFullXML(w, root, cat, rows); err != nil {
		log.Printf("backup: device %s: full %s: %v", d.ID, cat, err)
	}
}

func (s *Service) handleDeviceRestoreManifest(w http.ResponseWriter, r *http.Request) {
	d, ok := s.readDevice(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	cat := PhoneCategory(q.Get("category"))
	if cat != "" && (!cat.valid() || !cat.isFiles()) {
		writeValidation(w, map[string]string{"category": string(FieldInvalid)})
		return
	}
	limit := phoneManifestPageSize
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > phoneManifestPageMax {
			writeValidation(w, map[string]string{"limit": string(FieldOutOfRange)})
			return
		}
		limit = n
	}
	after := q.Get("after")
	sid, takenAt, t, err := s.snapshotAt(d.ID, q.Get("snapshot"))
	if err != nil {
		s.pfail(w, err)
		return
	}
	where, args := "1 = 1", []interface{}{}
	if cat != "" {
		where, args = "category = ?", []interface{}{string(cat)}
	}
	if after != "" {
		ac, ap, _ := strings.Cut(after, ":")
		where += " AND (category > ? OR (category = ? AND path > ?))"
		args = append(args, ac, ac, ap)
	}
	items, err := s.itemsWhere(d.ID, t, where, args, "category, path", limit)
	if err != nil {
		s.pfail(w, err)
		return
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Category != items[j].Category {
			return items[i].Category < items[j].Category
		}
		return items[i].Path < items[j].Path
	})
	more := len(items) > limit
	if len(items) >= limit {
		more = true
		items = items[:limit]
	}
	out := RestoreManifest{Snapshot: sid, TakenAt: takenAt, Items: make([]RestoreItem, 0, len(items)), Exports: []PhoneExport{}}
	for _, it := range items {
		v := url.Values{"category": {it.Category}, "path": {it.Path}, "snapshot": {sid}}
		out.Items = append(out.Items, RestoreItem{
			Category: PhoneCategory(it.Category), Path: it.Path, Size: it.Size, MTime: it.MTime, SHA256: it.SHA256, TakenAt: it.TakenAt,
			MediaID: it.MediaID, DeletedOnDeviceAt: it.DeletedOnDeviceAt,
			Content: "/devices/" + d.ID + "/files/content?" + v.Encode(),
		})
	}
	if more && len(items) > 0 {
		last := items[len(items)-1]
		out.NextAfter = last.Category + ":" + last.Path
	}
	if after == "" && cat == "" {
		rows, err := s.exportsAt(d.ID, "", t)
		if err != nil {
			s.pfail(w, err)
			return
		}
		for _, row := range rows {
			out.Exports = append(out.Exports, row.toAPI())
		}
	}
	writeOK(w, http.StatusOK, out)
}

func (s *Service) handleDeviceExcludedList(w http.ResponseWriter, r *http.Request) {
	d, ok := s.readDevice(w, r)
	if !ok {
		return
	}
	var rows []PhoneExcludedRow
	if err := s.store.db.Where("device_id = ?", d.ID).Order("added_at DESC").Find(&rows).Error; err != nil {
		s.pfail(w, err)
		return
	}
	out := make([]PhoneExcluded, len(rows))
	for i, row := range rows {
		out[i] = PhoneExcluded{SHA256: row.SHA256, Path: row.Path, AddedAt: row.AddedAt}
	}
	writeOK(w, http.StatusOK, out)
}

// ---------------------------------------------------------------------
// Verify

// VerifyResult is the outcome of POST /devices/:id/verify.
type VerifyResult struct {
	Deep      bool       `json:"deep"`
	Running   bool       `json:"running"`
	Checked   int64      `json:"checked"`
	Missing   int64      `json:"missing"`
	Damaged   int64      `json:"damaged"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	Error     string     `json:"error"`
}

func (s *Service) handleDeviceVerify(w http.ResponseWriter, r *http.Request) {
	d, ok := s.readDevice(w, r)
	if !ok {
		return
	}
	var req VerifyRequest
	if !decode(w, r, &req) {
		return
	}
	root, err := s.readRoot(r.Context(), &d)
	if err != nil {
		s.pfail(w, err)
		return
	}
	s.phone.mu.Lock()
	if s.phone.verify[d.ID] {
		s.phone.mu.Unlock()
		s.pfail(w, errConflict("a check of this phone's backup is running"))
		return
	}
	s.phone.verify[d.ID] = true
	s.phone.mu.Unlock()
	res := VerifyResult{Deep: req.Deep, StartedAt: s.now().UTC()}
	run := func(ctx context.Context) VerifyResult {
		defer func() {
			s.phone.mu.Lock()
			delete(s.phone.verify, d.ID)
			s.phone.mu.Unlock()
		}()
		out := s.verifyDevice(ctx, d, root, req.Deep, res)
		_ = s.updateDevice(d.ID, map[string]interface{}{"last_verify": mustJSON(out)})
		s.deviceChanged(d.ID, DeviceChangeVerified)
		return out
	}
	if req.Deep {
		res.Running = true
		_ = s.updateDevice(d.ID, map[string]interface{}{"last_verify": mustJSON(res)})
		s.goRun(func() { run(s.ctx) })
		writeOK(w, http.StatusAccepted, res)
		return
	}
	writeOK(w, http.StatusOK, run(r.Context()))
}

// verifyDevice checks every stored file of the index: missing or of the
// wrong size (deep: wrong SHA-256) marks the entry damaged, so the next
// check asks the phone for it again.
func (s *Service) verifyDevice(ctx context.Context, d DeviceRow, root string, deep bool, res VerifyResult) VerifyResult {
	var rows []PhoneFileRow
	err := s.store.db.Where("device_id = ?", d.ID).FindInBatches(&rows, 1000, func(tx *gorm.DB, _ int) error {
		for _, f := range rows {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			res.Checked++
			bad := false
			abs := filepath.Join(root, filepath.FromSlash(f.Stored))
			fi, err := os.Lstat(abs)
			switch {
			case root == "" || err != nil || !fi.Mode().IsRegular():
				res.Missing++
				bad = true
			case fi.Size() != f.Size:
				res.Damaged++
				bad = true
			case deep:
				if sum, err := fileSHA(abs); err != nil || sum != f.SHA256 {
					res.Damaged++
					bad = true
				}
			}
			if bad != f.Damaged {
				if err := s.store.db.Model(&PhoneFileRow{}).Where("device_id = ? AND category = ? AND path = ?", f.DeviceID, f.Category, f.Path).
					Update("damaged", bad).Error; err != nil {
					return err
				}
			}
		}
		return nil
	}).Error
	if err != nil {
		res.Error = err.Error()
	}
	end := s.now().UTC()
	res.EndedAt, res.Running = &end, false
	return res
}

func fileSHA(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func lastVerify(d DeviceRow) *VerifyResult {
	if d.LastVerify == "" {
		return nil
	}
	var v VerifyResult
	if json.Unmarshal([]byte(d.LastVerify), &v) != nil {
		return nil
	}
	return &v
}

var _ = engine.CodeOf
