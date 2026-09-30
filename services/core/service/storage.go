package service

import (
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/httper"
)

// StorageService is what core still does with rclone remotes through the
// rclone daemon: config only (the legacy /v1/recover OAuth callback).
// Mounting is nivaroos-local-storage's job alone.
type StorageService interface {
	CreateConfig(data map[string]string, name string, t string) error
	GetConfigByName(name string) (map[string]string, error)
	DeleteConfigByName(name string) error
	GetConfig() (httper.RemotesResult, error)
}

type storageStruct struct {
}

func (s *storageStruct) CreateConfig(data map[string]string, name string, t string) error {
	httper.CreateConfig(data, name, t)
	return nil
}

func (s *storageStruct) GetConfigByName(name string) (map[string]string, error) {
	return httper.GetConfigByName(name)
}
func (s *storageStruct) DeleteConfigByName(name string) error {
	return httper.DeleteConfigByName(name)
}
func (s *storageStruct) GetConfig() (httper.RemotesResult, error) {
	section, err := httper.GetAllConfigName()
	if err != nil {
		return httper.RemotesResult{}, err
	}
	return section, nil
}
func NewStorageService() StorageService {
	return &storageStruct{}
}
