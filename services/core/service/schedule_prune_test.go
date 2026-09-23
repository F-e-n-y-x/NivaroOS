package service

import (
	"strings"
	"testing"
)

// The nightly cleanup must never delete containers or volumes.
func TestDockerCleanupNeverRemovesContainersOrVolumes(t *testing.T) {
	for _, args := range dockerPruneSteps {
		cmd := strings.Join(args, " ")
		if args[0] == "system" || args[0] == "container" || args[0] == "volume" || strings.Contains(cmd, "--volumes") || strings.Contains(cmd, " -a") {
			t.Errorf("unsafe cleanup step: docker %s", cmd)
		}
	}
	if len(dockerPruneSteps) == 0 {
		t.Fatal("cleanup does nothing")
	}
}
