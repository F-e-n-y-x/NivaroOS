/*
credit: https://github.com/containrrr/watchtower
*/
package docker

import (
	"context"
	"fmt"
	"strings"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/samber/lo"
)

func ImageName(containerInfo *container.InspectResponse) string {
	imageName := containerInfo.Config.Image

	if !strings.Contains(imageName, ":") {
		imageName = imageName + ":latest"
	}

	return imageName
}

func Container(ctx context.Context, id string) (*container.InspectResponse, error) {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	result, err := cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return nil, err
	}

	return &result.Container, nil
}

func CloneContainer(ctx context.Context, id string, newName string) (string, error) {
	containerInfo, err := Container(ctx, id)
	if err != nil {
		return "", err
	}

	imageInfo, err := Image(ctx, containerInfo.Image)
	if err != nil {
		return "", err
	}

	config := runtimeConfig(containerInfo, imageInfo)

	// RecreateContainer clones under a temporary "<name>-XXXX" name (see
	// caller) before removing the old container and renaming the clone back
	// to the original name. A plain (non-casaos-labeled) container has no
	// other stable identity - the frontend keys folder placement and icon/
	// display overrides on this container's name - so without this label,
	// the temp-named clone briefly looks like a brand new, never-seen
	// container mid-update and gets dropped into "Other Containers" with a
	// default icon. Recording the pre-clone name here lets the container
	// list report the real name throughout the whole recreate window,
	// before the final rename below ever happens.
	if config.Labels == nil {
		config.Labels = map[string]string{}
	}
	config.Labels["nivaroos.recreate_original_name"] = strings.TrimPrefix(containerInfo.Name, "/")

	hostConfig := hostConfig(containerInfo)
	networkConfig := &network.NetworkingConfig{EndpointsConfig: containerInfo.NetworkSettings.Networks}
	simpleNetworkConfig := simpleNetworkConfig(networkConfig)

	cli, err := client.New(client.FromEnv)
	if err != nil {
		return "", err
	}
	defer cli.Close()

	newContainer, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:           config,
		HostConfig:       hostConfig,
		NetworkingConfig: simpleNetworkConfig,
		Name:             newName,
	})
	if err != nil {
		return "", err
	}

	if !(hostConfig.NetworkMode.IsHost()) {
		for k := range simpleNetworkConfig.EndpointsConfig {
			if _, err := cli.NetworkDisconnect(ctx, k, client.NetworkDisconnectOptions{Container: newContainer.ID, Force: true}); err != nil {
				return newContainer.ID, err
			}
		}

		for k, v := range networkConfig.EndpointsConfig {
			if _, err := cli.NetworkConnect(ctx, k, client.NetworkConnectOptions{Container: newContainer.ID, EndpointConfig: v}); err != nil {
				return newContainer.ID, err
			}
		}
	}

	return newContainer.ID, nil
}

func RemoveContainer(ctx context.Context, id string) error {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return err
	}
	defer cli.Close()

	_, err = cli.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true})
	return err
}

func RenameContainer(ctx context.Context, id string, name string) error {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return err
	}
	defer cli.Close()

	_, err = cli.ContainerRename(ctx, id, client.ContainerRenameOptions{NewName: name})
	return err
}

func StartContainer(ctx context.Context, id string) error {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return err
	}
	defer cli.Close()

	containerInfo, err := cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return err
	}

	if !containerInfo.Container.State.Running {
		_, err = cli.ContainerStart(ctx, id, client.ContainerStartOptions{})
		return err
	}

	return nil
}

func StopContainer(ctx context.Context, id string) error {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return err
	}
	defer cli.Close()

	containerInfo, err := cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return err
	}

	if containerInfo.Container.State.Running {
		if _, err := cli.ContainerStop(ctx, id, client.ContainerStopOptions{}); err != nil {
			return err
		}

		if err := WaitContainer(ctx, id, container.WaitConditionNotRunning); err != nil {
			return err
		}
	}

	return nil
}

func WaitContainer(ctx context.Context, id string, condition container.WaitCondition) error {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return err
	}
	defer cli.Close()

	wait := cli.ContainerWait(ctx, id, client.ContainerWaitOptions{Condition: condition})
	select {
	case err := <-wait.Error:
		return err
	case <-wait.Result:
		return nil
	}
}

func runtimeConfig(containerInfo *container.InspectResponse, imageInfo *image.InspectResponse) *container.Config {
	config := containerInfo.Config
	hostConfig := containerInfo.HostConfig
	imageConfig := imageInfo.Config

	if config.WorkingDir == imageConfig.WorkingDir {
		config.WorkingDir = ""
	}

	if config.User == imageConfig.User {
		config.User = ""
	}

	if hostConfig.NetworkMode.IsContainer() {
		config.Hostname = ""
	}

	if utils.CompareStringSlices(config.Entrypoint, imageConfig.Entrypoint) {
		config.Entrypoint = nil
		if utils.CompareStringSlices(config.Cmd, imageConfig.Cmd) {
			config.Cmd = nil
		}
	}

	config.Env = lo.Filter(config.Env, func(s string, i int) bool { return !lo.Contains(imageConfig.Env, s) })

	config.Labels = lo.OmitBy(config.Labels, func(k string, v string) bool {
		v2, ok := imageConfig.Labels[k]
		return ok && v == v2
	})

	config.Volumes = lo.OmitBy(config.Volumes, func(k string, v struct{}) bool {
		v2, ok := imageConfig.Volumes[k]
		return ok && v == v2
	})

	// subtract ports exposed in image from container
	for k := range config.ExposedPorts {
		if _, ok := imageConfig.ExposedPorts[k.String()]; ok {
			delete(config.ExposedPorts, k)
		}
	}

	for p := range containerInfo.HostConfig.PortBindings {
		config.ExposedPorts[p] = struct{}{}
	}

	config.Image = ImageName(containerInfo)
	return config
}

func hostConfig(containerInfo *container.InspectResponse) *container.HostConfig {
	hostConfig := containerInfo.HostConfig

	for i, link := range hostConfig.Links {
		name := link[0:strings.Index(link, ":")]
		alias := link[strings.LastIndex(link, "/"):]

		hostConfig.Links[i] = fmt.Sprintf("%s:%s", name, alias)
	}

	return hostConfig
}

// simpleNetworkConfig is a networkConfig with only 1 network.
// see: https://github.com/docker/docker/issues/29265
func simpleNetworkConfig(networkConfig *network.NetworkingConfig) *network.NetworkingConfig {
	oneEndpoint := make(map[string]*network.EndpointSettings)
	for k, v := range networkConfig.EndpointsConfig {
		oneEndpoint[k] = v
		// we only need 1
		break
	}
	return &network.NetworkingConfig{EndpointsConfig: oneEndpoint}
}
