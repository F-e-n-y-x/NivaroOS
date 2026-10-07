package service

import (
	"testing"

	"github.com/compose-spec/compose-go/types"
)

// The nightly check: the same images before and after the store's
// compose means nothing to update.
func TestImagesChanged(t *testing.T) {
	a := types.Services{{Name: "app", Image: "ghcr.io/hotio/qbittorrent:release-5.0.4"}}
	if imagesChanged(a, types.Services{{Name: "app", Image: "ghcr.io/hotio/qbittorrent:release-5.0.4"}}) {
		t.Fatal("same image reported as changed")
	}
	if !imagesChanged(a, types.Services{{Name: "app", Image: "ghcr.io/hotio/qbittorrent:release-5.0.5"}}) {
		t.Fatal("new tag not reported")
	}
	// updatedServiceImages keeps a digest-checked tag local, so it can't
	// look like a change by itself.
	latest := types.Services{{Name: "app", Image: "nginx:latest"}}
	if imagesChanged(latest, updatedServiceImages(latest, types.Services{{Name: "app", Image: "nginx:latest"}})) {
		t.Fatal("latest tag counted as a change")
	}
}
