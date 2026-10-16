//go:build e2e

package e2e

import (
	"crypto/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/webdav"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
	"github.com/F-e-n-y-x/NivaroOS/services/backup/jobs"
)

// Versioned (restic) backups end to end: the real binary and a real
// restic (NIVAROOS_RESTIC, else the installed one) into a local folder, a
// local "cloud" remote and a WebDAV server.

func resticInstance(t *testing.T, env ...string) *instance {
	t.Helper()
	bin := os.Getenv("NIVAROOS_RESTIC")
	if bin == "" {
		bin = engine.ResticPath
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skip("restic not installed; set NIVAROOS_RESTIC")
	}
	return newInstance(t, append(env, "NIVAROOS_RESTIC="+bin)...)
}

// webdavServer serves dir over WebDAV on a loopback port until the test ends.
func webdavServer(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: &webdav.Handler{FileSystem: webdav.Dir(dir), LockSystem: webdav.NewMemLS()}}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return "http://" + ln.Addr().String() + "/"
}

func versionsOf(in *instance, id string) []engine.Version {
	in.t.Helper()
	var vers []engine.Version
	in.must("GET", "/jobs/"+id+"/versions", nil, &vers)
	return vers
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestResticBackupDedupRestoreForget(t *testing.T) {
	in := resticInstance(t)
	dav := webdavServer(t, in.path("dav"))
	in.writeConf("[e2ecloud]\ntype = local\n\n[e2edav]\ntype = webdav\nurl = " + dav + "\nvendor = other\n")
	dests := []struct {
		name string
		ep   func() engine.Endpoint
	}{
		{"local", func() engine.Endpoint { return in.endpoint(in.path("usb")) }},
		{"cloud", func() engine.Endpoint { return cloud("cloud/repo") }},
		{"webdav", func() engine.Endpoint {
			return engine.Endpoint{Kind: engine.EPCloud, RefID: "e2edav", SubPath: "repo", Label: "WebDAV"}
		}},
	}
	parent := in.t
	for _, d := range dests {
		t.Run(d.name, func(t *testing.T) {
			in.t = t
			defer func() { in.t = parent }()
			src := in.path("src-" + d.name)
			writeFiles(t, src, 120, 32<<10)
			writeBig(t, filepath.Join(src, "big.bin"), 8<<20)
			total := int64(120*32<<10 + 8<<20)
			v1 := tree(t, src)
			if d.name == "local" {
				if err := os.MkdirAll(in.path("usb"), 0o755); err != nil {
					t.Fatal(err)
				}
			}

			j := job("versioned "+d.name, jobs.TypeBackup, in.endpoint(src), d.ep())
			j.Retention = engine.Retention{KeepLast: 2}
			jd := in.createJob(j)
			r := in.waitRun(in.runJob(jd.ID), 2*time.Minute)
			if r.Status != jobs.StatusSuccess {
				t.Fatalf("first run: %s %s\n%s", r.Status, r.ErrorCode, tail(in.log(), 3000))
			}
			if r.Counts.BytesTransferred < total {
				t.Errorf("first run uploaded %d bytes, the source has %d", r.Counts.BytesTransferred, total)
			}

			// 64 KiB changed: the second run uploads about that, not 12 MiB.
			writeFile(t, filepath.Join(src, "d000", "f000000.bin"), randomBytes(t, 32<<10))
			writeFile(t, filepath.Join(src, "new", "added.bin"), randomBytes(t, 32<<10))
			v2 := tree(t, src)
			r = in.waitRun(in.runJob(jd.ID), 2*time.Minute)
			if r.Status != jobs.StatusSuccess {
				t.Fatalf("second run: %s %s", r.Status, r.ErrorCode)
			}
			if up := r.Counts.BytesTransferred; up < 64<<10 || up > 1<<20 {
				t.Errorf("second run uploaded %d bytes for 64 KiB of changes", up)
			}
			if r.Counts.Added != 1 || r.Counts.Changed != 1 {
				t.Errorf("second run: %d added, %d changed", r.Counts.Added, r.Counts.Changed)
			}
			vers := versionsOf(in, jd.ID)
			if len(vers) != 2 || vers[0].Kind != engine.VersionSnapshot {
				t.Fatalf("versions: %+v", vers)
			}
			first := vers[1].ID

			// The first snapshot, restored elsewhere, is the first tree.
			var started jobs.RunStarted
			out := in.path("restored-" + d.name)
			other := in.endpoint(out)
			in.must("POST", "/jobs/"+jd.ID+"/restore", jobs.RestoreRequest{VersionID: first, Target: jobs.RestoreTarget{Mode: "other", Endpoint: &other}}, &started)
			if r := in.waitRun(started.RunID, 2*time.Minute); r.Status != jobs.StatusSuccess {
				t.Fatalf("restore to other: %s %s %+v", r.Status, r.ErrorCode, r.Summary)
			}
			sameTree(t, v1, tree(t, out))

			// One folder of the first snapshot back in place, over the change.
			in.must("POST", "/jobs/"+jd.ID+"/restore", jobs.RestoreRequest{VersionID: first, Paths: []string{"d000"},
				Target: jobs.RestoreTarget{Mode: "original"}, Conflict: engine.ConflictOverwrite}, &started)
			if r := in.waitRun(started.RunID, 2*time.Minute); r.Status != jobs.StatusSuccess {
				t.Fatalf("restore to original: %s %s", r.Status, r.ErrorCode)
			}
			want := map[string]string{}
			for p, h := range v2 {
				want[p] = h
			}
			for p, h := range v1 {
				if strings.HasPrefix(p, "d000/") {
					want[p] = h
				}
			}
			sameTree(t, want, tree(t, src))

			// A third run: keep_last 2 forgets the first snapshot and prunes
			// what only it used; then a check reads data back.
			if r := in.waitRun(in.runJob(jd.ID), 2*time.Minute); r.Status != jobs.StatusSuccess {
				t.Fatalf("third run: %s %s", r.Status, r.ErrorCode)
			}
			vers = versionsOf(in, jd.ID)
			if len(vers) != 2 || vers[0].ID == first || vers[1].ID == first {
				t.Fatalf("after forget: %+v (first was %s)", vers, first)
			}
			var chk jobs.RunStarted
			in.must("POST", "/jobs/"+jd.ID+"/run", jobs.RunRequest{Check: true}, &chk)
			if r := in.waitRun(chk.RunID, 2*time.Minute); r.Status != jobs.StatusSuccess {
				t.Fatalf("check: %s %s", r.Status, r.ErrorCode)
			}
		})
	}
}

// A crash mid-upload (SIGKILL: no clean-up, restic killed with the
// service) leaves restic's lock and some unindexed packs behind; the next
// run must still succeed, the repository check pass, and the restore be
// exact. Live progress and speed show while it runs.
func TestResticInterruptedRunResumes(t *testing.T) {
	in := resticInstance(t, slow)
	writeFiles(t, in.path("src"), 40, 256<<10) // 10 MiB at 1 MiB/s
	want := tree(t, in.path("src"))
	jd := in.createJob(job("versioned crash", jobs.TypeBackup, in.endpoint(in.path("src")), cloud("cloud/crash")))
	id := in.runJob(jd.ID)
	in.waitFor("upload under way", time.Minute, func() bool {
		r := in.getRun(id)
		if r.Status.Final() {
			t.Fatalf("run ended %s before it could be interrupted", r.Status)
		}
		return r.Live != nil && r.Live.Bytes > 2<<20 && r.Live.SpeedBps > 0 && r.Live.TotalBytes >= 10<<20
	})
	in.kill()
	in.start()
	if r := in.waitRun(id, 30*time.Second); r.Status == jobs.StatusSuccess {
		t.Fatalf("killed run reports success")
	}
	var list jobs.RunList
	in.must("GET", "/runs?job_id="+jd.ID+"&status=queued,running", nil, &list)
	for _, rr := range list.Runs {
		in.waitRun(rr.ID, 3*time.Minute)
	}
	r := in.waitRun(in.runJob(jd.ID), 3*time.Minute)
	if r.Status != jobs.StatusSuccess {
		t.Fatalf("run after crash: %s %s\n%s", r.Status, r.ErrorCode, tail(in.log(), 3000))
	}
	var chk jobs.RunStarted
	in.must("POST", "/jobs/"+jd.ID+"/run", jobs.RunRequest{Check: true}, &chk)
	if r := in.waitRun(chk.RunID, 2*time.Minute); r.Status != jobs.StatusSuccess {
		t.Fatalf("check after crash: %s %s", r.Status, r.ErrorCode)
	}
	vers := versionsOf(in, jd.ID)
	var started jobs.RunStarted
	other := in.endpoint(in.path("restored"))
	in.must("POST", "/jobs/"+jd.ID+"/restore", jobs.RestoreRequest{VersionID: vers[0].ID, Target: jobs.RestoreTarget{Mode: "other", Endpoint: &other}}, &started)
	if r := in.waitRun(started.RunID, 3*time.Minute); r.Status != jobs.StatusSuccess {
		t.Fatalf("restore: %s %s", r.Status, r.ErrorCode)
	}
	sameTree(t, want, tree(t, in.path("restored")))
}

// An encrypted versioned backup: the job's password is the repository's
// (restic opens it with that password and nothing else), and a second job
// pointed at the same folder adopts it with the recovery key - a
// reinstall.
func TestResticEncryptedRepoAndRecoveryKey(t *testing.T) {
	in := resticInstance(t)
	writeFiles(t, in.path("src"), 20, 16<<10)
	want := tree(t, in.path("src"))
	j := job("versioned secret", jobs.TypeBackup, in.endpoint(in.path("src")), cloud("cloud/secret"))
	j.Dest.Encryption = &engine.Encryption{Mode: engine.EncryptRestic}
	j.EncryptionSecret = &jobs.EncryptionSecret{Password: "correct horse battery", MakeRecoveryKey: true}
	jd := in.createJob(j)
	if jd.RecoveryKey == "" {
		t.Fatal("no recovery key in the create answer")
	}
	if r := in.waitRun(in.runJob(jd.ID), 2*time.Minute); r.Status != jobs.StatusSuccess {
		t.Fatalf("encrypted run: %s %s\n%s", r.Status, r.ErrorCode, tail(in.log(), 3000))
	}
	bin := os.Getenv("NIVAROOS_RESTIC")
	if bin == "" {
		bin = engine.ResticPath
	}
	repo := in.path("cloud", "secret")
	open := func(withPassword bool) error {
		args := []string{"-r", repo, "--no-cache", "snapshots"}
		cmd := exec.Command(bin, args...)
		cmd.Env = []string{"RESTIC_PASSWORD=correct horse battery"}
		if !withPassword {
			cmd = exec.Command(bin, append(args, "--insecure-no-password")...)
			cmd.Env = []string{}
		}
		return cmd.Run()
	}
	if err := open(true); err != nil {
		t.Fatalf("restic can't open the repository with the job's password: %v", err)
	}
	if err := open(false); err == nil {
		t.Fatal("the repository opens without a password")
	}

	// The same folder, a new job, the recovery key instead of the password.
	in.must("DELETE", "/jobs/"+jd.ID, nil, nil)
	j2 := job("versioned secret again", jobs.TypeBackup, in.endpoint(in.path("src")), cloud("cloud/secret"))
	j2.Dest.Encryption = &engine.Encryption{Mode: engine.EncryptRestic}
	j2.EncryptionSecret = &jobs.EncryptionSecret{RecoveryKey: jd.RecoveryKey}
	jd2 := in.createJob(j2)
	// The folder's marker names the deleted job: take it over, as the
	// app's "Reconnect to this folder" does.
	in.must("POST", "/jobs/"+jd2.ID+"/reconnect-dest", jobs.ReconnectRequest{AdoptOtherJob: true}, nil)
	vers := versionsOf(in, jd2.ID)
	if len(vers) != 1 {
		t.Fatalf("the adopted repository shows %d snapshots", len(vers))
	}
	var started jobs.RunStarted
	other := in.endpoint(in.path("restored"))
	in.must("POST", "/jobs/"+jd2.ID+"/restore", jobs.RestoreRequest{VersionID: vers[0].ID, Target: jobs.RestoreTarget{Mode: "other", Endpoint: &other}}, &started)
	if r := in.waitRun(started.RunID, 2*time.Minute); r.Status != jobs.StatusSuccess {
		t.Fatalf("restore with the recovery key: %s %s", r.Status, r.ErrorCode)
	}
	sameTree(t, want, tree(t, in.path("restored")))
}
