package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	rconfig "github.com/rclone/rclone/fs/config"
	"go.uber.org/zap"
)

// Cloud accounts whose sign-in expired or was revoked
//
// When the provider stops accepting an OAuth account's refresh token
// (Google's invalid_grant "Token has been expired or revoked", rclone's
// "maybe token expired? - try refreshing with rclone config reconnect"),
// the account can't mount again until someone signs in. Retrying every
// 10 minutes doesn't help, so the mount watcher marks it "sign-in
// expired", tries again only hourly (in case it was a provider hiccup),
// and sends one notification - again only if it is still broken
// signInRenotify later. Online Storage lists the account with a
// Reconnect button. MountStorage succeeding clears it.

const (
	signInRetry    = time.Hour
	signInRenotify = 3 * 24 * time.Hour
	// A Google app left in "Testing" publishing status gets refresh
	// tokens that expire 7 days after sign-in.
	testingTokenLife = 7 * 24 * time.Hour
)

// ConnectedAtKey: when the account last signed in (RFC 3339), kept in
// its rclone.conf section next to the token.
const ConnectedAtKey = "connected_at"

// CloudSignInFile keeps the notification times, so a restart of
// local-storage doesn't send the notification again.
var CloudSignInFile = "/var/run/nivaroos/cloud-signin.json"

// CloudProblem: why an account isn't mounted.
type CloudProblem struct {
	SignInExpired bool      `json:"sign_in_expired"`
	Message       string    `json:"message"`
	Since         time.Time `json:"since"`
	NotifiedAt    time.Time `json:"notified_at,omitempty"`
}

// cloudAccountInfo reads what the message needs from rclone.conf (fake in
// tests).
var cloudAccountInfo = func(remote string) (typ, label string, ownClient bool, connectedAt time.Time) {
	get := func(k string) string { v, _ := rconfig.LoadedData().GetValue(remote, k); return v }
	connectedAt, _ = time.Parse(time.RFC3339, get(ConnectedAtKey))
	return get("type"), get("username"), get("client_id") != "", connectedAt
}

var cloudProblems = struct {
	sync.Mutex
	m      map[string]*CloudProblem // by remote
	loaded bool
}{m: map[string]*CloudProblem{}}

