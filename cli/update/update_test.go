package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVerifySigFixtures(t *testing.T) {
	read := func(n string) []byte {
		b, err := os.ReadFile(filepath.Join("testdata", n))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	pk, err := parsePub(string(read("test.pub")))
	if err != nil {
		t.Fatal(err)
	}
	msg := read("SHA256SUMS")
	c, err := verifySig(pk, msg, read("legacy.minisig"))
	if err != nil || c != "nivaroos-server 1.5.0 stable" {
		t.Fatalf("legacy signature from minisign -l: %q %v", c, err)
	}
	if _, err := verifySig(pk, msg, read("prehashed.minisig")); err == nil || !strings.Contains(err.Error(), "prehashed") {
		t.Fatalf("prehashed signature must be refused, got %v", err)
	}
	if _, err := verifySig(pk, append([]byte("x"), msg...), read("legacy.minisig")); err == nil {
		t.Fatal("tampered SHA256SUMS verified")
	}
	forged := strings.Replace(string(read("legacy.minisig")), "1.5.0 stable", "9.9.9 stable", 1)
	if _, err := verifySig(pk, msg, []byte(forged)); err == nil {
		t.Fatal("edited trusted comment verified")
	}
	rel, err := parsePub(releaseKey)
	if err != nil {
		t.Fatalf("the compiled-in release key doesn't parse: %v", err)
	}
	if _, err := verifySig(rel, msg, read("legacy.minisig")); err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Fatalf("a signature by another key must be refused, got %v", err)
	}
}

func TestCompareVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"1.5.0", "1.4.9", 1}, {"1.5.0", "1.5.0", 0}, {"1.10.0", "1.9.0", 1},
		{"1.6.0-beta.1", "1.6.0", -1}, {"1.6.0-beta.2", "1.6.0-beta.10", -1},
		{"1.6.0-beta.1", "1.5.9", 1}, {"1.6.0-alpha", "1.6.0-beta", -1}, {"junk", "0.0.1", -1},
	} {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%s, %s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func rel(tag string, pre, draft bool, assets ...string) ghRelease {
	r := ghRelease{Tag: tag, Prerelease: pre, Draft: draft}
	for _, a := range assets {
		r.Assets = append(r.Assets, ghAsset{Name: a, URL: "https://x/" + a, Size: 10})
	}
	return r
}

func full(v string) []string {
	return []string{TarName(v, "amd64"), "SHA256SUMS", "SHA256SUMS.minisig"}
}

func TestPickRelease(t *testing.T) {
	list := []ghRelease{
		rel("v1.4.16", false, false, "NivaroOS-1.4.16.apk"), // the Android app
		rel("server-v1.5.0", false, false, full("1.5.0")...),
		rel("server-v1.6.0-beta.1", true, false, full("1.6.0-beta.1")...),
		rel("server-v1.7.0", false, true, full("1.7.0")...),           // draft
		rel("server-v1.5.1", false, false, TarName("1.5.1", "amd64")), // unsigned
		rel("server-v1.5.2", false, false, full("1.5.2")[1:]...),      // no amd64 bundle
	}
	if a := pickRelease(list, "stable", "amd64", "1.4.0", nil); a == nil || a.Version != "1.5.0" || a.Channel != "stable" {
		t.Fatalf("stable: %+v", a)
	}
	if a := pickRelease(list, "beta", "amd64", "1.4.0", nil); a == nil || a.Version != "1.6.0-beta.1" || a.Channel != "beta" {
		t.Fatalf("beta: %+v", a)
	}
	if a := pickRelease(list, "stable", "amd64", "1.5.0", nil); a != nil {
		t.Fatalf("nothing newer than 1.5.0 on stable, got %+v", a)
	}
	if a := pickRelease(list, "stable", "amd64", "1.4.0", []string{"1.5.0"}); a != nil {
		t.Fatalf("a rolled-back version is not offered again, got %+v", a)
	}
	if a := pickRelease(list, "stable", "arm64", "", nil); a != nil {
		t.Fatalf("no arm64 bundle, got %+v", a)
	}
}

// --- a signed bundle, made the way build-bundle.sh + release-server.sh do ---

type testKey struct {
	pub  pubKey
	sk   ed25519.PrivateKey
	file string // minisign .pub format
}

func newKey(t *testing.T) testKey {
	pk, sk, _ := ed25519.GenerateKey(nil)
	id := [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
	raw := append(append([]byte("Ed"), id[:]...), pk...)
	return testKey{pubKey{id, pk}, sk, "untrusted comment: test\n" + base64.StdEncoding.EncodeToString(raw) + "\n"}
}

// sign writes a legacy minisign signature (what `minisign -S -l` makes).
func (k testKey) sign(msg []byte, comment string) []byte {
	s := ed25519.Sign(k.sk, msg)
	g := ed25519.Sign(k.sk, append(append([]byte{}, s...), comment...))
	raw := append(append([]byte("Ed"), k.pub.id[:]...), s...)
	return []byte(fmt.Sprintf("untrusted comment: sig\n%s\ntrusted comment: %s\n%s\n",
		base64.StdEncoding.EncodeToString(raw), comment, base64.StdEncoding.EncodeToString(g)))
}

type entry struct {
	name, body string
	typ        byte
	link       string
}

// mkTar: a bundle tarball for version v; files get a MANIFEST unless the
// test overrides it. Returns the tarball path and its SHA256SUMS.
func mkTar(t *testing.T, dir, v string, entries []entry, manifest string) (string, []byte) {
	top := strings.TrimSuffix(TarName(v, "amd64"), ".tar.gz")
	if manifest == "" {
		var b strings.Builder
		fmt.Fprintf(&b, "version %s\narch amd64\nglibc_min 2.34\n", v)
		for _, e := range entries {
			if e.typ == 0 {
				h := sha256.Sum256([]byte(e.body))
				fmt.Fprintf(&b, "file x 0644 %s %s\n", hex.EncodeToString(h[:]), e.name)
			}
		}
		manifest = b.String()
	}
	entries = append(entries, entry{name: "MANIFEST", body: manifest})
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	_ = tw.WriteHeader(&tar.Header{Name: top + "/", Typeflag: tar.TypeDir, Mode: 0o755})
	for _, e := range entries {
		h := &tar.Header{Name: top + "/" + e.name, Mode: 0o755, Size: int64(len(e.body)), Typeflag: tar.TypeReg}
		if strings.HasPrefix(e.name, "/") || strings.HasPrefix(e.name, "../") {
			h.Name = e.name
		}
		if e.typ != 0 {
			h.Typeflag, h.Linkname, h.Size = e.typ, e.link, 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if e.typ == 0 {
			_, _ = tw.Write([]byte(e.body))
		}
	}
	_ = tw.Close()
	_ = zw.Close()
	path := filepath.Join(dir, TarName(v, "amd64"))
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return path, []byte(hex.EncodeToString(sum[:]) + "  " + filepath.Base(path) + "\n")
}

func TestStage(t *testing.T) {
	k := newKey(t)
	files := []entry{{name: "apply.sh", body: "#!/bin/bash\nexit 0\n"}, {name: "root/usr/bin/nivaroos", body: "core"}}
	want := Expect{"1.5.0", "stable", "amd64"}

	try := func(name string, entries []entry, manifest, comment string, edit func(tarPath string, sums []byte) []byte) error {
		t.Helper()
		dir := t.TempDir()
		tp, sums := mkTar(t, dir, "1.5.0", entries, manifest)
		if edit != nil {
			sums = edit(tp, sums)
		}
		if comment == "" {
			comment = TrustedComment("1.5.0", "stable")
		}
		staged, m, err := stage(k.pub, tp, sums, k.sign(sums, comment), want, filepath.Join(dir, "staged"))
		if err == nil {
			if b, rerr := os.ReadFile(filepath.Join(staged, "root/usr/bin/nivaroos")); rerr != nil || string(b) != "core" || m.Version != "1.5.0" {
				t.Fatalf("%s: staged bundle wrong: %v %q", name, rerr, b)
			}
		} else if _, serr := os.Stat(filepath.Join(dir, "staged", "1.5.0")); serr == nil {
			t.Fatalf("%s: a refused bundle was left staged", name)
		}
		return err
	}

	if err := try("good", files, "", "", nil); err != nil {
		t.Fatalf("good bundle: %v", err)
	}
	for _, c := range []struct {
		name     string
		entries  []entry
		manifest string
		comment  string
		edit     func(string, []byte) []byte
		errHas   string
	}{
		{name: "beta signature on stable", entries: files, comment: TrustedComment("1.5.0", "beta"), errHas: "signature is for"},
		{name: "old release served as new", entries: files, comment: TrustedComment("1.4.0", "stable"), errHas: "signature is for"},
		{name: "tarball swapped after signing", entries: files, edit: func(tp string, sums []byte) []byte {
			_ = os.WriteFile(tp, []byte("evil"), 0o600)
			return sums
		}, errHas: "does not match SHA256SUMS"},
		{name: "path traversal", entries: append(files, entry{name: "../../etc/evil", body: "x"}), errHas: "unsafe path"},
		{name: "absolute path", entries: append(files, entry{name: "/etc/evil", body: "x"}), errHas: "unsafe path"},
		{name: "symlink", entries: append(files, entry{name: "root/usr/bin/link", typ: tar.TypeSymlink, link: "/etc/shadow"}), errHas: "not a file or folder"},
		{name: "file MANIFEST doesn't list", entries: files, manifest: "version 1.5.0\narch amd64\nfile x 0644 " + strings.Repeat("0", 64) + " apply.sh\n", errHas: "MANIFEST"},
	} {
		err := try(c.name, c.entries, c.manifest, c.comment, c.edit)
		if err == nil || !strings.Contains(err.Error(), c.errHas) {
			t.Errorf("%s: want error containing %q, got %v", c.name, c.errHas, err)
		}
	}
}

func TestWindowGlibcUnsupported(t *testing.T) {
	loc := time.Local
	at := func(h, m int) time.Time { return time.Date(2026, 10, 16, h, m, 0, 0, loc) }
	for _, c := range []struct {
		t    time.Time
		want bool
	}{{at(3, 29), false}, {at(3, 30), true}, {at(4, 20), true}, {at(5, 29), true}, {at(5, 30), false}, {at(14, 0), false}} {
		if got := inWindow(c.t, "04:00"); got != c.want {
			t.Errorf("inWindow(%s) = %v", c.t.Format("15:04"), got)
		}
	}
	if !glibcOK("2.34", "2.41") || !glibcOK("2.34", "2.34") || glibcOK("2.35", "2.31") || glibcOK("2.34", "") || !glibcOK("0", "") {
		t.Error("glibcOK")
	}
	dir := t.TempDir()
	osRelease = filepath.Join(dir, "os-release")
	defer func() { osRelease = "/etc/os-release"; glibc = func() string { return "" } }()
	glibc = func() string { return "2.41" }
	for content, ok := range map[string]bool{
		"ID=debian\n": true, "ID=ubuntu\nID_LIKE=debian\n": true, "ID=linuxmint\nID_LIKE=\"ubuntu debian\"\n": true,
		"ID=alpine\n": false, "ID=fedora\nID_LIKE=\"rhel centos\"\n": false,
	} {
		_ = os.WriteFile(osRelease, []byte(content), 0o644)
		if got := unsupported() == ""; got != ok {
			t.Errorf("unsupported() for %q: supported=%v, want %v", content, got, ok)
		}
	}
}

// setup points the package at a temp box: update dir, release.json, a fake
// GitHub, no services running, Debian with glibc 2.41.
func setup(t *testing.T, current string, releases []ghRelease) (string, *testKey) {
	t.Helper()
	dir := t.TempDir()
	Dir = filepath.Join(dir, "update")
	ReleaseFile = filepath.Join(dir, "release.json")
	if current != "" {
		_ = os.WriteFile(ReleaseFile, []byte(`{"version":"`+current+`","channel":"stable"}`), 0o644)
	}
	osRelease = filepath.Join(dir, "os-release")
	_ = os.WriteFile(osRelease, []byte("ID=debian\n"), 0o644)
	glibc = func() string { return "2.41" }
	arch = "amd64"
	RuntimeDir = dir // no message-bus.url: notifications are dropped
	TransfersFile = filepath.Join(dir, "transfer-jobs.json")
	dead := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(dead.Close)
	BackupAPI, SidecarAPI = dead.URL, dead.URL
	k := newKey(t)
	releaseKey = k.file
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(releases)
	}))
	t.Cleanup(gh.Close)
	ReleasesAPI = gh.URL
	return dir, &k
}

