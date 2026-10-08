package model

import (
	"runtime"
	"strconv"
	"strings"

	"github.com/F-e-n-y-x/NivaroOS/services/app-management/codegen"
	"github.com/F-e-n-y-x/NivaroOS/services/app-management/common"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/samber/lo"
)

func (p *PortMap) ServicePortConfig() (types.ServicePortConfig, error) {
	target, err := strconv.Atoi(p.ContainerPort)
	if err != nil {
		return types.ServicePortConfig{}, err
	}

	return types.ServicePortConfig{
		Target:    uint32(target),
		Published: p.CommendPort,
		Protocol:  p.Protocol,
	}, nil
}

func (p *PortArray) ServicePortConfigList() []types.ServicePortConfig {
	ports := []types.ServicePortConfig{}

	for _, port := range *p {
		servicePortConfig, err := port.ServicePortConfig()
		if err != nil {
			continue
		}

		ports = append(ports, servicePortConfig)
	}
	return ports
}

func (p *PortArray) PortStoreInfoList() []codegen.PortStoreInfo {
	return lo.Map(*p, func(p PortMap, i int) codegen.PortStoreInfo {
		return codegen.PortStoreInfo{
			Container:   p.ContainerPort,
			Description: map[string]string{common.DefaultLanguage: p.Desc},
		}
	})
}

func (p *PathArray) ServiceVolumeConfigList() []types.ServiceVolumeConfig {
	volumes := []types.ServiceVolumeConfig{}

	for _, path := range *p {

		volumeType := "volume"
		if strings.Contains(path.Path, "/") {
			volumeType = "bind"
		}

		volumes = append(volumes, types.ServiceVolumeConfig{
			Type:   volumeType,
			Source: path.Path,
			Target: path.ContainerPath,
		})
	}
	return volumes
}

func (p *PathArray) DeviceMappings() []types.DeviceMapping {
	return lo.Map(*p, func(p PathMap, i int) types.DeviceMapping {
		return types.DeviceMapping{Source: p.Path, Target: p.ContainerPath, Permissions: "rwm"}
	})
}

func (p *PathArray) DeviceStoreInfoList() []codegen.DeviceStoreInfo {
	return lo.Map(*p, func(p PathMap, i int) codegen.DeviceStoreInfo {
		return codegen.DeviceStoreInfo{
			Container:   &p.ContainerPath,
			Description: &map[string]string{common.DefaultLanguage: p.Desc},
		}
	})
}

func (p *PathArray) VolumeStoreInfoList() []codegen.VolumeStoreInfo {
	return lo.Map(*p, func(p PathMap, i int) codegen.VolumeStoreInfo {
		return codegen.VolumeStoreInfo{
			Container:   p.ContainerPath,
			Description: map[string]string{common.DefaultLanguage: p.Desc},
		}
	})
}

func (ea *EnvArray) ToMappingWithEquals() types.MappingWithEquals {
	return lo.SliceToMap(*ea, func(e Env) (string, *string) {
		return e.Name, &e.Value
	})
}

func (ea *EnvArray) EnvStoreInfoList() []codegen.EnvStoreInfo {
	return lo.Map(*ea, func(e Env, i int) codegen.EnvStoreInfo {
		return codegen.EnvStoreInfo{
			Container:   e.Name,
			Description: map[string]string{common.DefaultLanguage: e.Desc},
		}
	})
}

func (c *CustomizationPostData) AppStoreInfo() codegen.AppStoreInfo {
	return codegen.AppStoreInfo{
		Devices: c.Devices.DeviceStoreInfoList(),
		Envs:    c.Envs.EnvStoreInfoList(),
		Ports:   c.Ports.PortStoreInfoList(),
		Volumes: c.Volumes.VolumeStoreInfoList(),
	}
}

func (c *CustomizationPostData) ComposeAppStoreInfo() codegen.ComposeAppStoreInfo {
	name := strings.ToLower(c.ContainerName)

	message := "This is a compose app converted from a legacy app (CasaOS v0.4.3 or earlier)"

	return codegen.ComposeAppStoreInfo{
		Architectures: &[]string{runtime.GOARCH},
		Author:        "NivaroOS User",
		Category:      "unknown",
		Description:   map[string]string{common.DefaultLanguage: c.Description},
		Developer:     "unknown",
		Icon:          c.Icon,
		Index:         c.Index,
		Main:          &name,
		PortMap:       c.PortMap,
		Scheme:        (*codegen.Scheme)(&c.Protocol),
		Tagline:       map[string]string{common.DefaultLanguage: message},
		Tips:          codegen.TipsStoreInfo{Custom: &message},
		Title:         map[string]string{common.DefaultLanguage: c.Label},
	}
}

func (c *CustomizationPostData) Services() types.Services {
	s := types.ServiceConfig{
		Name:    strings.ToLower(c.ContainerName),
		Restart: c.Restart,
		Extensions: map[string]interface{}{
			common.ComposeExtensionNameXCasaOS: c.AppStoreInfo(),
		},
	}
	s.CapAdd = c.CapAdd
	s.Command = emtpySliceThenNil(c.Cmd)
	s.CPUShares = c.CPUShares
	s.Devices = c.Devices.DeviceMappings()
	s.Environment = c.Envs.ToMappingWithEquals()
	s.Image = c.Image
	s.NetworkMode = c.NetworkModel
	s.Ports = c.Ports.ServicePortConfigList()
	s.Privileged = c.Privileged
	s.Volumes = c.Volumes.ServiceVolumeConfigList()

	return types.Services{s.Name: s}
}

func (c *CustomizationPostData) Compose() codegen.ComposeApp {
	return codegen.ComposeApp{
		Name:     strings.ToLower(c.ContainerName),
		Services: c.Services(),
		Extensions: map[string]interface{}{
			common.ComposeExtensionNameXCasaOS: c.ComposeAppStoreInfo(),
		},
	}
}

func emtpySliceThenNil[T any](arr []T) []T {
	if len(arr) == 0 {
		return nil
	}

	return arr
}
