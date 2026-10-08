package cloudcache

import (
	"errors"
	"strings"
	"testing"
	"time"

	"gopkg.in/ini.v1"
)

func fakeEnv(mounts []Mount, free uint64) Env {
	return Env{
		Mounts:           func() ([]Mount, error) { return mounts, nil },
		Free:             func(string) (uint64, error) { return free, nil },
		CacheUsed:        func(string) int64 { return 0 },
		CloudMountPoints: []string{"/mnt/terabox_x", "/mnt/google_drive_y"},
		ProbeWritable:    func(string) error { return nil },
	}
}

var baseMounts = []Mount{
	{Point: "/", FSType: "ext4"},
	{Point: "/tmp", FSType: "tmpfs"},
	{Point: "/DATA/Disk1", FSType: "ext4"},
	{Point: "/mnt/terabox_x", FSType: "fuse.rclone"},
	{Point: "/media/nas", FSType: "cifs"},
}

func TestDefaults(t *testing.T) {
	d := Defaults()
	if d.Mode != ModeFull || d.MaxSize != 20<<30 || d.MaxAge != time.Hour || d.Dir != "/var/cache/nivaroos/rclone" {
		t.Fatalf("defaults: %+v", d)
	}
	if d.VFSCacheMode() != 3 || (Settings{Mode: ModeWrites}).VFSCacheMode() != 2 || (Settings{Mode: ModeOff}).VFSCacheMode() != 0 {
		t.Fatal("cache mode numbers")
	}
}

func TestLoadStoreRoundTrip(t *testing.T) {
	cfg := ini.Empty()
	if got := Load(cfg); got != Defaults() {
		t.Fatalf("empty cfg: %+v", got)
	}
	want := Settings{Mode: ModeWrites, MaxSize: 5 << 30, MaxAge: 90 * time.Minute, Dir: "/DATA/Disk1/cache"}
	Store(cfg, want)
	if got := Load(cfg); got != want {
		t.Fatalf("round trip: %+v", got)
	}
	cfg.Section(Section).Key("Mode").SetValue("bogus")
	cfg.Section(Section).Key("Dir").SetValue("relative/dir")
	cfg.Section(Section).Key("MaxSize").SetValue("-4")
	got := Load(cfg)
	if got.Mode != ModeFull || got.Dir != DefaultDir || got.MaxSize != DefaultMaxSize {
		t.Fatalf("bad values should fall back: %+v", got)
	}
}

