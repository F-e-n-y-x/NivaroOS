package docker_test

import (
	"context"
	"io"
	"runtime"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/app-management/pkg/docker"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/random"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/samber/lo"
	"go.uber.org/goleak"
	"gotest.tools/v3/assert"
)

func setupTestContainer(ctx context.Context, t *testing.T) *client.ContainerCreateResult {
	cli, err := client.New(client.FromEnv)
	assert.NilError(t, err)
	defer cli.Close()

	imageName := "alpine:latest"

	config := &container.Config{
		Image: imageName,
		Cmd:   []string{"tail", "-f", "/dev/null"},
		Env:   []string{"FOO=BAR"},
	}

	hostConfig := &container.HostConfig{}
	networkingConfig := &network.NetworkingConfig{}

	out, err := cli.ImagePull(ctx, imageName, client.ImagePullOptions{})
	assert.NilError(t, err)

	_, err = io.ReadAll(out)
	assert.NilError(t, err)

	response, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:           config,
		HostConfig:       hostConfig,
		NetworkingConfig: networkingConfig,
		Name:             "test-" + random.RandomString(4, false),
	})
	assert.NilError(t, err)

	return &response
}

func TestCloneContainer(t *testing.T) {
	defer goleak.VerifyNone(t)

	defer func() {
		// workaround due to https://github.com/patrickmn/go-cache/issues/166
		docker.Cache = nil
		runtime.GC()
	}()

	if !docker.IsDaemonRunning() {
		t.Skip("Docker daemon is not running")
	}

	cli, err := client.New(client.FromEnv)
	assert.NilError(t, err)
	defer cli.Close()

	ctx := context.Background()

	// setup
	response := setupTestContainer(ctx, t)

	defer func() {
		_, err = cli.ContainerRemove(ctx, response.ID, client.ContainerRemoveOptions{})
		assert.NilError(t, err)
	}()

	err = docker.StartContainer(ctx, response.ID)
	assert.NilError(t, err)

	defer func() {
		err = docker.StopContainer(ctx, response.ID)
		assert.NilError(t, err)
	}()

	newID, err := docker.CloneContainer(ctx, response.ID, "test-"+random.RandomString(4, false))
	assert.NilError(t, err)

	defer func() {
		err := docker.RemoveContainer(ctx, newID)
		assert.NilError(t, err)
	}()

	err = docker.StartContainer(ctx, newID)
	assert.NilError(t, err)

	defer func() {
		err := docker.StopContainer(ctx, newID)
		assert.NilError(t, err)
	}()

	containerInfo, err := docker.Container(ctx, newID)
	assert.NilError(t, err)
	assert.Assert(t, lo.Contains(containerInfo.Config.Env, "FOO=BAR"))
}

func TestNonExistingContainer(t *testing.T) {
	containerInfo, err := docker.Container(context.Background(), "non-existing-container")
	assert.ErrorContains(t, err, "non-existing-container")
	assert.Assert(t, containerInfo == nil)
}
