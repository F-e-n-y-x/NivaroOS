package middleware

import (
	"os"
	"path/filepath"
	"testing"
)

// Never this host's /etc/nivaroos/local-automation-strict: the loopback trust
// tests would pass or fail with the box's own setting.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "middleware-test-")
	if err != nil {
		panic(err)
	}
	LocalStrictPath = filepath.Join(dir, "local-automation-strict")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
