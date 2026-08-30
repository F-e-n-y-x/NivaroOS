package modmanagement_test

import (
	"testing"

	modmanagement "github.com/F-e-n-y-x/recasa/services/common/pkg/mod_management"
	"github.com/stretchr/testify/assert"
)

func TestInstallableModules(t *testing.T) {
	t.Skip("skipping integration test requiring running mod_management service")
	client, err := modmanagement.NewClient(modmanagement.ModManagementClientOpts{})
	assert.NoError(t, err)
	modules, err := client.InstallableModules()
	assert.NoError(t, err)

	t.Log(modules)
}

func TestInstallModule(t *testing.T) {
	t.Skip("skipping integration test requiring running mod_management service")
	err := modmanagement.RequireModule("doconverter", "/var/run/casaos")
	assert.NoError(t, err)
}

func TestInstallNoExistModule(t *testing.T) {
	t.Skip("skipping integration test requiring running mod_management service")
	err := modmanagement.RequireModule("abc", "/var/run/casaos")
	assert.ErrorIs(t, err, modmanagement.ErrModuleNoInStore)
}
