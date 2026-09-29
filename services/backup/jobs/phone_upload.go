package jobs

import (
	"crypto/sha256"
	"encoding"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// Resumable uploads: a subset of tus 1.0 (https://tus.io/protocols/
// resumable-upload): creation, HEAD, PATCH and termination, no
// extensions beyond those. One upload is one file (or one export) with
// its SHA-256 announced up front; the server hashes as it receives and
// only a matching file is placed. Chunks are at most PhoneChunkSize.

const (
	TusVersion         = "1.0.0"
	UploadResultHeader = "X-NivaroOS-Upload-Result"
	tusContentType     = "application/offset+octet-stream"
)

// Upload results (UploadCreated.Status and the X-NivaroOS-Upload-Result
// header of the last PATCH).
const (
	UploadCreatedStatus = "created"
	UploadResumed       = "resumed"
	UploadHave          = "have"         // already stored: nothing to send
	UploadExcluded      = "excluded"     // the owner removed this content: don't send
	UploadStored        = "stored"       // placed (last PATCH, or an empty file at creation)
	UploadUnchanged     = "unchanged"    // an export equal to the newest one: not stored again
	UploadNoNewItems    = "no_new_items" // an sms/calllog export with nothing new: not stored
)

func NewUploadID() string { return "up_" + randomHex(12) }

// uploadTemp is a new temp file path under the device's upload folder.
func (s *Service) uploadTemp(root, name string) (string, error) {
	dir := filepath.Join(root, phoneUploadsDir)
	if err := os.MkdirAll(dir, phoneDirMode); err != nil {
		return "", engine.Errorf(ErrIOError, "creating %s: %v", dir, err)
	}
	return filepath.Join(dir, name), nil
}

func uploadPart(root, id string) string {
	return filepath.Join(root, phoneUploadsDir, id+".part")
}

// parseUploadMetadata reads tus Upload-Metadata: "key base64,key2 base64".
func parseUploadMetadata(h string) (map[string]string, bool) {
	out := map[string]string{}
	if strings.TrimSpace(h) == "" {
		return out, true
	}
	for _, pair := range strings.Split(h, ",") {
		pair = strings.TrimSpace(pair)
		k, v, _ := strings.Cut(pair, " ")
		if k == "" {
			return nil, false
		}
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(v))
		if err != nil {
			return nil, false
		}
		out[k] = string(raw)
	}
	return out, true
}

func tusHeaders(w http.ResponseWriter) {
	w.Header().Set("Tus-Resumable", TusVersion)
	w.Header().Set("Cache-Control", "no-store")
}

func (s *Service) uploadLocation(deviceID, uploadID string) string {
	return APIBase + "/devices/" + deviceID + "/uploads/" + uploadID
}