func TestNightlySkipsWhileBusyAndCheckNeverInstalls(t *testing.T) {
	list := []ghRelease{rel("server-v1.5.0", false, false, full("1.5.0")...)}
	dir, _ := setup(t, "1.4.0", list)
	now = func() time.Time { return time.Date(2026, 10, 16, 4, 10, 0, 0, time.Local) }
	defer func() { now = time.Now }()
	_ = os.WriteFile(TransfersFile, []byte(`[{"state":"done"},{"state":"running"}]`), 0o644)
	if err := Nightly(context.Background(), os.Stderr); err != nil {
		t.Fatal(err)
	}
	st := LoadState()
	if st.State != StateAvailable || st.Available == nil || st.Available.Version != "1.5.0" {
		t.Fatalf("after the nightly check: %+v", st)
	}
	if !strings.Contains(st.Skipped, "1 file transfer") {
		t.Fatalf("a running transfer must skip tonight's install, skipped=%q", st.Skipped)
	}
	if _, err := os.Stat(filepath.Join(Dir, "download")); err == nil {
		t.Fatal("nothing may be downloaded while the box is busy")
	}
	// Notify-only: checked, never installed, even when idle in the window.
	_ = os.Remove(TransfersFile)
	_ = SaveSettings(Settings{Channel: "stable", Auto: "notify", Window: "04:00"})
	if err := Nightly(context.Background(), os.Stderr); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(Dir, "download")); err == nil {
		t.Fatal("notify-only must not download")
	}
	_ = dir
}

