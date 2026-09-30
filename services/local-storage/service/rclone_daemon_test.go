package service

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"testing"
)

func TestSocketDaemon(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "rclone.sock")
	if _, err := newSocketDaemon(sock).ListMounts(); !errors.Is(err, errDaemonDown) {
		t.Fatalf("no socket: want errDaemonDown, got %v", err)
	}
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	var unmounted string
	mux := http.NewServeMux()
	mux.HandleFunc("/mount/listmounts", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"mountPoints":[{"Fs":"g:","MountPoint":"/mnt/g","MountedOn":"2026-09-30T03:00:38Z"}]}`))
	})
	mux.HandleFunc("/vfs/stats", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		json.NewDecoder(r.Body).Decode(&in)
		switch in["fs"] {
		case "g:":
			w.Write([]byte(`{"diskCache":{"uploadsInProgress":1,"uploadsQueued":2}}`))
		case "off:":
			w.Write([]byte(`{"inUse":1}`))
		default:
			w.WriteHeader(500)
			w.Write([]byte(`{"error":"no VFS found"}`))
		}
	})
	mux.HandleFunc("/mount/unmount", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		json.NewDecoder(r.Body).Decode(&in)
		unmounted = in["mountPoint"]
		w.Write([]byte(`{}`))
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(l)
	defer srv.Close()

	d := newSocketDaemon(sock)
	ms, err := d.ListMounts()
	if err != nil || len(ms) != 1 || ms[0].Fs != "g:" || ms[0].MountPoint != "/mnt/g" {
		t.Fatalf("listmounts: %v %v", ms, err)
	}
	if n, err := d.PendingUploads("g:"); err != nil || n != 3 {
		t.Fatalf("pending g: %d %v", n, err)
	}
	if n, err := d.PendingUploads("off:"); err != nil || n != 0 {
		t.Fatalf("cache off: %d %v", n, err)
	}
	if _, err := d.PendingUploads("x:"); err == nil {
		t.Fatal("an rc error must not read as 0 pending")
	}
	if err := d.Unmount("/mnt/g"); err != nil || unmounted != "/mnt/g" {
		t.Fatalf("unmount: %v %q", err, unmounted)
	}
}
