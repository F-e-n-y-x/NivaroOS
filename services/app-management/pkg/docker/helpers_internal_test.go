package docker

import "testing"

func TestArchitecturesFromManifest(t *testing.T) {
	list := map[string]interface{}{
		"schemaVersion": 2.0,
		"mediaType":     MediaTypeManifestList,
		"manifests": []interface{}{
			map[string]interface{}{"digest": "sha256:a", "size": 529.0, "platform": map[string]interface{}{"architecture": "amd64", "os": "linux"}},
			map[string]interface{}{"digest": "sha256:b", "size": 529.0, "platform": map[string]interface{}{"architecture": "arm64", "os": "linux"}},
			map[string]interface{}{"digest": "sha256:c", "size": 566.0, "platform": map[string]interface{}{"architecture": "unknown", "os": "unknown"}},
			map[string]interface{}{"digest": "sha256:d", "size": 566.0},
		},
	}
	got, err := tryGetArchitecturesFromManifestList(list)
	if err != nil || len(got) != 2 || got[0] != "amd64" || got[1] != "arm64" {
		t.Fatalf("got %v %v", got, err)
	}

	got, err = tryGetArchitecturesFromV1SignedManifest(map[string]interface{}{"schemaVersion": 1.0, "architecture": "arm"})
	if err != nil || len(got) != 1 || got[0] != "arm" {
		t.Fatalf("got %v %v", got, err)
	}
}