// signInExpired: err means the provider no longer accepts the account's
// sign-in. A token request that failed for a network reason ("couldn't
// fetch token: dial tcp ...") is not this: rclone only says "rclone config
// reconnect" for a 400/401 from the token endpoint.
func signInExpired(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{
		"invalid_grant", "token has been expired or revoked", "maybe token expired",
		"token expired and there's no refresh token", "rclone config reconnect",
		"expired_access_token", "invalid_access_token", // Dropbox
		"aadsts70008", "aadsts700082", "aadsts50173", // OneDrive (Microsoft identity)
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

func providerName(typ string) string {
	switch typ {
	case "drive":
		return "Google Drive"
	case "onedrive":
		return "OneDrive"
	case "dropbox":
		return "Dropbox"
	case "iclouddrive":
		return "iCloud Drive"
	}
	if typ == "" {
		return "The cloud account"
	}
	return typ
}

// signInMessage: why the account needs signing in again, and what to do.
func signInMessage(typ string, ownClient bool, connectedAt, since time.Time) string {
	if typ == "drive" && ownClient {
		// Unknown sign-in time (accounts connected before it was kept):
		// for an own app this is by far the usual cause.
		if age := since.Sub(connectedAt); connectedAt.IsZero() || (age > testingTokenLife-24*time.Hour && age < testingTokenLife+48*time.Hour) {
			return "Your Google app is in Testing mode, so Google signs it out every 7 days. " +
				"Publish it (Google Cloud › OAuth consent screen › Publish app), then Reconnect."
		}
	}
	if typ == "drive" {
		return "Google says the sign-in expired or was revoked (a changed password, access removed in your Google Account, or 6 months unused). Reconnect it in Settings › Online Storage."
	}
	return providerName(typ) + " no longer accepts the sign-in (it expired or was revoked). Reconnect it in Settings › Online Storage."
}

func loadCloudProblemsLocked() {
	if cloudProblems.loaded {
		return
	}
	cloudProblems.loaded = true
	raw, err := os.ReadFile(CloudSignInFile)
	if err != nil {
		return
	}
	_ = json.Unmarshal(raw, &cloudProblems.m)
	if cloudProblems.m == nil {
		cloudProblems.m = map[string]*CloudProblem{}
	}
}

func saveCloudProblemsLocked() {
	raw, _ := json.Marshal(cloudProblems.m)
	_ = os.MkdirAll(filepath.Dir(CloudSignInFile), 0o755)
	tmp := CloudSignInFile + ".tmp"
	if os.WriteFile(tmp, raw, 0o644) == nil {
		_ = os.Rename(tmp, CloudSignInFile)
	}
}

// noteMountFailure records why remote didn't mount; true when its sign-in
// expired (then it notifies, once per signInRenotify).
func noteMountFailure(remote string, err error, now time.Time) bool {
	cloudProblems.Lock()
	loadCloudProblemsLocked()
	p := cloudProblems.m[remote]
	if !signInExpired(err) {
		// A network hiccup on an expired account doesn't make it fine.
		expired := p != nil && p.SignInExpired
		if !expired {
			cloudProblems.m[remote] = &CloudProblem{Message: err.Error(), Since: now}
		}
		cloudProblems.Unlock()
		return expired
	}
	typ, label, own, connectedAt := cloudAccountInfo(remote)
	if p == nil || !p.SignInExpired {
		p = &CloudProblem{SignInExpired: true, Since: now}
		cloudProblems.m[remote] = p
	}
	p.Message = signInMessage(typ, own, connectedAt, p.Since)
	notify := p.NotifiedAt.IsZero() || now.Sub(p.NotifiedAt) >= signInRenotify
	if notify {
		p.NotifiedAt = now
	}
	msg := p.Message
	saveCloudProblemsLocked()
	cloudProblems.Unlock()

	if !notify {
		logger.Info("cloud mounts: an account's sign-in is still expired; retrying hourly", zap.String("remote", remote), zap.Error(err))
		return true
	}
	title := providerName(typ) + " needs you to sign in again"
	if label != "" {
		title = label + " (" + providerName(typ) + ") needs you to sign in again"
	}
	args, _ := json.Marshal(map[string]string{"remote": remote, "reason": "sign_in_expired"})
	notifyDrive(map[string]string{
		"title": title, "message": msg, "level": "error", "category": "storage",
		"args": string(args), "action": `{"target":"settings","props":{"section":"cloud"}}`,
	})
	logger.Error("cloud mounts: an account's sign-in expired; notified, retrying hourly",
		zap.String("remote", remote), zap.String("why", msg), zap.Error(err))
	return true
}

// clearCloudProblem: remote mounted (or was removed).
func clearCloudProblem(remote string) {
	cloudProblems.Lock()
	defer cloudProblems.Unlock()
	loadCloudProblemsLocked()
	if p, ok := cloudProblems.m[remote]; ok {
		delete(cloudProblems.m, remote)
		if p.SignInExpired {
			saveCloudProblemsLocked()
		}
	}
}

// CloudProblemOf: why remote isn't mounted, nil when nothing is known.
func CloudProblemOf(remote string) *CloudProblem {
	cloudProblems.Lock()
	defer cloudProblems.Unlock()
	loadCloudProblemsLocked()
	if p := cloudProblems.m[remote]; p != nil {
		c := *p
		return &c
	}
	return nil
}

// CloudAccount: a configured cloud account and whether it is mounted.
type CloudAccount struct {
	Name, MountPoint string
	Mounted          bool
}

// CloudAccounts: every mounted account, then every configured one that
// isn't mounted (so a broken account stays listed instead of vanishing).
func CloudAccounts() []CloudAccount {
	var out []CloudAccount
	seen := map[string]bool{}
	mountMu.Lock()
	for mp, m := range MountLists {
		if m != nil && m.Fs != nil {
			out = append(out, CloudAccount{Name: m.Fs.Name(), MountPoint: mp, Mounted: true})
			seen[m.Fs.Name()] = true
		}
	}
	mountMu.Unlock()
	for _, name := range rconfig.LoadedData().GetSectionList() {
		mp, _ := rconfig.LoadedData().GetValue(name, "mount_point")
		if !seen[name] && cloudMountPointOK(mp) {
			out = append(out, CloudAccount{Name: name, MountPoint: mp})
		}
	}
	return out
}
