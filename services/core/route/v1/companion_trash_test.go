package v1

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/core/service"
	"github.com/F-e-n-y-x/NivaroOS/services/core/service/trash"
)

// phoneFS is the phone's file server over a real temp dir: phone path
// /storage/emulated/0/x is <dir>/storage/emulated/0/x. It behaves like
// mobile/lib/services/companion_file_server.dart (rename refuses to
// overwrite, a folder rename needs its parent).
type phoneFS struct {
	id, secret, dir string
}

func (p *phoneFS) local(q string) string { return filepath.Join(p.dir, filepath.Clean("/"+q)) }

func (p *phoneFS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	reply := func(code int, ok bool, msg string) {
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": ok, "message": msg})
	}
	if r.URL.Path == "/hello" {
		json.NewEncoder(w).Encode(map[string]string{"id": p.id, "proof": companionHelloProof(p.secret, p.id, r.URL.Query().Get("nonce"))})
		return
	}
	if r.Header.Get("X-Companion-Secret") != p.secret {
		reply(401, false, "Unauthorized")
		return
	}
	q := r.URL.Query()
	switch r.URL.Path {
	case "/files":
		entries, err := os.ReadDir(p.local(q.Get("path")))
		if err != nil {
			reply(404, false, "Directory not found")
			return
		}
		files := []map[string]interface{}{}
		for _, e := range entries {
			fi, _ := e.Info()
			files = append(files, map[string]interface{}{"name": e.Name(), "is_dir": e.IsDir(), "size": fi.Size()})
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "files": files})
	case "/mkdir":
		os.MkdirAll(p.local(q.Get("path")), 0o755)
		reply(200, true, "")
	case "/delete":
		os.RemoveAll(p.local(q.Get("path")))
		reply(200, true, "")
	case "/rename":
		from, to := p.local(q.Get("old_path")), p.local(q.Get("new_path"))
		if _, err := os.Lstat(to); err == nil {
			reply(409, false, "Something with that name already exists")
			return
		}
		if err := os.Rename(from, to); err != nil {
			reply(404, false, "Path not found")
			return
		}
		reply(200, true, "")
	default:
		reply(404, false, "Not found")
	}
}

func phoneTrashSetup(t *testing.T) (*CompanionDevice, *phoneFS, string) {
	t.Helper()
	_, base := useTempCompanionState(t)
	useLoopbackAddressKinds(t)
	port := freePort(t)
	ph := &phoneFS{id: "ph1", secret: "s3cret", dir: t.TempDir()}
	listenOn(t, ph, port, "127.0.0.1")
	storage := filepath.Join(base, "Pixel")
	dev := &CompanionDevice{ID: "ph1", Name: "Pixel", Secret: "s3cret", Port: port, IP: "127.0.0.1", StoragePath: storage, RootPath: "/storage/emulated/0", OwnerUserID: "1", LastSeen: time.Now()}
	companionMu.Lock()
	companionLoaded = true
	companionDevices[dev.ID] = dev
	companionMu.Unlock()

	oldBin := service.Trash
	service.Trash = trash.New(trash.Options{IndexPath: filepath.Join(t.TempDir(), "roots.json")})
	t.Cleanup(func() { service.Trash = oldBin })
	return dev, ph, storage
}

func dirs() map[string][]CompanionFileItem { return map[string][]CompanionFileItem{} }

