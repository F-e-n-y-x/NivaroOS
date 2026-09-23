package service_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/app-management/service"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
)

// These run against the real Docker daemon with throwaway containers
// (named nvtest-*), because the bugs were in how the daemon's state was
// carried over - nothing a fake would show.

func dockerCLI(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func needDocker(t *testing.T) {
	t.Helper()
	if exec.Command("docker", "info").Run() != nil {
		t.Skip("docker not available")
	}
	logger.LogInitConsoleOnly()
}

// newContainer creates (but doesn't start) a container and removes it and
// any recreated copy at the end.
func newContainer(t *testing.T, name, image string) string {
	t.Helper()
	exec.Command("docker", "rm", "-f", name).Run()
	id := dockerCLI(t, "create", "--name", name, image, "sleep", "600")
	t.Cleanup(func() { exec.Command("docker", "rm", "-f", name).Run() })
	return id
}

func state(t *testing.T, name string) (id string, running bool) {
	t.Helper()
	out := dockerCLI(t, "inspect", "-f", "{{.Id}} {{.State.Running}}", name)
	f := strings.Fields(out)
	return f[0], f[1] == "true"
}

// The nightly auto-update restarted containers the owner had stopped.
func TestRecreateKeepsAStoppedContainerStopped(t *testing.T) {
	needDocker(t)
	name := "nvtest-stopped"
	old := newContainer(t, name, "debian:latest")

	if err := service.NewDockerService().RecreateContainer(context.Background(), old, false, true); err != nil {
		t.Fatal(err)
	}
	id, running := state(t, name)
	if id == old {
		t.Fatal("container was not recreated")
	}
	if running {
		t.Fatal("a stopped container was started by the recreate")
	}
}

func TestRecreateKeepsARunningContainerRunning(t *testing.T) {
	needDocker(t)
	name := "nvtest-running"
	old := newContainer(t, name, "debian:latest")
	dockerCLI(t, "start", name)

	if err := service.NewDockerService().RecreateContainer(context.Background(), old, false, true); err != nil {
		t.Fatal(err)
	}
	if id, running := state(t, name); id == old || !running {
		t.Fatalf("id changed=%v running=%v", id != old, running)
	}
}

// An update whose pull failed was logged and then reported as success.
func TestUpdateWhosePullFailsIsAnErrorAndChangesNothing(t *testing.T) {
	needDocker(t)
	dockerCLI(t, "tag", "debian:latest", "nvtest-local-only:1")
	t.Cleanup(func() { exec.Command("docker", "rmi", "nvtest-local-only:1").Run() })
	name := "nvtest-local"
	old := newContainer(t, name, "nvtest-local-only:1")

	err := service.NewDockerService().RecreateContainer(context.Background(), old, true, false)
	if err == nil {
		t.Fatal("pull of an image that exists in no registry reported success")
	}
	if id, _ := state(t, name); id != old {
		t.Fatal("container was recreated although the pull failed")
	}
}

// Recreating every container every night even when nothing changed.
func TestUpdateWithNoNewerImageLeavesTheContainerAlone(t *testing.T) {
	needDocker(t)
	if exec.Command("docker", "pull", "-q", "busybox:latest").Run() != nil {
		t.Skip("no registry access")
	}
	name := "nvtest-current"
	old := newContainer(t, name, "busybox:latest")

	if err := service.NewDockerService().RecreateContainer(context.Background(), old, true, false); err != nil {
		t.Fatal(err)
	}
	if id, _ := state(t, name); id != old {
		t.Fatal("container was recreated although its image is already the latest")
	}
}
