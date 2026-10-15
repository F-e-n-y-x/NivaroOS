// Package localtoken is the root-only token that same-host automation
// (other NivaroOS services, nivaroos-cli, the installer) sends instead of
// relying on a loopback connection alone.
package localtoken

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Loopback alone is not proof of root: any local uid (desktop apps, the DS
// browser's uid) and any host-network container also connect from 127.0.0.1
// (security audit 2026-10, H1). Same-host automation proves itself with a
// random token that only root can read, sent in Header.
//
// Services check it in common/middleware (HasLocalToken, IsLocalAutomation).

// Header carries the local-automation token.
const Header = "X-Nivaroos-Local-Token"

// Path holds the token: 64 hex chars, 0600, owned by the service
// uid (root). /run is a tmpfs, so a new one is made after every boot.
var Path = "/var/run/nivaroos/local-automation.token"

// Read returns the token, or "" if the file is missing or anybody
// but its owner (us) could have read or written it.
func Read() string {
	fi, err := os.Lstat(Path)
	if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o077 != 0 {
		return ""
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != os.Geteuid() {
		return ""
	}
	b, err := os.ReadFile(Path)
	if err != nil {
		return ""
	}
	t := strings.TrimSpace(string(b))
	if len(t) < 32 {
		return ""
	}
	return t
}

// LocalToken returns the token, creating it if there is none yet ("" if it
// can't be read or made - e.g. a service with a read-only /run).
func Get() string {
	if t := Read(); t != "" {
		return t
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	_ = os.MkdirAll(filepath.Dir(Path), 0o755)
	// CreateTemp: a unique name, mode 0600.
	if f, err := os.CreateTemp(filepath.Dir(Path), ".local-automation.token.*"); err == nil {
		_, werr := f.WriteString(hex.EncodeToString(b) + "\n")
		if f.Close() == nil && werr == nil {
			// Link never replaces: a concurrent creator wins and we read theirs.
			_ = os.Link(f.Name(), Path)
		}
		_ = os.Remove(f.Name())
	}
	return Read()
}

// Add sets Header on a request to a loopback address
// (never anywhere else, so a wrong URL can't leak it).
func Add(r *http.Request) {
	h := r.URL.Hostname()
	if ip := net.ParseIP(h); !(h == "localhost" || (ip != nil && ip.IsLoopback())) {
		return
	}
	if t := Get(); t != "" {
		r.Header.Set(Header, t)
	}
}

// Editor is Add as a generated client's RequestEditorFn.
func Editor(_ context.Context, r *http.Request) error {
	Add(r)
	return nil
}