func (s *Service) handleUploadCreate(w http.ResponseWriter, r *http.Request) {
	tusHeaders(w)
	d := deviceOf(r)
	length, err := strconv.ParseInt(r.Header.Get("Upload-Length"), 10, 64)
	fe := fieldErrors{}
	if err != nil || length < 0 {
		fe.add("Upload-Length", FieldInvalid)
	} else if length > PhoneMaxUploadSize {
		writeError(w, http.StatusRequestEntityTooLarge, ErrorBody{ErrorCode: ErrTooLarge, Detail: fmt.Sprintf("at most %d bytes per file", int64(PhoneMaxUploadSize))})
		return
	}
	meta, ok := parseUploadMetadata(r.Header.Get("Upload-Metadata"))
	if !ok {
		fe.add("Upload-Metadata", FieldInvalid)
	}
	if len(fe) > 0 {
		writeValidation(w, fe)
		return
	}
	up := PhoneUploadRow{DeviceID: d.ID, SessionID: meta["session"], Category: meta["category"], SHA256: strings.ToLower(meta["sha256"]), Length: length}
	cat := PhoneCategory(up.Category)
	if !cat.valid() {
		fe.add("category", FieldInvalid)
	}
	if !validSHA(up.SHA256) {
		fe.add("sha256", FieldInvalid)
	}
	num := func(k string) int64 {
		v := meta[k]
		if v == "" {
			return 0
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			fe.add(k, FieldInvalid)
		}
		return n
	}
	up.MTime, up.TakenAt, up.MediaID = num("mtime"), num("taken_at"), num("media_id")
	up.Encrypted = meta["encrypted"] == "1" || meta["encrypted"] == "true"
	if cat.valid() {
		if cat.isFiles() {
			p, ok := cleanPhonePath(meta["path"])
			if !ok {
				fe.add("path", ErrPathNotAllowed)
			}
			up.Path = p
		} else {
			name, ok := cleanExportName(cat, meta["name"])
			if !ok {
				fe.add("name", FieldInvalid)
			}
			up.Name = name
		}
	}
	if len(fe) > 0 {
		writeValidation(w, fe)
		return
	}
	ses, err := s.sessionOf(d, up.SessionID)
	if err != nil {
		s.pfail(w, err)
		return
	}
	if !sessionCat(ses, cat) {
		writeValidation(w, map[string]string{"category": string(FieldInvalid)})
		return
	}
	lock := s.phone.devLock(d.ID)
	lock.Lock()
	defer lock.Unlock()
	d, err = s.store.GetDevice(d.ID)
	if err != nil {
		s.pfail(w, err)
		return
	}
	root, err := s.writableRoot(r.Context(), &d)
	if err != nil {
		s.pfail(w, err)
		return
	}
	answer := func(status int, c UploadCreated) {
		if c.UploadID != "" {
			w.Header().Set("Location", c.Location)
			w.Header().Set("Upload-Offset", strconv.FormatInt(c.Offset, 10))
		}
		writeOK(w, status, c)
	}
	// Excluded, or stored already?
	var ex int64
	s.store.db.Model(&PhoneExcludedRow{}).Where("device_id = ? AND sha256 = ?", d.ID, up.SHA256).Count(&ex)
	if ex > 0 {
		answer(http.StatusOK, UploadCreated{Status: UploadExcluded, Length: length})
		return
	}
	if cat.isFiles() {
		var f PhoneFileRow
		if err := s.store.db.Where("device_id = ? AND category = ? AND path = ?", d.ID, up.Category, up.Path).Take(&f).Error; err == nil && f.SHA256 == up.SHA256 && !f.Damaged {
			if f.DeletedOnDeviceAt != nil {
				_ = s.store.db.Model(&PhoneFileRow{}).Where("device_id = ? AND category = ? AND path = ?", d.ID, up.Category, up.Path).Update("deleted_on_device_at", nil).Error
			}
			s.bumpSession(ses.ID, map[string]int64{"unchanged": 1})
			answer(http.StatusOK, UploadCreated{Status: UploadHave, Length: length})
			return
		}
	}
	// Resume an unfinished upload of the same file.
	var prev PhoneUploadRow
	q := s.store.db.Where("device_id = ? AND category = ? AND path = ? AND name = ? AND sha256 = ? AND length = ?", d.ID, up.Category, up.Path, up.Name, up.SHA256, length)
	if err := q.Order("updated_at DESC").Take(&prev).Error; err == nil {
		if prev.DestGen == d.DestGen {
			now := s.now().UTC()
			prev.SessionID, prev.UpdatedAt, prev.ExpiresAt = ses.ID, now, now.Add(PhoneUploadExpiry)
			for dst, v := range map[*int64]int64{&prev.MTime: up.MTime, &prev.TakenAt: up.TakenAt, &prev.MediaID: up.MediaID} {
				if v != 0 {
					*dst = v
				}
			}
			if err := s.store.db.Save(&prev).Error; err != nil {
				s.pfail(w, err)
				return
			}
			s.bumpSession(ses.ID, nil)
			answer(http.StatusOK, UploadCreated{UploadID: prev.ID, Location: s.uploadLocation(d.ID, prev.ID), Offset: prev.Offset, Length: length, ExpiresAt: prev.ExpiresAt, Status: UploadResumed})
			return
		}
		s.dropUpload(root, prev)
	}
	var open []PhoneUploadRow
	if err := s.store.db.Where("device_id = ?", d.ID).Find(&open).Error; err != nil {
		s.pfail(w, err)
		return
	}
	if len(open) >= PhoneMaxOpenUploads {
		writeError(w, http.StatusTooManyRequests, ErrorBody{ErrorCode: ErrRateLimited, Detail: fmt.Sprintf("at most %d unfinished uploads per phone", PhoneMaxOpenUploads)})
		return
	}
	var pending int64
	for _, o := range open {
		pending += o.Length - o.Offset
	}
	if err := needSpace(root, length+pending); err != nil {
		s.pfail(w, err)
		return
	}
	now := s.now().UTC()
	up.ID, up.DestGen, up.CreatedAt, up.UpdatedAt, up.ExpiresAt = NewUploadID(), d.DestGen, now, now, now.Add(PhoneUploadExpiry)
	up.HashState, _ = sha256.New().(encoding.BinaryMarshaler).MarshalBinary()
	part, err := s.uploadTemp(root, up.ID+".part")
	if err != nil {
		s.pfail(w, err)
		return
	}
	f, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_EXCL, phoneFileMode)
	if err != nil {
		s.pfail(w, engine.Errorf(ErrIOError, "creating %s: %v", part, err))
		return
	}
	f.Close()
	if length == 0 {
		// Nothing to send: finish now.
		up.HashState = nil
		result, err := s.finishUpload(root, d, ses, up, part)
		if err != nil {
			_ = os.Remove(part)
			s.pfail(w, err)
			return
		}
		w.Header().Set(UploadResultHeader, result)
		answer(http.StatusCreated, UploadCreated{Length: 0, Status: UploadStored})
		return
	}
	if err := s.store.db.Create(&up).Error; err != nil {
		_ = os.Remove(part)
		s.pfail(w, err)
		return
	}
	s.bumpSession(ses.ID, nil)
	answer(http.StatusCreated, UploadCreated{UploadID: up.ID, Location: s.uploadLocation(d.ID, up.ID), Offset: 0, Length: length, ExpiresAt: up.ExpiresAt, Status: UploadCreatedStatus})
}

