package middleware

import (
	"crypto/subtle"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/common/localtoken"
)

// Loopback alone is not proof of root: any local uid (desktop apps, the DS
// browser's uid) and any host-network container also connect from 127.0.0.1
// (security audit 2026-10, H1). Same-host automation proves itself with the
// root-only token of package localtoken, sent in LocalTokenHeader.
//
// For one release the old loopback rule still works for callers that don't
// send the token yet - unless LocalStrictPath exists. Next release: drop
// LegacyLoopbackTrust and LocalAutomationHeader.

// LocalTokenHeader carries the local-automation token.
const LocalTokenHeader = localtoken.Header

// LocalStrictPath: while this file exists, loopback without the token is
// never local automation (the end state; see above).
var LocalStrictPath = "/etc/nivaroos/local-automation-strict"

// A test binary never sees the box's own setting: every test of a handler
// behind this middleware would pass or fail with it. Tests that need strict
// mode point LocalStrictPath at a file of their own.
func init() {
	if testing.Testing() {
		LocalStrictPath = ""
	}
}

// HasLocalToken reports whether r is from a loopback peer, not from a
// browser, and carries the right token.
func HasLocalToken(r *http.Request) bool {
	got := r.Header.Get(LocalTokenHeader)
	if got == "" || !IsLoopbackAddr(r.RemoteAddr) || hasBrowserMarks(r) {
		return false
	}
	want := localtoken.Read()
	return want != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// LegacyLoopbackTrust reports whether the old rule (loopback peer, no
// proxy/browser headers) may still stand in for the token.
func LegacyLoopbackTrust() bool {
	if LoopbackUntrusted() {
		return false
	}
	_, err := os.Stat(LocalStrictPath)
	return errors.Is(err, fs.ErrNotExist)
}

// LegacyQueryToken reports whether ?token= may still stand in for the
// Authorization header on plain (non-WebSocket) requests - until
// LocalStrictPath exists, like LegacyLoopbackTrust (audit L5; see
// common/utils/jwt/ticket.go).
func LegacyQueryToken() bool {
	_, err := os.Stat(LocalStrictPath)
	return errors.Is(err, fs.ErrNotExist)
}
