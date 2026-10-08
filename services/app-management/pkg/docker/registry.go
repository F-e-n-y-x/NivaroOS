/*
credit: https://github.com/containrrr/watchtower
*/
package docker

import (
	"context"

	"github.com/moby/moby/client"
)

// GetPullOptions creates a struct with all options needed for pulling images from a registry
func GetPullOptions(imageName string) (client.ImagePullOptions, error) {
	auth, err := EncodedAuth(imageName)
	if err != nil {
		return client.ImagePullOptions{}, err
	}

	if auth == "" {
		return client.ImagePullOptions{}, nil
	}

	return client.ImagePullOptions{
		RegistryAuth:  auth,
		PrivilegeFunc: func(context.Context) (string, error) { return "", nil },
	}, nil
}
