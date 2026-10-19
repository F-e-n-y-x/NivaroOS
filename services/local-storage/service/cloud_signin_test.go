package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Nothing in this package's tests may write the server's state file or
// publish to the real message bus.
func TestMain(m *testing.M) {
	dir, _ := os.MkdirTemp("", "ls-signin-")
	CloudSignInFile = filepath.Join(dir, "cloud-signin.json")
	notifyDrive = func(map[string]string) {}
	cloudAccountInfo = func(string) (string, string, bool, time.Time) { return "drive", "", false, time.Time{} }
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// The error local-storage logged for the owner's account on 2026-10-18.
var googleExpired = errors.New(`couldn't find root directory ID: Get "https://www.googleapis.com/drive/v3/files/root?alt=json": couldn't fetch token: invalid_grant: maybe token expired? - try refreshing with "rclone config reconnect google_drive_drive_1:"`)

func resetSignIn(t *testing.T) *[]map[string]string {
	t.Helper()
	CloudSignInFile = filepath.Join(t.TempDir(), "cloud-signin.json")
	cloudProblems.Lock()
	cloudProblems.m, cloudProblems.loaded = map[string]*CloudProblem{}, false
	cloudProblems.Unlock()
	var sent []map[string]string
	old := notifyDrive
	notifyDrive = func(p map[string]string) { sent = append(sent, p) }
	t.Cleanup(func() { notifyDrive = old })
	return &sent
}

func TestSignInExpiredClassifier(t *testing.T) {
	for _, c := range []struct {
		err  string
		want bool
	}{
		{googleExpired.Error(), true},
		{"oauth2: \"invalid_grant\" \"Token has been expired or revoked.\"", true},
		{"token expired and there's no refresh token - manually refresh with \"rclone config reconnect x:\"", true},
		{"couldn't fetch token: (can't decode error info) - try refreshing token with \"rclone config reconnect x:\"", true},
		{"path/to: expired_access_token", true},                             // Dropbox
		{"AADSTS700082: The refresh token has expired due to inactivity", true}, // OneDrive
		{"couldn't fetch token: Post \"https://oauth2.googleapis.com/token\": dial tcp: lookup oauth2.googleapis.com: no such host", false},
		{"context deadline exceeded", false},
		{"token expired", false}, // rclone's debug wording, not a refusal
	} {
		if got := signInExpired(errors.New(c.err)); got != c.want {
			t.Errorf("signInExpired(%q) = %v, want %v", c.err, got, c.want)
		}
	}
	if signInExpired(nil) {
		t.Error("nil is not expired")
	}
}

func TestSignInMessageExplainsTestingMode(t *testing.T) {
	now := time.Date(2026, 10, 18, 0, 42, 0, 0, time.UTC)
	tm := "Testing mode"
	for _, c := range []struct {
		name      string
		typ       string
		own       bool
		connected time.Time
		want      string
	}{
		{"own app, 7 days after connecting", "drive", true, now.Add(-7*24*time.Hour - time.Hour), tm},
		{"own app, sign-in time unknown", "drive", true, time.Time{}, tm},
		{"own app, months later", "drive", true, now.Add(-90 * 24 * time.Hour), "Google says"},
		{"rclone's shared app", "drive", false, time.Time{}, "Google says"},
		{"OneDrive", "onedrive", true, time.Time{}, "OneDrive no longer accepts"},
	} {
		got := signInMessage(c.typ, c.own, c.connected, now)
		if !strings.Contains(got, c.want) || !strings.Contains(got, "Reconnect") {
			t.Errorf("%s: %q, want it to say %q and Reconnect", c.name, got, c.want)
		}
	}
	if m := signInMessage("drive", true, time.Time{}, now); !strings.Contains(m, "OAuth consent screen › Publish app") {
		t.Errorf("Testing mode message doesn't say how to publish: %q", m)
	}
}

func TestSignInExpiredNotifiesOnce(t *testing.T) {
	sent := resetSignIn(t)
	now := time.Unix(1_790_000_000, 0)
	for i := 0; i < 30; i++ { // 30 hourly retries
		if !noteMountFailure("gd", googleExpired, now) {
			t.Fatal("not classified as sign-in expired")
		}
		now = now.Add(time.Hour)
	}
	if len(*sent) != 1 {
		t.Fatalf("want 1 notification, got %d", len(*sent))
	}
	n := (*sent)[0]
	if n["title"] != "Google Drive needs you to sign in again" || n["level"] != "error" || !strings.Contains(n["action"], `"section":"cloud"`) {
		t.Fatalf("notification: %v", n)
	}
	// A network hiccup in between doesn't make it fine, or notify.
	if !noteMountFailure("gd", errors.New("dial tcp: i/o timeout"), now) {
		t.Fatal("a network error cleared the expired state")
	}
	// A restart doesn't send it again.
	cloudProblems.Lock()
	cloudProblems.m, cloudProblems.loaded = map[string]*CloudProblem{}, false
	cloudProblems.Unlock()
	noteMountFailure("gd", googleExpired, now)
	if len(*sent) != 1 {
		t.Fatalf("restart re-sent the notification (%d)", len(*sent))
	}
	if p := CloudProblemOf("gd"); p == nil || !p.SignInExpired || p.Message == "" {
		t.Fatalf("problem not listed: %+v", p)
	}
	// Still broken days later: one reminder.
	now = now.Add(signInRenotify)
	noteMountFailure("gd", googleExpired, now)
	noteMountFailure("gd", googleExpired, now.Add(time.Hour))
	if len(*sent) != 2 {
		t.Fatalf("want a reminder after %s, got %d notifications", signInRenotify, len(*sent))
	}
	// Mounted again (Reconnect): cleared, and a later expiry notifies anew.
	clearCloudProblem("gd")
	if CloudProblemOf("gd") != nil {
		t.Fatal("not cleared")
	}
	noteMountFailure("gd", googleExpired, now.Add(2*time.Hour))
	if len(*sent) != 3 {
		t.Fatalf("a new expiry after reconnecting didn't notify (%d)", len(*sent))
	}
}

func TestWatcherBacksOffExpiredSignInHourly(t *testing.T) {
	sent := resetSignIn(t)
	env := newFakeEnv(gdrive)
	env.mountErr = googleExpired
	w, c := testWatcher(env, &fakeDaemon{env: env, down: true})
	for i := 0; i < 12*60*3; i++ { // 3 hours of 5 s ticks
		w.Tick()
		c.advance(5 * time.Second)
	}
	if n := len(env.mounts); n < 3 || n > 4 {
		t.Fatalf("want ~hourly tries over 3 h, got %d", n)
	}
	if len(*sent) != 1 {
		t.Fatalf("want 1 notification, got %d", len(*sent))
	}
	if p := CloudProblemOf(gdrive.Name); p == nil || !p.SignInExpired {
		t.Fatalf("not marked sign-in expired: %+v", p)
	}
}
