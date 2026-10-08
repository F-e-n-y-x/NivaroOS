package v1

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/core/service"
	"github.com/F-e-n-y-x/NivaroOS/services/core/service/trash"
)

// Phone files deleted through NivaroOS go to a trash folder on the phone
// itself: one rename on the phone's own storage (instant, nothing is
// copied) into <volume>/.nivaroos-trash/<id>/<name>. Android's media
// scanner skips dot folders, so a trashed photo leaves the gallery too.
// Android's own trash (MediaStore.createTrashRequest) isn't used: the
// phone would ask its user to confirm every single delete.
//
// The records live here on the server, so an item stays listed - as
// unavailable - while its phone is away, and the phone app needs nothing
// new (it already has /mkdir, /rename and /delete).

const phoneTrashDir = ".nivaroos-trash"

type companionTrashRec struct {
	trash.Item
	DeviceID  string `json:"device_id"`
	Owner     string `json:"owner,omitempty"`
	PhonePath string `json:"phone_path"` // where it was on the phone
	HeldAt    string `json:"held_at"`    // where it is now
}

var (
	companionTrashMu sync.Mutex
	phoneVolume      = regexp.MustCompile(`^/storage/(emulated/\d+|[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4})`)
)

func init() { service.PurgeMore = purgeCompanionTrash }

func companionTrashFile() string { return filepath.Join(companionStateDir, "companion-trash.json") }

func readCompanionTrashLocked() []companionTrashRec {
	var recs []companionTrashRec
	if raw, err := os.ReadFile(companionTrashFile()); err == nil {
		_ = json.Unmarshal(raw, &recs)
	}
	return recs
}

func companionTrashRecs() []companionTrashRec {
	companionTrashMu.Lock()
	defer companionTrashMu.Unlock()
	return readCompanionTrashLocked()
}

// editCompanionTrash runs fn on the records and saves what it returns.
func editCompanionTrash(fn func([]companionTrashRec) []companionTrashRec) error {
	companionTrashMu.Lock()
	defer companionTrashMu.Unlock()
	raw, _ := json.Marshal(fn(readCompanionTrashLocked()))
	_ = os.MkdirAll(companionStateDir, 0o755)
	tmp := companionTrashFile() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, companionTrashFile())
}

func dropCompanionTrash(id string) {
	_ = editCompanionTrash(func(recs []companionTrashRec) []companionTrashRec {
		out := recs[:0]
		for _, r := range recs {
			if r.ID != id {
				out = append(out, r)
			}
		}
		return out
	})
}

func phoneVolumeOf(dev *CompanionDevice, p string) string {
	if v := phoneVolume.FindString(p); v != "" {
		return v
	}
	if dev.RootPath != "" {
		return dev.RootPath
	}
	return "/storage/emulated/0"
}

// trashOnCompanion moves phonePath (storagePath in NivaroOS) into the
// phone's trash. ok is false when there was nothing there to trash.
// listed caches folder listings across one request's items (listing DCIM
// can take seconds).
func trashOnCompanion(dev *CompanionDevice, storagePath, phonePath string, listed map[string][]CompanionFileItem) (it trash.Item, ok bool, err error) {
	if trash.IsTrashPath(phonePath) {
		return it, false, errors.New("that's the Trash itself")
	}
	parent, name := path.Split(phonePath)
	parent = path.Clean(parent)
	files, cached := listed[dev.ID+":"+parent]
	if !cached {
		if files, err = FetchCompanionFilesFromDevice(dev, parent); err != nil {
			return it, false, err
		}
		listed[dev.ID+":"+parent] = files
	}
	var found *CompanionFileItem
	for i := range files {
		if files[i].Name == name {
			found = &files[i]
		}
	}
	if found == nil {
		return it, false, nil
	}
	id := trash.NewID()
	holder := path.Join(phoneVolumeOf(dev, phonePath), phoneTrashDir, id)
	rec := companionTrashRec{
		Item: trash.Item{
			ID: id, Name: name, OriginalPath: storagePath, DeletedAt: time.Now(), IsDir: found.IsDir,
			Kind: trash.KindPhone, Location: companionDisplayName(dev),
		},
		DeviceID: dev.ID, Owner: dev.OwnerUserID, PhonePath: phonePath, HeldAt: path.Join(holder, name),
	}
	if !found.IsDir {
		rec.Size = found.Size
	}
	if err := companionSimple(dev, http.MethodPost, "/mkdir", url.Values{"path": {holder}}, 30*time.Second); err != nil {
		return it, false, err
	}
	// Record first: if the move then fails the record goes again; the
	// reverse order could lose track of a trashed file.
	if err := editCompanionTrash(func(r []companionTrashRec) []companionTrashRec { return append(r, rec) }); err != nil {
		return it, false, err
	}
	if err := companionSimple(dev, http.MethodPost, "/rename", url.Values{"old_path": {phonePath}, "new_path": {rec.HeldAt}}, 30*time.Second); err != nil {
		dropCompanionTrash(id)
		_ = ProxyCompanionFileDelete(dev, holder)
		return it, false, err
	}
	return rec.Item, true, nil
}

