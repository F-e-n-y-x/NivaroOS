package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/rclone/rclone/backend/local"
	_ "github.com/rclone/rclone/backend/memory"
	"github.com/rclone/rclone/cmd/mountlib"
	"github.com/rclone/rclone/fs"
	rconfig "github.com/rclone/rclone/fs/config"
)

func newTestFs(t *testing.T, name, typ string, kv ...string) fs.Fs {
	t.Helper()
	rconfig.LoadedData().SetValue(name, "type", typ)
	f, err := fs.NewFs(context.Background(), name+":"+t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(kv); i += 2 {
		rconfig.LoadedData().SetValue(name, kv[i], kv[i+1])
	}
	t.Cleanup(func() { rconfig.LoadedData().DeleteSection(name) })
	return f
}

func TestCloudTrashModes(t *testing.T) {
	moves := newTestFs(t, "ct-local", "local") // server-side Move + DirMove
	noMove := newTestFs(t, "ct-mem", "memory") // copy only: a rename re-uploads
	gd := newTestFs(t, "ct-gd", "memory", "type", "drive")
	gdOff := newTestFs(t, "ct-gdoff", "memory", "type", "drive", "use_trash", "false")
	tb := newTestFs(t, "ct-tb", "memory", "type", "terabox")
	tbOff := newTestFs(t, "ct-tboff", "memory", "type", "terabox", "delete_permanently", "true")
	od := newTestFs(t, "ct-od", "memory", "type", "onedrive")

	oldLists, oldFile := MountLists, CloudTrashFile
	CloudTrashFile = filepath.Join(t.TempDir(), "cloud-trash.json")
	MountLists = map[string]*mountlib.MountPoint{}
	t.Cleanup(func() { MountLists, CloudTrashFile = oldLists, oldFile })
	for mp, f := range map[string]fs.Fs{"/mnt/l": moves, "/mnt/m": noMove, "/mnt/gd": gd, "/mnt/gdoff": gdOff, "/mnt/tb": tb, "/mnt/tboff": tbOff, "/mnt/od": od} {
		MountLists[mp] = &mountlib.MountPoint{MountPoint: mp, Fs: f}
	}
	writeCloudTrash()

	raw, err := os.ReadFile(CloudTrashFile)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]cloudTrash
	json.Unmarshal(raw, &got)
	want := map[string]cloudTrash{
		"/mnt/l": {Mode: "trash"}, "/mnt/m": {Mode: "none"},
		"/mnt/gd": {Mode: "provider", Provider: "Google Drive"}, "/mnt/gdoff": {Mode: "none"},
		"/mnt/tb": {Mode: "provider", Provider: "TeraBox"}, "/mnt/tboff": {Mode: "none"},
		"/mnt/od": {Mode: "provider", Provider: "OneDrive"},
	}
	for mp, w := range want {
		if got[mp] != w {
			t.Errorf("%s: got %+v, want %+v", mp, got[mp], w)
		}
	}
}