// uploadOf loads an upload of the requesting device; a void one (older
// location) is dropped and reported missing.
func (s *Service) uploadOf(r *http.Request) (PhoneUploadRow, DeviceRow, error) {
	d := deviceOf(r)
	var up PhoneUploadRow
	err := s.store.db.Where("id = ? AND device_id = ?", r.PathValue("uid"), d.ID).Take(&up).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return up, d, fmt.Errorf("upload %s: %w", r.PathValue("uid"), errNoRecord)
	}
	if err != nil {
		return up, d, err
	}
	if fresh, err := s.store.GetDevice(d.ID); err == nil {
		d = fresh
	}
	if up.DestGen != d.DestGen {
		_ = s.store.db.Delete(&PhoneUploadRow{}, "id = ?", up.ID).Error
		return up, d, fmt.Errorf("upload %s (the backup location changed): %w", up.ID, errNoRecord)
	}
	return up, d, nil
}

func (s *Service) handleUploadHead(w http.ResponseWriter, r *http.Request) {
	tusHeaders(w)
	up, _, err := s.uploadOf(r)
	if err != nil {
		if isNoRecord(err) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Upload-Offset", strconv.FormatInt(up.Offset, 10))
	w.Header().Set("Upload-Length", strconv.FormatInt(up.Length, 10))
	w.WriteHeader(http.StatusOK)
}

func (s *Service) handleUploadDelete(w http.ResponseWriter, r *http.Request) {
	tusHeaders(w)
	up, d, err := s.uploadOf(r)
	if err != nil {
		s.pfail(w, err)
		return
	}
	root, err := s.phoneRoot(r.Context(), &d, false)
	if err != nil && !errors.Is(err, errNoFolder) {
		// Offline: forget the row; the part file goes with the next purge.
		root = ""
	}
	s.dropUpload(root, up)
	w.WriteHeader(http.StatusNoContent)
}

// dropUpload removes an upload's row and part file (root "" = row only).
func (s *Service) dropUpload(root string, up PhoneUploadRow) {
	if err := s.store.db.Delete(&PhoneUploadRow{}, "id = ?", up.ID).Error; err != nil {
		log.Printf("backup: drop upload %s: %v", up.ID, err)
	}
	if root != "" {
		_ = os.Remove(uploadPart(root, up.ID))
	}
	s.phone.forgetUpload(up.ID)
}

func (s *Service) handleUploadPatch(w http.ResponseWriter, r *http.Request) {
	tusHeaders(w)
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, tusContentType) {
		writeError(w, http.StatusUnsupportedMediaType, ErrorBody{ErrorCode: ErrValidation, Detail: "Content-Type must be " + tusContentType})
		return
	}
	if r.ContentLength > PhoneChunkSize {
		writeError(w, http.StatusRequestEntityTooLarge, ErrorBody{ErrorCode: ErrTooLarge, Detail: fmt.Sprintf("at most %d bytes per PATCH", PhoneChunkSize)})
		return
	}
	offset, err := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
	if err != nil || offset < 0 {
		writeValidation(w, map[string]string{"Upload-Offset": string(FieldInvalid)})
		return
	}
	up, d, err := s.uploadOf(r)
	if err != nil {
		s.pfail(w, err)
		return
	}
	ul := s.phone.upLock(up.ID)
	if !ul.TryLock() {
		s.pfail(w, errConflict("another PATCH of upload %s is still running", up.ID))
		return
	}
	defer ul.Unlock()
	// Reload under the lock: the offset may have moved.
	if err := s.store.db.Where("id = ?", up.ID).Take(&up).Error; err != nil {
		s.pfail(w, fmt.Errorf("upload %s: %w", up.ID, errNoRecord))
		return
	}
	ses, err := s.sessionOf(d, up.SessionID)
	if err != nil {
		s.pfail(w, err)
		return
	}
	root, err := s.writableRoot(r.Context(), &d)
	if err != nil {
		s.pfail(w, err)
		return
	}
	part := uploadPart(root, up.ID)
	fi, err := os.Lstat(part)
	if err != nil || !fi.Mode().IsRegular() {
		s.dropUpload(root, up)
		s.pfail(w, fmt.Errorf("upload %s (its data is gone; start it again): %w", up.ID, errNoRecord))
		return
	}
	if fi.Size() != up.Offset {
		// A crash between writing and recording: keep what was recorded.
		if fi.Size() < up.Offset {
			up.Offset, up.HashState = 0, nil
		}
		if err := os.Truncate(part, up.Offset); err != nil {
			s.pfail(w, engine.Errorf(ErrIOError, "%s: %v", part, err))
			return
		}
	}
	if offset != up.Offset {
		w.Header().Set("Upload-Offset", strconv.FormatInt(up.Offset, 10))
		writeError(w, http.StatusConflict, ErrorBody{ErrorCode: ErrOffsetMismatch, Detail: fmt.Sprintf("the upload is at %d, not %d", up.Offset, offset)})
		return
	}
	h := sha256.New()
	if len(up.HashState) > 0 {
		if err := h.(encoding.BinaryUnmarshaler).UnmarshalBinary(up.HashState); err != nil {
			up.HashState = nil
		}
	}
	if len(up.HashState) == 0 && up.Offset > 0 {
		// Rebuild the running hash from what is on disk.
		if err := hashPrefix(h, part, up.Offset); err != nil {
			s.pfail(w, engine.Errorf(ErrIOError, "%s: %v", part, err))
			return
		}
	}
	max := up.Length - up.Offset
	if max > PhoneChunkSize {
		max = PhoneChunkSize
	}
	f, err := os.OpenFile(part, os.O_WRONLY|os.O_APPEND, phoneFileMode)
	if err != nil {
		s.pfail(w, engine.Errorf(ErrIOError, "%s: %v", part, err))
		return
	}
	n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(r.Body, max))
	var extra [1]byte
	tooLong := false
	if copyErr == nil && up.Offset+n == up.Length {
		if k, _ := r.Body.Read(extra[:]); k > 0 {
			tooLong = true
		}
	} else if copyErr == nil && n == max && max < up.Length-up.Offset {
		if k, _ := r.Body.Read(extra[:]); k > 0 {
			tooLong = true
		}
	}
	syncErr := f.Sync()
	f.Close()
	if tooLong {
		_ = os.Truncate(part, up.Offset)
		writeError(w, http.StatusRequestEntityTooLarge, ErrorBody{ErrorCode: ErrTooLarge, Detail: "the body runs past Upload-Length or the chunk size"})
		return
	}
	if syncErr != nil && copyErr == nil {
		copyErr = syncErr
	}
	if n > 0 {
		state, _ := h.(encoding.BinaryMarshaler).MarshalBinary()
		up.Offset += n
		up.HashState, up.HashedLen = state, up.Offset
		now := s.now().UTC()
		up.UpdatedAt, up.ExpiresAt = now, now.Add(PhoneUploadExpiry)
		if err := s.store.db.Model(&PhoneUploadRow{}).Where("id = ?", up.ID).Updates(map[string]interface{}{
			"offset": up.Offset, "hash_state": up.HashState, "hashed_len": up.HashedLen, "updated_at": up.UpdatedAt, "expires_at": up.ExpiresAt,
		}).Error; err != nil {
			_ = os.Truncate(part, up.Offset-n)
			s.pfail(w, err)
			return
		}
		s.bumpSession(ses.ID, map[string]int64{"bytes": n})
		s.liveAdd(ses.RunID, n, 0)
	}
	w.Header().Set("Upload-Offset", strconv.FormatInt(up.Offset, 10))
	if copyErr != nil {
		// The connection broke: what arrived is kept; the phone resumes.
		log.Printf("backup: upload %s: %v (kept %d bytes)", up.ID, copyErr, up.Offset)
		writeError(w, http.StatusBadRequest, ErrorBody{ErrorCode: ErrIOError, Detail: "the chunk was cut short; resume at Upload-Offset"})
		return
	}
	if up.Offset < up.Length {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != up.SHA256 {
		s.dropUpload(root, up)
		writeError(w, http.StatusUnprocessableEntity, ErrorBody{ErrorCode: ErrChecksumMismatch, Detail: fmt.Sprintf("the file's SHA-256 is %s, not %s; upload it again", got, up.SHA256)})
		return
	}
	lock := s.phone.devLock(d.ID)
	lock.Lock()
	result, err := s.finishUpload(root, d, ses, up, part)
	lock.Unlock()
	if err != nil {
		s.pfail(w, err)
		return
	}
	s.phone.forgetUpload(up.ID)
	w.Header().Set(UploadResultHeader, result)
	w.WriteHeader(http.StatusNoContent)
}