// companionTrashDevice: the record's phone (a snapshot), nil once it was
// removed from NivaroOS; visible says whether uid may see the record.
func companionTrashDevice(rec companionTrashRec, uid string) (dev *CompanionDevice, visible bool) {
	companionMu.Lock()
	loadCompanionDevicesLocked()
	if d, ok := companionDevices[rec.DeviceID]; ok {
		dev = d.snapshot()
	}
	companionMu.Unlock()
	if dev != nil {
		return dev, companionVisibleTo(dev, uid)
	}
	return nil, uid == "" || rec.Owner == "" || rec.Owner == uid
}

// companionTrashList: uid's phone items, an offline phone's marked
// unavailable.
func companionTrashList(uid string) []trash.Item {
	out := []trash.Item{}
	for _, rec := range companionTrashRecs() {
		dev, visible := companionTrashDevice(rec, uid)
		if !visible {
			continue
		}
		it := rec.Item
		it.Unavailable = dev == nil || !companionOnlineRemotely(dev)
		if dev != nil {
			it.Location = companionDisplayName(dev)
		}
		out = append(out, it)
	}
	return out
}

// pick splits ids into uid's phone records and the rest (local Trash).
func pickCompanionTrash(ids []string, uid string) (mine []companionTrashRec, rest []string) {
	byID := map[string]companionTrashRec{}
	for _, r := range companionTrashRecs() {
		byID[r.ID] = r
	}
	for _, id := range ids {
		if r, ok := byID[id]; ok {
			if _, visible := companionTrashDevice(r, uid); visible {
				mine = append(mine, r)
			}
			continue
		}
		rest = append(rest, id)
	}
	return mine, rest
}

var errPhoneRemoved = errors.New("that phone was removed from NivaroOS")

func restoreCompanionTrash(rec companionTrashRec, uid string) (string, error) {
	dev, _ := companionTrashDevice(rec, uid)
	if dev == nil {
		return "", errPhoneRemoved
	}
	parent := path.Dir(rec.PhonePath)
	if parent != phoneVolumeOf(dev, rec.PhonePath) {
		if err := companionSimple(dev, http.MethodPost, "/mkdir", url.Values{"path": {parent}}, 30*time.Second); err != nil {
			return "", errors.New(companionUnreachableMessage(dev, err))
		}
	}
	// Never over something that has the name now: "name (restored)".
	taken := map[string]bool{}
	files, err := FetchCompanionFilesFromDevice(dev, parent)
	if err != nil {
		return "", errors.New(companionUnreachableMessage(dev, err))
	}
	for _, f := range files {
		taken[f.Name] = true
	}
	dest := rec.PhonePath
	for i := 1; taken[path.Base(dest)]; i++ {
		dest = trash.RestoredName(rec.PhonePath, i)
	}
	if err := companionSimple(dev, http.MethodPost, "/rename", url.Values{"old_path": {rec.HeldAt}, "new_path": {dest}}, 30*time.Second); err != nil {
		return "", err
	}
	_ = deleteCompanionHolder(dev, rec)
	dropCompanionTrash(rec.ID)
	return filepath.Join(filepath.Dir(rec.OriginalPath), path.Base(dest)), nil
}

// deleteCompanionHolder deletes rec's <volume>/.nivaroos-trash/<id> on the
// phone - only ever a folder of that shape, whatever the record says.
func deleteCompanionHolder(dev *CompanionDevice, rec companionTrashRec) error {
	holder := path.Dir(rec.HeldAt)
	if path.Base(holder) != rec.ID || rec.ID == "" || path.Base(path.Dir(holder)) != phoneTrashDir {
		return errors.New("bad trash record")
	}
	return ProxyCompanionFileDelete(dev, holder)
}

func deleteCompanionTrash(rec companionTrashRec, uid string) error {
	dev, _ := companionTrashDevice(rec, uid)
	if dev != nil {
		if err := deleteCompanionHolder(dev, rec); err != nil {
			return errors.New(companionUnreachableMessage(dev, err))
		}
	}
	// A removed phone: nothing to reach any more, the record just goes.
	dropCompanionTrash(rec.ID)
	return nil
}

// purgeCompanionTrash deletes phone items older than maxAge; an offline
// phone's wait for the next round.
func purgeCompanionTrash(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)
	for _, rec := range companionTrashRecs() {
		if !rec.DeletedAt.Before(cutoff) {
			continue
		}
		if dev, _ := companionTrashDevice(rec, ""); dev == nil || companionOnlineRemotely(dev) {
			_ = deleteCompanionTrash(rec, "")
		}
	}
}

func sortTrash(items []trash.Item) {
	sort.Slice(items, func(i, j int) bool { return items[i].DeletedAt.After(items[j].DeletedAt) })
}
