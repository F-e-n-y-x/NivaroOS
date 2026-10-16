package route

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/common/middleware"
)

// Never this host's /etc/nivaroos/local-automation-strict: tests that call
// the router from loopback would pass or fail with the box's own setting.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "local-storage-route-test-")
	if err != nil {
		panic(err)
	}
	middleware.LocalStrictPath = filepath.Join(dir, "local-automation-strict")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
