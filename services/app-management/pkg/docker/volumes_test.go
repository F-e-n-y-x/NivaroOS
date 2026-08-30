package docker_test

import (
	"fmt"
	"testing"

	"github.com/F-e-n-y-x/recasa/services/app-management/pkg/docker"
)

func TestGetDir(t *testing.T) {
	fmt.Println(docker.GetDir("", "config"))
}