// liveAdd moves a session's run's live counters.
func (s *Service) liveAdd(runID string, bytes, files int64) {
	s.liveMu.Lock()
	st := s.lives[runID]
	st.Bytes += bytes
	st.Files += files
	if st.TotalBytes < st.Bytes {
		st.TotalBytes = st.Bytes
	}
	s.lives[runID] = st
	s.liveMu.Unlock()
}

func hashPrefix(h hash.Hash, p string, n int64) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.CopyN(h, f, n)
	return err
}

// finishUpload places a complete, verified upload (device lock held).
func (s *Service) finishUpload(root string, d DeviceRow, ses PhoneSessionRow, up PhoneUploadRow, part string) (string, error) {
	cat := PhoneCategory(up.Category)
	if err := s.checkRootNow(d, root); err != nil {
		return "", err
	}
	var result string
	var err error
	if cat.isFiles() {
		err = s.placeFile(root, d, cat, up.Path, part, fileMeta{Size: up.Length, MTime: up.MTime, SHA256: up.SHA256, MediaID: up.MediaID, TakenAt: up.TakenAt}, false)
		result = UploadStored
		if err == nil {
			s.bumpSession(ses.ID, map[string]int64{"uploaded": 1})
		}
	} else {
		result, err = s.placeExport(root, d, ses.ID, cat, up, part, false)
		if err == nil && result == UploadStored {
			s.bumpSession(ses.ID, map[string]int64{"uploaded": 1, "exports": 1})
		}
	}
	if err != nil {
		return "", err
	}
	if up.ID != "" {
		_ = s.store.db.Delete(&PhoneUploadRow{}, "id = ?", up.ID).Error
	}
	s.liveAdd(ses.RunID, 0, 1)
	return result, nil
}

