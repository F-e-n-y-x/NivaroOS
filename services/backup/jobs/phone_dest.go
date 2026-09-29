package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"gorm.io/gorm"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// Where a phone's backups live (mobile plan §7.2). A phone's location is
// an endpoint - the folder that holds the phone's own folder - pinned on
// first use: by default the endpoint /DATA/Backup resolves to, or the
// folder the owner picked (POST /devices/:id/destination). The phone's
// folder is <location>/<DeviceRow.Folder>.
//
// The job side writes phone uploads itself, so it must never write to
// the wrong disk:
//
//   - the location is resolved by the engine (allowed roots, symlinks,
//     the right drive by UUID); offline or not local gives dest_offline
//     and nothing is created - there is no fallback folder;
//   - the folder's st_dev is remembered and checked again before every
//     write: when the drive is unplugged, the folder is gone (or is
//     another filesystem) and the write is refused;
//   - the folder carries .nivaroos-device.json with the device id; a
//     folder of another phone gives dest_marker_mismatch.

const (
	phoneRootTTL     = 30 * time.Second
	phoneMovingFile  = ".nivaro-moving"
	phoneDirMode     = 0o750
	phoneFileMode    = 0o640
	MoveStateMoving  = "moving"
	MoveStateFailed  = "failed"
	phoneMarkerV     = 1
	phoneLoopDefault = 5 * time.Minute
)

// phoneState is the in-memory side of phone backups.
type phoneState struct {
	mu       sync.Mutex
	devLocks map[string]*sync.Mutex
	upLocks  map[string]*sync.Mutex
	roots    map[string]phoneRootInfo
	moves    map[string]*DeviceMove
	checks   *rateLimiter
	verify   map[string]bool
	last     time.Time // phoneNow's previous stamp
}

type phoneRootInfo struct {
	root string
	dev  uint64
	gen  int
	at   time.Time
}

func newPhoneState() *phoneState {
	return &phoneState{
		devLocks: map[string]*sync.Mutex{}, upLocks: map[string]*sync.Mutex{},
		roots: map[string]phoneRootInfo{}, moves: map[string]*DeviceMove{},
		checks: newRateLimiter(PhoneCheckPerMinute, time.Minute), verify: map[string]bool{},
	}
}

// devLock serialises changes of one device's index and folder.
func (p *phoneState) devLock(id string) *sync.Mutex {
	p.mu.Lock()
	defer p.mu.Unlock()
	m := p.devLocks[id]
	if m == nil {
		m = &sync.Mutex{}
		p.devLocks[id] = m
	}
	return m
}

// upLock is one upload's lock (a PATCH at a time).
func (p *phoneState) upLock(id string) *sync.Mutex {
	p.mu.Lock()
	defer p.mu.Unlock()
	m := p.upLocks[id]
	if m == nil {
		m = &sync.Mutex{}
		p.upLocks[id] = m
	}
	return m
}

func (p *phoneState) forgetUpload(id string) {
	p.mu.Lock()
	delete(p.upLocks, id)
	p.mu.Unlock()
}

func (p *phoneState) forgetRoot(deviceID string) {
	p.mu.Lock()
	delete(p.roots, deviceID)
	p.mu.Unlock()
}

// phoneMarker is the content of .nivaroos-device.json.
type phoneMarker struct {
	V        int       `json:"v"`
	DeviceID string    `json:"device_id"`
	Name     string    `json:"name"`
	Created  time.Time `json:"created"`
}

// ---------------------------------------------------------------------
// DeviceRow JSON columns

func (r DeviceRow) destEndpoint() (Endpoint, bool) {
	if r.Dest == "" {
		return Endpoint{}, false
	}
	var ep Endpoint
	if json.Unmarshal([]byte(r.Dest), &ep) != nil {
		return Endpoint{}, false
	}
	return ep, true
}

func (r DeviceRow) settings() DeviceSettings {
	st := defaultDeviceSettings()
	if r.Settings != "" {
		_ = json.Unmarshal([]byte(r.Settings), &st)
	}
	return st
}

