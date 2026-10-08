package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
)

const testFstab = `# /etc/fstab
UUID=aaaa /               ext4    errors=remount-ro 0 1
UUID=bbbb none            swap    sw 0 0
UUID=B000FE9A00FE66AE /DATA/tower ntfs3 defaults,noatime,nofail,uid=1000,gid=1000,umask=000 0 2 # Added by the NivaroOS
UUID=cccc /mnt/my\040disk ext4 nofail 0 2
#UUID=dddd /DATA/old ext4 nofail 0 2
`

func TestFstabMountPoints(t *testing.T) {
	if got := strings.Join(fstabMountPoints(testFstab), "|"); got != "/DATA/tower|/mnt/my disk" {
		t.Fatalf("got %q", got)
	}
}

func TestMissingDrive(t *testing.T) {
	conf := []string{"/DATA/tower", "/DATA/tower/inner", "/mnt/my disk"}
	none := map[string]bool{}
	cases := []struct {
		src     []string
		mounted map[string]bool
		want    string
	}{
		{[]string{"/DATA/tower/Gallery"}, none, "/DATA/tower"},
		{[]string{"/DATA/tower"}, none, "/DATA/tower"},
		{[]string{"/DATA/towerX/a"}, none, ""},
		{[]string{"/DATA/AppData/immich", "/DATA/tower/Gallery"}, none, "/DATA/tower"},
		{[]string{"/DATA/tower/Gallery"}, map[string]bool{"/DATA/tower": true}, ""},
		{[]string{"/DATA/tower/inner/x"}, map[string]bool{"/DATA/tower": true}, "/DATA/tower/inner"},
		{[]string{"/DATA"}, none, ""}, // a parent of the drive, not on it
		{[]string{"/mnt/my disk/films"}, none, "/mnt/my disk"},
	}
	for _, c := range cases {
		if got := missingDrive(c.src, conf, c.mounted); got != c.want {
			t.Errorf("%v %v: got %q want %q", c.src, c.mounted, got, c.want)
		}
	}
}

type fakeDocker struct {
	list           []types.Container
	stopped, start []string
}

func (f *fakeDocker) ContainerList(context.Context, types.ContainerListOptions) ([]types.Container, error) {
	return f.list, nil
}

func (f *fakeDocker) ContainerStop(_ context.Context, id string, _ container.StopOptions) error {
	f.stopped = append(f.stopped, id)
	for i := range f.list {
		if f.list[i].ID == id {
			f.list[i].State = "exited"
		}
	}
	return nil
}

func (f *fakeDocker) ContainerStart(_ context.Context, id string, _ types.ContainerStartOptions) error {
	f.start = append(f.start, id)
	for i := range f.list {
		if f.list[i].ID == id {
			f.list[i].State = "running"
		}
	}
	return nil
}

// The 2026-10-08 power cut: tower failed to mount, Docker started immich
// and font2svg on empty folders. They are stopped, the database (on the
// system disk) and an app stopped by its owner are left alone, and once
// tower is mounted - even after an app-management restart - the two are
// started again, nothing else.
func TestDriveGuardHoldsAppsUntilTheDriveIsBack(t *testing.T) {
	bind := func(src string) []types.MountPoint { return []types.MountPoint{{Type: "bind", Source: src}} }
	d := &fakeDocker{list: []types.Container{
		{ID: "immich000000a", State: "running", Labels: map[string]string{"com.docker.compose.project": "immich"}, Mounts: bind("/DATA/tower/Gallery")},
		{ID: "postgres0000b", State: "running", Mounts: bind("/DATA/AppData/immich/pgdata")},
		{ID: "font2svg0000c", State: "restarting", Names: []string{"/font2svg"}, Mounts: bind("/DATA/tower/Backup_15-Aug/AppData/font2svg")},
		{ID: "stopped0000dd", State: "exited", Mounts: bind("/DATA/tower/Media")},
	}}
	mounted := map[string]bool{}
	var notes []string
	file := filepath.Join(t.TempDir(), "drive-wait.json")
	newGuard := func() *driveGuard {
		g := &driveGuard{docker: d, fstab: func() string { return testFstab }, mounted: func() map[string]bool { return mounted },
			notify: func(title, _, _ string) { notes = append(notes, title) }, file: file}
		g.load()
		return g
	}
	g := newGuard()
	g.tick(context.Background())
	if strings.Join(d.stopped, ",") != "immich000000a,font2svg0000c" {
		t.Fatalf("stopped %v", d.stopped)
	}
	if strings.Join(notes, "|") != "immich waits for drive tower|font2svg waits for drive tower" {
		t.Fatalf("notes %v", notes)
	}
	g.tick(context.Background())
	if len(d.stopped) != 2 || len(d.start) != 0 {
		t.Fatal("acted again while nothing changed")
	}

	mounted["/DATA/tower"] = true
	newGuard().tick(context.Background()) // app-management restarted meanwhile
	if strings.Join(d.start, ",") != "immich000000a,font2svg0000c" {
		t.Fatalf("started %v", d.start)
	}
	if g2 := newGuard(); len(g2.waiting) != 0 {
		t.Fatalf("still waiting: %v", g2.waiting)
	}
}
