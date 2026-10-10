package v1

import (
	"testing"

	"github.com/rclone/rclone/fs"
)

// Every provider the UI offers must be linked in (backend/supported).
func TestCatalogBackendsLinked(t *testing.T) {
	for _, p := range providerCatalog {
		if _, err := fs.Find(p.Type); err != nil {
			t.Errorf("%s: %v (add it to backend/supported)", p.Type, err)
		}
	}
}