func (r DeviceRow) phoneSettings() *PhoneSettings {
	if r.PhoneSettings == "" {
		return nil
	}
	var ps PhoneSettings
	if json.Unmarshal([]byte(r.PhoneSettings), &ps) != nil {
		return nil
	}
	return &ps
}

func mustJSON(v interface{}) string {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

// updateDevice writes some columns of a device.
func (s *Service) updateDevice(id string, cols map[string]interface{}) error {
	res := s.store.db.Model(&DeviceRow{}).Where("id = ?", id).Updates(cols)
	if res.Error != nil {
		return fmt.Errorf("store: update device %s: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("device %s: %w", id, errNoRecord)
	}
	return nil
}

// ---------------------------------------------------------------------
// Resolution

// defaultDestEndpoint is the endpoint of the default location.
func (s *Service) defaultDestEndpoint(ctx context.Context) (Endpoint, error) {
	res, err := s.engine.ResolvePath(ctx, engine.ResolvePathRequest{Path: s.deviceRoot()})
	if err != nil {
		return Endpoint{}, err
	}
	if !res.OK || res.Endpoint == nil {
		code := res.Reason
		if code == "" {
			code = ErrEndpointUnknown
		}
		return Endpoint{}, engine.Errorf(code, "the default phone backup folder %s can't be used", s.deviceRoot())
	}
	return *res.Endpoint, nil
}

// pinDest gives d a location when it has none yet (the default).
func (s *Service) pinDest(ctx context.Context, d *DeviceRow) error {
	if d.Dest != "" {
		return nil
	}
	ep, err := s.defaultDestEndpoint(ctx)
	if err != nil {
		return err
	}
	raw := mustJSON(ep)
	if err := s.store.db.Model(&DeviceRow{}).Where("id = ? AND dest = ''", d.ID).
		Updates(map[string]interface{}{"dest": raw, "dest_default": true}).Error; err != nil {
		return fmt.Errorf("store: pin device destination: %w", err)
	}
	fresh, err := s.store.GetDevice(d.ID)
	if err != nil {
		return err
	}
	d.Dest, d.DestDefault = fresh.Dest, fresh.DestDefault
	return nil
}

func localKind(k EndpointKind) bool { return k == EPVolume || k == EPUSB || k == EPMerge }

// resolveLocation resolves a location endpoint to its local folder.
func (s *Service) resolveLocation(ctx context.Context, ep Endpoint) (engine.Resolved, error) {
	if !localKind(ep.Kind) {
		return engine.Resolved{}, engine.Errorf(ErrPathNotAllowed, "phone backups need a local drive (not %s)", ep.Kind)
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	res, err := s.engine.Resolve(cctx, engine.ResolveRequest{Endpoint: ep})
	if err != nil {
		return res, err
	}
	if !res.Online || res.LocalPath == "" {
		return res, engine.Errorf(ErrDestOffline, "the drive for phone backups (%s) is not connected", displayEndpoint(ep))
	}
	if !filepath.IsAbs(res.LocalPath) {
		return res, engine.Errorf(ErrPathNotAllowed, "the engine gave a relative path")
	}
	return res, nil
}

func displayEndpoint(ep Endpoint) string {
	if ep.Label != "" {
		return path.Join(ep.Label, ep.SubPath)
	}
	return path.Join(ep.RefID, ep.SubPath)
}

// errNoFolder: the device folder doesn't exist yet (nothing backed up).
var errNoFolder = errors.New("device folder not created yet")

// phoneRoot returns d's backup folder, online and checked. With create
// the folder and its marker are made when missing; without, a missing
// folder gives errNoFolder.
func (s *Service) phoneRoot(ctx context.Context, d *DeviceRow, create bool) (string, error) {
	s.phone.mu.Lock()
	c, ok := s.phone.roots[d.ID]
	s.phone.mu.Unlock()
	if ok && c.gen == d.DestGen && s.now().Sub(c.at) < phoneRootTTL {
		if err := s.checkRoot(d.ID, c.root, c.dev); err == nil {
			return c.root, nil
		}
	}
	if err := s.pinDest(ctx, d); err != nil {
		return "", err
	}
	ep, ok := d.destEndpoint()
	if !ok {
		return "", engine.Errorf(ErrEndpointUnknown, "device %s has an unreadable destination", d.ID)
	}
	res, err := s.resolveLocation(ctx, ep)
	if err != nil {
		return "", err
	}
	root := filepath.Join(res.LocalPath, d.Folder)
	if filepath.Dir(root) != filepath.Clean(res.LocalPath) {
		return "", engine.Errorf(ErrPathNotAllowed, "bad device folder %q", d.Folder)
	}
	fi, err := os.Lstat(root)
	switch {
	case err == nil:
		if !fi.IsDir() {
			return "", engine.Errorf(ErrPathNotAllowed, "%s is not a folder", root)
		}
	case errors.Is(err, fs.ErrNotExist):
		if !create {
			return root, errNoFolder
		}
		if err := os.MkdirAll(root, phoneDirMode); err != nil {
			return "", engine.Errorf(ErrIOError, "creating %s: %v", root, err)
		}
		if fi, err = os.Lstat(root); err != nil || !fi.IsDir() {
			return "", engine.Errorf(ErrIOError, "creating %s: %v", root, err)
		}
	default:
		return "", engine.Errorf(ErrIOError, "%s: %v", root, err)
	}
	if err := s.checkMarker(root, *d, create); err != nil {
		return "", err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return "", engine.Errorf(ErrIOError, "stat %s", root)
	}
	s.phone.mu.Lock()
	s.phone.roots[d.ID] = phoneRootInfo{root: root, dev: uint64(st.Dev), gen: d.DestGen, at: s.now()}
	s.phone.mu.Unlock()
	if d.DestPath != root {
		d.DestPath = root
		_ = s.updateDevice(d.ID, map[string]interface{}{"dest_path": root})
	}
	return root, nil
}

// checkRoot re-checks that root is still the folder resolved (same
// filesystem): the last guard before a write.
func (s *Service) checkRoot(deviceID, root string, dev uint64) error {
	fi, err := os.Lstat(root)
	if err == nil && fi.IsDir() {
		if st, ok := fi.Sys().(*syscall.Stat_t); ok && uint64(st.Dev) == dev {
			return nil
		}
	}
	s.phone.forgetRoot(deviceID)
	return engine.Errorf(ErrDestOffline, "the phone's backup folder %s is no longer there (drive removed?)", root)
}

// writableRoot is phoneRoot(create) plus the device check, for a write.
func (s *Service) writableRoot(ctx context.Context, d *DeviceRow) (string, error) {
	root, err := s.phoneRoot(ctx, d, true)
	if err != nil {
		return "", err
	}
	s.phone.mu.Lock()
	c := s.phone.roots[d.ID]
	s.phone.mu.Unlock()
	if c.root != root {
		return "", engine.Errorf(ErrDestOffline, "the phone's backup folder moved")
	}
	return root, s.checkRoot(d.ID, root, c.dev)
}

// checkMarker reads the folder's marker: another device's gives
// dest_marker_mismatch; none is written when write is set.
func (s *Service) checkMarker(root string, d DeviceRow, write bool) error {
	p := filepath.Join(root, phoneMarkerFile)
	raw, err := os.ReadFile(p)
	if err == nil {
		var m phoneMarker
		if json.Unmarshal(raw, &m) != nil || m.DeviceID == "" {
			return engine.Errorf(ErrDestMarkerMismatch, "%s has an unreadable %s", root, phoneMarkerFile)
		}
		if m.DeviceID != d.ID {
			return engine.Errorf(ErrDestMarkerMismatch, "%s holds the backups of another phone (%s)", root, m.Name)
		}
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return engine.Errorf(ErrIOError, "reading %s: %v", p, err)
	}
	if !write {
		return nil
	}
	raw, _ = json.MarshalIndent(phoneMarker{V: phoneMarkerV, DeviceID: d.ID, Name: d.Name, Created: s.now().UTC().Truncate(time.Second)}, "", "  ")
	if err := writeFileAtomic(p, append(raw, '\n'), phoneFileMode); err != nil {
		return engine.Errorf(ErrIOError, "writing %s: %v", p, err)
	}
	return nil
}

// phoneSpace is the free space at dir and the reserve kept on top.
func phoneSpace(dir string) (free, reserve int64, ok bool) {
	for {
		var st syscall.Statfs_t
		if err := syscall.Statfs(dir, &st); err == nil {
			free = int64(st.Bavail) * int64(st.Bsize)
			reserve = int64(st.Blocks) * int64(st.Bsize) * PhoneFreeReservePct / 100
			return free, reserve, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return 0, 0, false
		}
		dir = parent
	}
}

// needSpace refuses a write of need bytes that would eat the reserve.
func needSpace(root string, need int64) error {
	free, reserve, ok := phoneSpace(root)
	if !ok {
		return nil
	}
	if need+reserve > free {
		return engine.Errorf(ErrNoSpace, "not enough space for the phone backup: %d bytes needed, %d free (%d kept free)", need, free, reserve)
	}
	return nil
}

// destStatus reports d's location for the web UI and the phone.
func (s *Service) destStatus(ctx context.Context, d *DeviceRow) DeviceDestStatus {
	st := DeviceDestStatus{Default: d.DestDefault || d.Dest == "", Path: d.DestPath, Quirks: []Quirk{}}
	if err := s.pinDest(ctx, d); err != nil {
		st.ErrorCode, st.Detail = engine.CodeOf(err), err.Error()
		st.Default = true
		return st
	}
	st.Default = d.DestDefault
	if ep, ok := d.destEndpoint(); ok && !d.DestDefault {
		st.Location = &ep
	}
	if st.Path == "" {
		st.Path = d.DefaultDest
	}
	root, err := s.phoneRoot(ctx, d, false)
	if err != nil && !errors.Is(err, errNoFolder) {
		// Offline, or there but unusable (another phone's folder...).
		st.ErrorCode, st.Detail = engine.CodeOf(err), err.Error()
		st.Online = st.ErrorCode != ErrDestOffline
		return st
	}
	st.Online, st.Path = true, root
	if free, reserve, ok := phoneSpace(root); ok {
		st.FreeBytes, st.ReserveBytes = &free, reserve
	}
	if ep, ok := d.destEndpoint(); ok {
		if res, err := s.engine.Resolve(ctx, engine.ResolveRequest{Endpoint: ep}); err == nil && res.Quirks != nil {
			st.Quirks = res.Quirks
		}
	}
	return st
}

// Quirk is a filesystem limitation (engine.Quirk).
type Quirk = engine.Quirk

// ---------------------------------------------------------------------
// Changing the location

// errDestConflict answers 409 invalid_state with a reason.
func errConflict(format string, args ...interface{}) error {
	return fmt.Errorf(format+": %w", append(args, errInvalidState)...)
}

// deviceHasData reports whether anything was backed up for the device.
func (s *Service) deviceHasData(id string) (bool, error) {
	var n int64
	for _, m := range []interface{}{&PhoneFileRow{}, &PhoneExportRow{}} {
		if err := s.store.db.Model(m).Where("device_id = ?", id).Limit(1).Count(&n).Error; err != nil {
			return false, err
		}
		if n > 0 {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) openSessionCount(deviceID string) (int64, error) {
	var n int64
	err := s.store.db.Model(&PhoneSessionRow{}).Where("device_id = ? AND status = ?", deviceID, SessionOpen).Count(&n).Error
	return n, err
}

// pathInside reports whether a is b or below it.
func pathInside(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	return a == b || strings.HasPrefix(a, b+string(filepath.Separator))
}

// changeDest moves d to target (nil: the default) - see
// DeviceDestRequest. It returns once the switch happened, or once a
// background copy started (MoveState moving).
func (s *Service) changeDest(ctx context.Context, d DeviceRow, target *Endpoint, mode string) (DeviceRow, error) {
	lock := s.phone.devLock(d.ID)
	lock.Lock()
	defer lock.Unlock()
	d, err := s.store.GetDevice(d.ID)
	if err != nil {
		return d, err
	}
	if d.MoveState == MoveStateMoving {
		return d, errConflict("the backups of %s are being moved already", d.Name)
	}
	if n, err := s.openSessionCount(d.ID); err != nil {
		return d, err
	} else if n > 0 {
		return d, errConflict("%s is backing up right now; try again when it has finished", d.Name)
	}
	var ep Endpoint
	isDefault := target == nil
	if isDefault {
		if ep, err = s.defaultDestEndpoint(ctx); err != nil {
			return d, err
		}
	} else {
		ep = *target
	}
	res, err := s.resolveLocation(ctx, ep)
	if err != nil {
		return d, err
	}
	newRoot := filepath.Join(res.LocalPath, d.Folder)

	// The current folder, when it is reachable.
	oldRoot, oldErr := s.phoneRoot(ctx, &d, false)
	oldOnline := oldErr == nil
	if errors.Is(oldErr, errNoFolder) {
		oldOnline = false
	}
	if oldRoot != "" && oldErr == nil && filepath.Clean(oldRoot) == filepath.Clean(newRoot) {
		// Same place: only the default flag may change.
		raw := mustJSON(ep)
		if err := s.updateDevice(d.ID, map[string]interface{}{"dest": raw, "dest_default": isDefault}); err != nil {
			return d, err
		}
		return s.store.GetDevice(d.ID)
	}
	if oldRoot != "" && (pathInside(newRoot, oldRoot) || pathInside(oldRoot, newRoot)) {
		return d, engine.Errorf(ErrDestInsideSource, "the new location %s and the current one %s lie inside each other", newRoot, oldRoot)
	}
	others, err := s.store.ListDevices()
	if err != nil {
		return d, err
	}
	for _, o := range others {
		if o.ID == d.ID || o.DestPath == "" {
			continue
		}
		if pathInside(newRoot, o.DestPath) || pathInside(o.DestPath, newRoot) {
			return d, errConflict("%s is inside the backups of %s", newRoot, o.Name)
		}
	}
	jobs, err := s.store.ListJobs()
	if err != nil {
		return d, err
	}
	phoneEP := ep
	phoneEP.SubPath = path.Join(ep.SubPath, d.Folder)
	for _, j := range jobs {
		if endpointsOverlap(phoneEP, j.Dest) {
			return d, errConflict("%s overlaps the destination of the job %q", newRoot, j.Name)
		}
	}
	if entries, err := os.ReadDir(newRoot); err == nil && len(entries) > 0 {
		return d, errConflict("the folder %s already exists and is not empty", newRoot)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return d, engine.Errorf(ErrIOError, "%s: %v", newRoot, err)
	}
	has, err := s.deviceHasData(d.ID)
	if err != nil {
		return d, err
	}
	if has && mode == "" {
		return d, &fieldError{field: "mode", code: string(FieldRequired)}
	}
	if !has {
		mode = DestModeFresh
	}
	if mode == DestModeMove && !oldOnline {
		return d, engine.Errorf(ErrDestOffline, "the current backups of %s can't be moved: their drive is not connected", d.Name)
	}
	raw := mustJSON(ep)
	switch mode {
	case DestModeFresh:
		err = s.store.db.Transaction(func(tx *gorm.DB) error {
			if err := dropPhoneIndex(tx, d.ID, false); err != nil {
				return err
			}
			return tx.Model(&DeviceRow{}).Where("id = ?", d.ID).Updates(map[string]interface{}{
				"dest": raw, "dest_default": isDefault, "dest_gen": d.DestGen + 1, "dest_path": newRoot,
				"move_state": "", "move_error": "", "move_target": "", "move_target_path": "",
			}).Error
		})
		if err != nil {
			return d, fmt.Errorf("store: switch device destination: %w", err)
		}
		if oldOnline {
			_ = os.RemoveAll(filepath.Join(oldRoot, phoneUploadsDir))
		}
		s.phone.forgetRoot(d.ID)
		return s.store.GetDevice(d.ID)
	case DestModeMove:
		if err := s.store.db.Where("device_id = ?", d.ID).Delete(&PhoneUploadRow{}).Error; err != nil {
			return d, err
		}
		_ = os.RemoveAll(filepath.Join(oldRoot, phoneUploadsDir))
		if err := os.MkdirAll(filepath.Dir(newRoot), phoneDirMode); err != nil {
			return d, engine.Errorf(ErrIOError, "creating %s: %v", filepath.Dir(newRoot), err)
		}
		_ = os.Remove(newRoot) // an empty folder, checked above
		if err := os.Rename(oldRoot, newRoot); err == nil {
			if err := s.finishMove(d, raw, isDefault, newRoot, ""); err != nil {
				return d, err
			}
			return s.store.GetDevice(d.ID)
		} else if !errors.Is(err, syscall.EXDEV) {
			return d, engine.Errorf(ErrIOError, "moving %s to %s: %v", oldRoot, newRoot, err)
		}
		// Another drive: copy in the background.
		if err := needSpace(filepath.Dir(newRoot), dirSize(oldRoot)); err != nil {
			return d, err
		}
		if err := os.MkdirAll(newRoot, phoneDirMode); err != nil {
			return d, engine.Errorf(ErrIOError, "creating %s: %v", newRoot, err)
		}
		if err := os.WriteFile(filepath.Join(newRoot, phoneMovingFile), []byte(d.ID+"\n"), phoneFileMode); err != nil {
			return d, engine.Errorf(ErrIOError, "%s: %v", newRoot, err)
		}
		now := s.now().UTC()
		if err := s.updateDevice(d.ID, map[string]interface{}{
			"move_state": MoveStateMoving, "move_error": "", "move_target": raw, "move_target_path": newRoot, "move_started_at": &now,
		}); err != nil {
			return d, err
		}
		mv := &DeviceMove{State: MoveStateMoving, Target: &ep, TargetPath: newRoot, StartedAt: now}
		mv.TotalFiles, mv.TotalBytes = dirCount(oldRoot)
		s.phone.mu.Lock()
		s.phone.moves[d.ID] = mv
		s.phone.mu.Unlock()
		dcopy := d
		s.goRun(func() { s.copyMove(dcopy, oldRoot, newRoot, raw, isDefault, mv) })
		return s.store.GetDevice(d.ID)
	}
	return d, &fieldError{field: "mode", code: string(FieldInvalid)}
}

// fieldError is a single-field validation failure from deep inside.
type fieldError struct{ field, code string }

func (e *fieldError) Error() string { return e.field + ": " + e.code }

// finishMove switches the device to its new folder.
func (s *Service) finishMove(d DeviceRow, raw string, isDefault bool, newRoot, oldRoot string) error {
	err := s.store.db.Model(&DeviceRow{}).Where("id = ?", d.ID).Updates(map[string]interface{}{
		"dest": raw, "dest_default": isDefault, "dest_gen": d.DestGen + 1, "dest_path": newRoot,
		"move_state": "", "move_error": "", "move_target": "", "move_target_path": "", "move_started_at": nil,
	}).Error
	s.phone.forgetRoot(d.ID)
	s.phone.mu.Lock()
	delete(s.phone.moves, d.ID)
	s.phone.mu.Unlock()
	if err != nil {
		return fmt.Errorf("store: switch device destination: %w", err)
	}
	if oldRoot != "" {
		if err := os.RemoveAll(oldRoot); err != nil {
			log.Printf("backup: device %s: removing the old folder %s: %v", d.ID, oldRoot, err)
		}
	}
	_ = os.Remove(filepath.Join(newRoot, phoneMovingFile))
	s.deviceChanged(d.ID, DeviceChangeMoved)
	return nil
}

// copyMove copies a phone's folder to another drive, then switches.
func (s *Service) copyMove(d DeviceRow, oldRoot, newRoot, raw string, isDefault bool, mv *DeviceMove) {
	lock := s.phone.devLock(d.ID)
	err := copyTree(s.ctx, oldRoot, newRoot, func(files, bytes int64) {
		s.phone.mu.Lock()
		mv.Files, mv.Bytes = files, bytes
		s.phone.mu.Unlock()
	})
	lock.Lock()
	defer lock.Unlock()
	if err == nil {
		err = s.finishMove(d, raw, isDefault, newRoot, oldRoot)
		if err == nil {
			return
		}
	}
	log.Printf("backup: device %s: moving the backups to %s failed: %v", d.ID, newRoot, err)
	_ = os.RemoveAll(newRoot)
	_ = s.updateDevice(d.ID, map[string]interface{}{"move_state": MoveStateFailed, "move_error": err.Error()})
	s.phone.mu.Lock()
	delete(s.phone.moves, d.ID)
	s.phone.mu.Unlock()
	s.deviceChanged(d.ID, DeviceChangeMoveFailed)
}

// recoverMoves cleans up copies a restart cut short: the half-copied
// folder goes, the backups stay where they were, the move is failed.
func (s *Service) recoverMoves() {
	var rows []DeviceRow
	if err := s.store.db.Where("move_state = ?", MoveStateMoving).Find(&rows).Error; err != nil {
		log.Printf("backup: recover device moves: %v", err)
		return
	}
	for _, d := range rows {
		if d.MoveTargetPath != "" {
			if raw, err := os.ReadFile(filepath.Join(d.MoveTargetPath, phoneMovingFile)); err == nil && strings.TrimSpace(string(raw)) == d.ID {
				_ = os.RemoveAll(d.MoveTargetPath)
			}
		}
		_ = s.updateDevice(d.ID, map[string]interface{}{"move_state": MoveStateFailed, "move_error": "interrupted by a restart"})
	}
}

func dirSize(root string) int64 {
	_, b := dirCount(root)
	return b
}

func dirCount(root string) (files, bytes int64) {
	_ = filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if e.Type().IsRegular() {
			if fi, err := e.Info(); err == nil {
				files++
				bytes += fi.Size()
			}
		}
		return nil
	})
	return files, bytes
}

// copyTree copies the regular files and folders of src into dst
// (existing), keeping modification times. Symbolic links are skipped:
// phone backups never make any.
func copyTree(ctx context.Context, src, dst string, progress func(files, bytes int64)) error {
	var files, total int64
	return filepath.WalkDir(src, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if rel == phoneUploadsDir && e.IsDir() {
			return filepath.SkipDir
		}
		target := filepath.Join(dst, rel)
		fi, err := e.Info()
		if err != nil {
			return err
		}
		switch {
		case e.IsDir():
			if rel == "." {
				return nil
			}
			return os.MkdirAll(target, phoneDirMode)
		case e.Type().IsRegular():
			if err := copyFile(p, target, fi.ModTime()); err != nil {
				return err
			}
			files++
			total += fi.Size()
			progress(files, total)
		}
		return nil
	})
}

func copyFile(src, dst string, mtime time.Time) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, phoneFileMode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(dst, mtime, mtime)
}

// dropPhoneIndex removes a device's index rows; the excluded list stays
// unless all.
func dropPhoneIndex(tx *gorm.DB, deviceID string, all bool) error {
	tables := []interface{}{&PhoneFileRow{}, &PhoneVersionRow{}, &PhoneExportRow{}, &PhoneSnapshotRow{}, &PhoneUploadRow{}, &PhoneItemRow{}}
	if all {
		tables = append(tables, &PhoneExcludedRow{}, &PhoneSessionRow{})
	}
	for _, m := range tables {
		if err := tx.Where("device_id = ?", deviceID).Delete(m).Error; err != nil {
			return err
		}
	}
	return nil
}