// checkRootNow is checkRoot against the cached root of d.
func (s *Service) checkRootNow(d DeviceRow, root string) error {
	s.phone.mu.Lock()
	c, ok := s.phone.roots[d.ID]
	s.phone.mu.Unlock()
	if !ok || c.root != root {
		return engine.Errorf(ErrDestOffline, "the phone's backup folder changed")
	}
	return s.checkRoot(d.ID, root, c.dev)
}

// fileMeta is what the index keeps about a file.
type fileMeta struct {
	Size, MTime      int64
	SHA256           string
	MediaID, TakenAt int64
}

// safeDir makes the folder rel (slash separated, under root) and checks
// that no part of it is a symbolic link or a file.
func safeDir(root, rel string) (string, error) {
	cur := root
	if rel == "" || rel == "." {
		return cur, nil
	}
	for _, seg := range strings.Split(rel, "/") {
		cur = filepath.Join(cur, seg)
		fi, err := os.Lstat(cur)
		switch {
		case err == nil:
			if fi.Mode()&fs.ModeSymlink != 0 || !fi.IsDir() {
				return "", engine.Errorf(ErrPathNotAllowed, "%s is not a plain folder", cur)
			}
		case errors.Is(err, fs.ErrNotExist):
			if err := os.Mkdir(cur, phoneDirMode); err != nil && !errors.Is(err, fs.ErrExist) {
				return "", engine.Errorf(ErrIOError, "creating %s: %v", cur, err)
			}
		default:
			return "", engine.Errorf(ErrIOError, "%s: %v", cur, err)
		}
	}
	return cur, nil
}