// A local bundle whose apply.sh says "rolled back" (exit 1): the history
// says so, the version is not offered again; exit 0 = installed.
func TestApplyLocalOutcomes(t *testing.T) {
	for _, c := range []struct {
		script, result string
		bad            bool
	}{
		{"echo '::step installing'; echo '1.5.0 installed (core)'; exit 0", "installed", false},
		{"echo 'failed: health check'; exit 1", "rolled_back", true},
		{"echo 'refused: x does not match MANIFEST'; exit 3", "failed", false},
	} {
		dir, k := setup(t, "1.4.0", nil)
		tp, sums := mkTar(t, dir, "1.5.0", []entry{{name: "apply.sh", body: "#!/bin/bash\n" + c.script + "\n"}}, "")
		sumsPath, sigPath := filepath.Join(dir, "SHA256SUMS"), filepath.Join(dir, "SHA256SUMS.minisig")
		_ = os.WriteFile(sumsPath, sums, 0o644)
		_ = os.WriteFile(sigPath, k.sign(sums, TrustedComment("1.5.0", "stable")), 0o644)
		err := Apply(context.Background(), ApplyOptions{Manual: true, Local: tp, Sums: sumsPath, Sig: sigPath, Out: os.Stderr})
		if (err == nil) != (c.result == "installed") {
			t.Fatalf("%s: err=%v", c.result, err)
		}
		h := History()
		st := LoadState()
		if len(h) != 1 || h[0].Result != c.result || h[0].From != "1.4.0" || h[0].To != "1.5.0" {
			t.Fatalf("%s: history %+v", c.result, h)
		}
		if got := len(st.BadVersions) == 1; got != c.bad || st.State != StateIdle {
			t.Fatalf("%s: state %+v", c.result, st)
		}
	}
}

func TestApplyLocalRefusesBetaOnStableAndDowngrade(t *testing.T) {
	dir, k := setup(t, "1.5.0", nil)
	tp, sums := mkTar(t, dir, "1.4.0", []entry{{name: "apply.sh", body: "exit 0"}}, "")
	_ = os.WriteFile(filepath.Join(dir, "S"), sums, 0o644)
	_ = os.WriteFile(filepath.Join(dir, "S.sig"), k.sign(sums, TrustedComment("1.4.0", "beta")), 0o644)
	err := Apply(context.Background(), ApplyOptions{Local: tp, Sums: filepath.Join(dir, "S"), Sig: filepath.Join(dir, "S.sig")})
	if err == nil || !strings.Contains(err.Error(), "beta") {
		t.Fatalf("beta on stable: %v", err)
	}
	_ = os.WriteFile(filepath.Join(dir, "S.sig"), k.sign(sums, TrustedComment("1.4.0", "stable")), 0o644)
	err = Apply(context.Background(), ApplyOptions{Local: tp, Sums: filepath.Join(dir, "S"), Sig: filepath.Join(dir, "S.sig")})
	if err == nil || !strings.Contains(err.Error(), "not newer") {
		t.Fatalf("downgrade: %v", err)
	}
}

