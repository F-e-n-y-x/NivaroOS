package service

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/robfig/cron/v3"
)

func testUpdateManager(t *testing.T) *ContainerUpdateManager {
	t.Helper()
	logger.LogInitConsoleOnly()
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	return &ContainerUpdateManager{
		configs:  map[string]ContainerUpdateInfo{},
		global:   GlobalAutoUpdateConfig{Schedule: "0 3 * * *"},
		dataFile: filepath.Join(t.TempDir(), "container_updates.json"),
		cron:     cron.New(cron.WithParser(parser)),
	}
}

// The per-container "Auto" switch was saved under the container's ID;
// every recreate gives a new ID, so the setting was lost (live: 12 orphan
// entries, all 20 containers showing Auto off).
func TestAutoUpdateChoiceSurvivesARecreate(t *testing.T) {
	if exec.Command("docker", "info").Run() != nil {
		t.Skip("docker not available")
	}
	name := "nvtest-autoupdate"
	exec.Command("docker", "rm", "-f", name).Run()
	out, err := exec.Command("docker", "create", "--name", name, "debian:latest", "sleep", "60").Output()
	if err != nil {
		t.Skip("can't create a test container")
	}
	t.Cleanup(func() { exec.Command("docker", "rm", "-f", name).Run() })
	id := strings.TrimSpace(string(out))

	m := testUpdateManager(t)
	if err := m.SetContainerAutoUpdate(id, true, ""); err != nil { // the UI passes the ID
		t.Fatal(err)
	}
	// Recreated (new ID, same name) - what every update does.
	exec.Command("docker", "rm", "-f", name).Run()
	exec.Command("docker", "create", "--name", name, "debian:latest", "sleep", "60").Run()

	list, err := m.GetAllContainersWithUpdates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range list {
		if c.Name == name {
			if !c.AutoUpdateEnabled {
				t.Fatal("auto-update choice lost after the container was recreated")
			}
			return
		}
	}
	t.Fatal("test container not listed")
}

func TestOldIDKeyedEntriesAreMigratedToNames(t *testing.T) {
	m := testUpdateManager(t)
	id := strings.Repeat("ab", 32)
	m.configs = map[string]ContainerUpdateInfo{
		id:                       {ID: id, Name: "immich", AutoUpdateEnabled: true},
		strings.Repeat("cd", 32): {ID: "gone"}, // orphan: no name
		"searxng":                {Name: "searxng"},
	}
	m.normalizeKeysLocked()
	if !m.configs["immich"].AutoUpdateEnabled {
		t.Fatal("ID-keyed entry not moved to its name")
	}
	if len(m.configs) != 2 {
		t.Fatalf("configs = %v", m.configs)
	}
}

// Opted-in containers were never updated unless "update all" was on: the
// timer only ran with the global switch.
func TestTheTimerRunsWhenOnlySomeContainersOptIn(t *testing.T) {
	m := testUpdateManager(t)
	m.configs["immich"] = ContainerUpdateInfo{Name: "immich", AutoUpdateEnabled: true}
	m.reschedule()
	if m.cronEntryID == 0 {
		t.Fatal("no timer although a container opted in")
	}
	m.configs["immich"] = ContainerUpdateInfo{Name: "immich"}
	m.reschedule()
	if m.cronEntryID != 0 {
		t.Fatal("timer still set with nothing to update")
	}
}
