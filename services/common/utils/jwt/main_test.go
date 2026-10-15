package jwt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/common/middleware"
)

// Never this host's /etc/nivaroos/local-automation-strict: whether ?token=
// is still accepted would follow the box's own setting.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "jwt-test-")
	if err != nil {
		panic(err)
	}
	middleware.LocalStrictPath = filepath.Join(dir, "local-automation-strict")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