func TestValidate(t *testing.T) {
	gb := uint64(1 << 30)
	cases := []struct {
		name    string
		s       Settings
		free    uint64
		wantErr string
		warn    string
	}{
		{"default on system disk warns", Defaults(), 100 * gb, "", "system disk"},
		{"data disk ok", Settings{Mode: "FULL", MaxSize: 10 << 30, MaxAge: time.Hour, Dir: "/DATA/Disk1/rc/"}, 100 * gb, "", ""},
		{"bad mode", Settings{Mode: "most", MaxSize: 1, MaxAge: time.Hour, Dir: DefaultDir}, 100 * gb, "cache mode", ""},
		{"zero size", Settings{Mode: ModeFull, MaxSize: 0, MaxAge: time.Hour, Dir: DefaultDir}, 100 * gb, "more than 0", ""},
		{"age too short", Settings{Mode: ModeFull, MaxSize: 1 << 30, MaxAge: time.Second, Dir: DefaultDir}, 100 * gb, "at least 1 minute", ""},
		{"relative", Settings{Mode: ModeFull, MaxSize: 1 << 30, MaxAge: time.Hour, Dir: "cache"}, 100 * gb, "full folder path", ""},
		{"root", Settings{Mode: ModeFull, MaxSize: 1 << 30, MaxAge: time.Hour, Dir: "/"}, 100 * gb, "not the whole disk", ""},
		{"system folder", Settings{Mode: ModeFull, MaxSize: 1 << 30, MaxAge: time.Hour, Dir: "/etc/cache"}, 100 * gb, "system folder", ""},
		{"inside cloud mount", Settings{Mode: ModeFull, MaxSize: 1 << 30, MaxAge: time.Hour, Dir: "/mnt/terabox_x/c"}, 100 * gb, "online storage drive", ""},
		{"inside configured but unmounted cloud", Settings{Mode: ModeFull, MaxSize: 1 << 30, MaxAge: time.Hour, Dir: "/mnt/google_drive_y/c"}, 100 * gb, "online storage drive", ""},
		{"contains cloud mount", Settings{Mode: ModeFull, MaxSize: 1 << 30, MaxAge: time.Hour, Dir: "/mnt"}, 100 * gb, "online storage drive", ""},
		{"network share", Settings{Mode: ModeFull, MaxSize: 1 << 30, MaxAge: time.Hour, Dir: "/media/nas/c"}, 100 * gb, "online or network drive", ""},
		{"tmpfs", Settings{Mode: ModeFull, MaxSize: 1 << 30, MaxAge: time.Hour, Dir: "/tmp/c"}, 100 * gb, "in memory", ""},
		{"bigger than free", Settings{Mode: ModeFull, MaxSize: 50 << 30, MaxAge: time.Hour, Dir: "/DATA/Disk1/c"}, 40 * gb, "less than the free space", ""},
		{"nearly all free", Settings{Mode: ModeFull, MaxSize: 38 << 30, MaxAge: time.Hour, Dir: "/DATA/Disk1/c"}, 40 * gb, "", "almost all"},
		{"off warns", Settings{Mode: ModeOff, MaxSize: 1 << 30, MaxAge: time.Hour, Dir: "/DATA/Disk1/c"}, 40 * gb, "", "cache off"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := c.s
			warns, err := Validate(&s, fakeEnv(baseMounts, c.free))
			if c.wantErr != "" {
				if err == nil || !IsValidation(err) || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("want error %q, got %v", c.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			joined := strings.Join(warns, "|")
			if c.warn != "" && !strings.Contains(joined, c.warn) {
				t.Fatalf("want warning %q, got %q", c.warn, joined)
			}
			if c.warn == "" && len(warns) > 0 {
				t.Fatalf("unexpected warnings %q", joined)
			}
		})
	}
}

func TestValidateNormalises(t *testing.T) {
	s := Settings{Mode: " Writes ", MaxSize: 1 << 30, MaxAge: time.Hour, Dir: "/DATA/Disk1/x/../rc/"}
	if _, err := Validate(&s, fakeEnv(baseMounts, 1<<40)); err != nil {
		t.Fatal(err)
	}
	if s.Mode != ModeWrites || s.Dir != "/DATA/Disk1/rc" {
		t.Fatalf("not normalised: %+v", s)
	}
}

func TestValidateCountsCurrentCacheAsFree(t *testing.T) {
	s := Settings{Mode: ModeFull, MaxSize: 30 << 30, MaxAge: time.Hour, Dir: "/DATA/Disk1/c"}
	env := fakeEnv(baseMounts, 20<<30)
	env.CacheUsed = func(string) int64 { return 20 << 30 }
	if _, err := Validate(&s, env); err != nil {
		t.Fatalf("cache already there counts as room: %v", err)
	}
}

func TestValidateNotWritable(t *testing.T) {
	s := Settings{Mode: ModeFull, MaxSize: 1 << 30, MaxAge: time.Hour, Dir: "/DATA/Disk1/c"}
	env := fakeEnv(baseMounts, 1<<40)
	env.ProbeWritable = func(string) error { return errors.New("read-only file system") }
	if _, err := Validate(&s, env); err == nil || !strings.Contains(err.Error(), "can't write") {
		t.Fatalf("want not-writable error, got %v", err)
	}
}

func TestHumanBytes(t *testing.T) {
	for n, want := range map[int64]string{512: "512 B", 20 << 30: "20 GB", 1536: "1.5 KB", 5900 << 20: "5.8 GB"} {
		if got := HumanBytes(n); got != want {
			t.Errorf("HumanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}