// Resume: a reboot during applying rolls back; a committed snapshot is
// just recorded as installed.
func TestResume(t *testing.T) {
	setup(t, "1.4.0", nil)
	snap := filepath.Join(Dir, "snap")
	_ = os.MkdirAll(snap, 0o700)
	_ = os.WriteFile(filepath.Join(snap, "to-version"), []byte("1.5.0\n"), 0o644)
	_ = os.WriteFile(filepath.Join(snap, "from-version"), []byte("1.4.0\n"), 0o644)
	_ = os.WriteFile(filepath.Join(snap, "apply.sh"), []byte("echo \"$@\" > \"$(dirname \"$0\")/ran\"; echo 'rolled back to 1.4.0'\n"), 0o755)

	_ = os.WriteFile(filepath.Join(snap, "state"), []byte("applying\n"), 0o644)
	st := LoadState()
	st.State = StateApplying
	_ = saveState(&st)
	if err := Resume(context.Background(), os.Stderr); err != nil {
		t.Fatal(err)
	}
	ran, _ := os.ReadFile(filepath.Join(snap, "ran"))
	if !strings.Contains(string(ran), "rollback --restore-data") {
		t.Fatalf("interrupted apply must roll back with the data: ran %q", ran)
	}
	if st := LoadState(); st.State != StateIdle || len(st.BadVersions) != 1 {
		t.Fatalf("after resume: %+v", st)
	}

	_ = os.Remove(filepath.Join(snap, "ran"))
	_ = os.WriteFile(filepath.Join(snap, "state"), []byte("committed\n"), 0o644)
	st = LoadState()
	st.State = StateProbation
	_ = saveState(&st)
	if err := Resume(context.Background(), os.Stderr); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(snap, "ran")); err == nil {
		t.Fatal("a committed update must not be rolled back at boot")
	}
	if h := History(); h[0].Result != "installed" {
		t.Fatalf("history after a committed resume: %+v", h[0])
	}
}

var realKey = releaseKey // before setup() swaps in a test key

// release-server.sh runs this on the files it is about to publish, so the
// compiled-in key and this verifier are known to accept them:
// NIVAROOS_RELEASE_DIR (SHA256SUMS, SHA256SUMS.minisig, the tarballs),
// NIVAROOS_RELEASE_VERSION, NIVAROOS_RELEASE_CHANNEL.
func TestReleaseBundle(t *testing.T) {
	dir, ver, ch := os.Getenv("NIVAROOS_RELEASE_DIR"), os.Getenv("NIVAROOS_RELEASE_VERSION"), os.Getenv("NIVAROOS_RELEASE_CHANNEL")
	if dir == "" {
		t.Skip("NIVAROOS_RELEASE_DIR not set (release-server.sh sets it)")
	}
	pk, err := parsePub(realKey)
	if err != nil {
		t.Fatal(err)
	}
	sums, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	sig, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS.minisig"))
	if err != nil {
		t.Fatal(err)
	}
	tars, _ := filepath.Glob(filepath.Join(dir, "nivaroos-server-"+ver+"-linux-*.tar.gz"))
	if len(tars) == 0 {
		t.Fatal("no bundles for " + ver)
	}
	for _, tp := range tars {
		a := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(tp), "nivaroos-server-"+ver+"-linux-"), ".tar.gz")
		if _, m, err := stage(pk, tp, sums, sig, Expect{ver, ch, a}, t.TempDir()); err != nil {
			t.Fatalf("%s: %v", filepath.Base(tp), err)
		} else {
			t.Logf("%s: verified, %d files", filepath.Base(tp), len(m.Files))
		}
	}
}
