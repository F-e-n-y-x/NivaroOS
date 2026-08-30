package config

import (
	"path/filepath"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/constants"
)

var (
	AppManagementConfigFilePath    = filepath.Join(constants.DefaultConfigPath, "app-management.conf")
	AppManagementGlobalEnvFilePath = filepath.Join(constants.DefaultConfigPath, "env")
	RemoveRuntimeIfNoNvidiaGPUFlag = false
)
