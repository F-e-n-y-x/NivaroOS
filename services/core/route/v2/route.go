package v2

import (
	"github.com/F-e-n-y-x/recasa/services/core/codegen"
	"github.com/F-e-n-y-x/recasa/services/core/service"
)

type CasaOS struct {
	fileUploadService *service.FileUploadService
}

func NewCasaOS() codegen.ServerInterface {
	return &CasaOS{
		fileUploadService: service.NewFileUploadService(),
	}
}