// versionPath is a free spot for a replaced file under the versions
// folder: .nivaro-versions/<ts>[-n]/<category>/<path>.
func versionPath(root string, now time.Time, cat PhoneCategory, p string) string {
	ts := now.UTC().Format(phoneTimeLayout)
	rel := path.Join(phoneVersionsDir, ts, string(cat), p)
	for i := 2; ; i++ {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel))); errors.Is(err, fs.ErrNotExist) {
			return rel
		}
		rel = path.Join(phoneVersionsDir, fmt.Sprintf("%s-%d", ts, i), string(cat), p)
	}
}

// placeFile moves tmp to the file's place in the backup; the content it
// replaces becomes a version. linked: the content came from the backup
// itself (check have_elsewhere).
func (s *Service) placeFile(root string, d DeviceRow, cat PhoneCategory, p, tmp string, m fileMeta, linked bool) error {
	stored := path.Join(cat.info().dir, p)
	dir, err := safeDir(root, path.Dir(stored))
	if err != nil {
		return err
	}
	abs := filepath.Join(dir, path.Base(stored))
	now := s.phoneNow()
	var old PhoneFileRow
	hasOld := s.store.db.Where("device_id = ? AND category = ? AND path = ?", d.ID, string(cat), p).Take(&old).Error == nil
	var version *PhoneVersionRow
	if fi, err := os.Lstat(abs); err == nil {
		if fi.IsDir() {
			return engine.Errorf(ErrPathNotAllowed, "%s is a folder in the backup", p)
		}
		vrel := versionPath(root, now, cat, p)
		vdir, err := safeDir(root, path.Dir(vrel))
		if err != nil {
			return err
		}
		if err := os.Rename(abs, filepath.Join(vdir, path.Base(vrel))); err != nil {
			return engine.Errorf(ErrIOError, "keeping the old %s: %v", p, err)
		}
		if hasOld && !old.Damaged {
			version = &PhoneVersionRow{
				DeviceID: d.ID, Category: string(cat), Path: p, Dir: old.Dir, Stored: vrel, Size: old.Size, MTime: old.MTime,
				SHA256: old.SHA256, MediaID: old.MediaID, TakenAt: old.TakenAt, Since: old.ContentSince, Until: now,
				DeletedOnDeviceAt: old.DeletedOnDeviceAt,
			}
		}
	}
	if err := os.Rename(tmp, abs); err != nil {
		return engine.Errorf(ErrIOError, "placing %s: %v", p, err)
	}
	_ = os.Chmod(abs, phoneFileMode)
	if m.MTime > 0 {
		mt := time.UnixMilli(m.MTime)
		_ = os.Chtimes(abs, mt, mt)
	}
	row := PhoneFileRow{
		DeviceID: d.ID, Category: string(cat), Path: p, Dir: phoneDirOf(p), Stored: stored, Size: m.Size, MTime: m.MTime,
		SHA256: m.SHA256, MediaID: m.MediaID, TakenAt: m.TakenAt, FirstSeen: now, ContentSince: now,
	}
	if hasOld {
		row.FirstSeen = old.FirstSeen
	}
	return s.store.db.Transaction(func(tx *gorm.DB) error {
		if version != nil {
			if err := tx.Create(version).Error; err != nil {
				return err
			}
		}
		return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error
	})
}

// purgeUploads drops uploads past their expiry and part files no row
// knows of.
func (s *Service) purgeUploads() {
	now := s.now().UTC()
	var rows []PhoneUploadRow
	if err := s.store.db.Where("expires_at < ?", now).Find(&rows).Error; err != nil {
		return
	}
	for _, up := range rows {
		root := ""
		if d, err := s.store.GetDevice(up.DeviceID); err == nil {
			if r, err := s.phoneRoot(s.ctx, &d, false); err == nil {
				root = r
			}
		}
		s.dropUpload(root, up)
	}
	devices, err := s.store.ListDevices()
	if err != nil {
		return
	}
	for _, d := range devices {
		root, err := s.phoneRoot(s.ctx, &d, false)
		if err != nil {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(root, phoneUploadsDir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			id := strings.TrimSuffix(e.Name(), ".part")
			var n int64
			s.store.db.Model(&PhoneUploadRow{}).Where("id = ?", id).Count(&n)
			if n > 0 {
				continue
			}
			if fi, err := e.Info(); err == nil && now.Sub(fi.ModTime()) > time.Hour {
				_ = os.Remove(filepath.Join(root, phoneUploadsDir, e.Name()))
			}
		}
	}
}
