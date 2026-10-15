package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// The self-updater's nightly install waits while anyone watches a VM or
// the host's desktop.
func TestSessionsCounts(t *testing.T) {
	vmConsoleSessions.Add(2)
	hostConsoleSessions.Add(1)
	defer func() { vmConsoleSessions.Add(-2); hostConsoleSessions.Add(-1) }()
	rec := httptest.NewRecorder()
	handleSessions(rec, httptest.NewRequest("GET", "/sessions", nil))
	if got := strings.TrimSpace(rec.Body.String()); got != `{"host_desktop":1,"vm_console":2}` {
		t.Fatalf("GET /sessions = %s", got)
	}
}