func phoneWrite(t *testing.T, ph *phoneFS, p, s string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(ph.local(p)), 0o755)
	if err := os.WriteFile(ph.local(p), []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

type trashListResp struct {
	Data struct {
		Items []trash.Item `json:"items"`
		Bytes int64        `json:"bytes"`
	} `json:"data"`
}

func listTrash(t *testing.T, uid int) trashListResp {
	t.Helper()
	var r trashListResp
	json.Unmarshal(companionReq(t, GetTrash, "GET", "/v1/trash", uid, "", nil).Body.Bytes(), &r)
	return r
}

func TestPhoneTrashRestoreAndDeleteForever(t *testing.T) {
	dev, ph, storage := phoneTrashSetup(t)
	phoneWrite(t, ph, "/storage/emulated/0/DCIM/a.jpg", "AAAA")
	phoneWrite(t, ph, "/storage/emulated/0/Music/album/1.mp3", "M")

	it, ok, err := trashOnCompanion(dev, filepath.Join(storage, "DCIM/a.jpg"), "/storage/emulated/0/DCIM/a.jpg", dirs())
	if err != nil || !ok || it.Size != 4 || it.Kind != trash.KindPhone {
		t.Fatalf("trash file: %+v %v %v", it, ok, err)
	}
	dir, ok, err := trashOnCompanion(dev, filepath.Join(storage, "Music/album"), "/storage/emulated/0/Music/album", dirs())
	if err != nil || !ok || !dir.IsDir {
		t.Fatalf("trash folder: %+v %v %v", dir, ok, err)
	}
	if _, err := os.Stat(ph.local("/storage/emulated/0/DCIM/a.jpg")); !os.IsNotExist(err) {
		t.Fatal("file still in place on the phone")
	}
	if _, err := os.Stat(ph.local("/storage/emulated/0/.nivaroos-trash/" + it.ID + "/a.jpg")); err != nil {
		t.Fatal("not in the phone's trash folder:", err)
	}
	// Gone already: nothing to do, no error.
	if _, ok, err := trashOnCompanion(dev, filepath.Join(storage, "DCIM/none"), "/storage/emulated/0/DCIM/none", dirs()); ok || err != nil {
		t.Fatal("missing file:", ok, err)
	}
	// The phone's trash folder is never listed.
	files, _ := FetchCompanionFilesFromDevice(dev, "/storage/emulated/0")
	for _, f := range files {
		if f.Name == phoneTrashDir {
			t.Fatal("trash folder listed")
		}
	}

	// Listed for the owner (with where it lives), not for another user,
	// and not counted as this server's disk space.
	l := listTrash(t, 1)
	if len(l.Data.Items) != 2 || l.Data.Items[0].Location != "Pixel" || l.Data.Items[0].Unavailable || l.Data.Bytes != 0 {
		t.Fatalf("owner list: %+v", l.Data)
	}
	if n := len(listTrash(t, 2).Data.Items); n != 0 {
		t.Fatalf("other user sees %d phone items", n)
	}

	// Restore next to a new file of the same name: never over it.
	phoneWrite(t, ph, "/storage/emulated/0/DCIM/a.jpg", "NEW")
	rec := companionReq(t, PostTrashRestore, "POST", "/v1/trash/restore", 1, "", map[string]interface{}{"ids": []string{it.ID}})
	var rr struct{ Data trash.RestoreResult }
	json.Unmarshal(rec.Body.Bytes(), &rr)
	if len(rr.Data.Restored) != 1 || rr.Data.Restored[0].Path != filepath.Join(storage, "DCIM/a (restored).jpg") {
		t.Fatalf("restore: %s", rec.Body.String())
	}
	if b, _ := os.ReadFile(ph.local("/storage/emulated/0/DCIM/a (restored).jpg")); string(b) != "AAAA" {
		t.Fatal("restored content wrong")
	}
	if _, err := os.Stat(ph.local("/storage/emulated/0/.nivaroos-trash/" + it.ID)); !os.IsNotExist(err) {
		t.Fatal("holder folder left behind")
	}

	// Offline phone: still listed, unavailable; delete forever fails and
	// keeps it.
	port := dev.Port
	companionMu.Lock()
	companionDevices[dev.ID].LastSeen = time.Now().Add(-time.Hour)
	companionDevices[dev.ID].Port = 1 // nothing answers
	companionMu.Unlock()
	resetCompanionRoutes()
	if l := listTrash(t, 1); len(l.Data.Items) != 1 || !l.Data.Items[0].Unavailable {
		t.Fatalf("offline list: %+v", l.Data.Items)
	}
	if rec := companionReq(t, DeleteTrashItems, "DELETE", "/v1/trash", 1, "", map[string]interface{}{"ids": []string{dir.ID}}); !strings.Contains(rec.Body.String(), `"error"`) {
		t.Fatalf("delete forever while offline: %s", rec.Body.String())
	}
	purgeCompanionTrash(0)
	if len(companionTrashRecs()) != 1 {
		t.Fatal("purged while the phone was offline")
	}

	// Back online: Empty Trash deletes it on the phone.
	companionMu.Lock()
	companionDevices[dev.ID].LastSeen = time.Now()
	companionDevices[dev.ID].Port = port
	companionMu.Unlock()
	resetCompanionRoutes()
	if rec := companionReq(t, DeleteTrashAll, "DELETE", "/v1/trash/all", 1, "", nil); strings.Contains(rec.Body.String(), `"error"`) {
		t.Fatalf("empty: %s", rec.Body.String())
	}
	if _, err := os.Stat(ph.local("/storage/emulated/0/.nivaroos-trash/" + dir.ID)); !os.IsNotExist(err) {
		t.Fatal("folder not deleted on the phone")
	}
	if len(companionTrashRecs()) != 0 {
		t.Fatal("records left")
	}
}

func TestPhoneTrashPurgeAndRemovedPhone(t *testing.T) {
	dev, ph, storage := phoneTrashSetup(t)
	phoneWrite(t, ph, "/storage/emulated/0/old.txt", "o")
	phoneWrite(t, ph, "/storage/emulated/0/new.txt", "n")
	old, _, err := trashOnCompanion(dev, filepath.Join(storage, "old.txt"), "/storage/emulated/0/old.txt", dirs())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := trashOnCompanion(dev, filepath.Join(storage, "new.txt"), "/storage/emulated/0/new.txt", dirs()); err != nil {
		t.Fatal(err)
	}
	editCompanionTrash(func(r []companionTrashRec) []companionTrashRec {
		for i := range r {
			if r[i].ID == old.ID {
				r[i].DeletedAt = r[i].DeletedAt.Add(-31 * 24 * time.Hour)
			}
		}
		return r
	})
	purgeCompanionTrash(service.TrashRetention)
	if recs := companionTrashRecs(); len(recs) != 1 || recs[0].Name != "new.txt" {
		t.Fatalf("purge: %+v", recs)
	}
	if _, err := os.Stat(ph.local("/storage/emulated/0/.nivaroos-trash/" + old.ID)); !os.IsNotExist(err) {
		t.Fatal("expired item still on the phone")
	}

	// A record that doesn't point into a phone trash folder is never used
	// to delete anything.
	bad := companionTrashRecs()[0]
	bad.HeldAt = "/storage/emulated/0/DCIM/x"
	if err := deleteCompanionHolder(dev, bad); err == nil {
		t.Fatal("deleted outside the trash folder")
	}

	// The phone was removed from NivaroOS: the item shows as unavailable;
	// restoring says why, delete forever just forgets it.
	companionMu.Lock()
	delete(companionDevices, dev.ID)
	companionMu.Unlock()
	l := listTrash(t, 1)
	if len(l.Data.Items) != 1 || !l.Data.Items[0].Unavailable {
		t.Fatalf("removed phone list: %+v", l.Data.Items)
	}
	id := l.Data.Items[0].ID
	rec := companionReq(t, PostTrashRestore, "POST", "/v1/trash/restore", 1, "", map[string]interface{}{"ids": []string{id}})
	if !strings.Contains(rec.Body.String(), errPhoneRemoved.Error()) {
		t.Fatalf("restore on removed phone: %s", rec.Body.String())
	}
	companionReq(t, DeleteTrashItems, "DELETE", "/v1/trash", 1, "", map[string]interface{}{"ids": []string{id}})
	if len(companionTrashRecs()) != 0 {
		t.Fatal("record kept")
	}
}

func TestTrashSupportForPhones(t *testing.T) {
	_, _, storage := phoneTrashSetup(t)
	var r struct{ Data trash.Support }
	json.Unmarshal(companionReq(t, GetTrashSupport, "GET", "/v1/trash/support?path="+filepath.Join(storage, "DCIM"), 1, "", nil).Body.Bytes(), &r)
	if !r.Data.Supported || r.Data.Kind != trash.KindPhone {
		t.Fatalf("phone support: %+v", r.Data)
	}
}
