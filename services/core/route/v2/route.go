package v2

import (
	"github.com/F-e-n-y-x/NivaroOS/services/core/codegen"
	"github.com/F-e-n-y-x/NivaroOS/services/core/service"
)

type NivaroOS struct {
	fileUploadService *service.FileUploadService
}

func NewNivaroOS() codegen.ServerInterface {
	return &NivaroOS{
		fileUploadService: service.NewFileUploadService(),
	}
}
