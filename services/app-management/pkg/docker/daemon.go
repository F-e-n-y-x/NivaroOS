package docker

import (
	"context"

	"github.com/moby/moby/client"
)

func IsDaemonRunning() bool {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return false
	}
	defer cli.Close()

	_, err = cli.Ping(context.Background(), client.PingOptions{})
	return err == nil
}

func CurrentArchitecture() (string, error) {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return "", err
	}
	defer cli.Close()

	ver, err := cli.ServerVersion(context.Background(), client.ServerVersionOptions{})
	if err != nil {
		return "", err
	}

	return ver.Arch, nil
}
