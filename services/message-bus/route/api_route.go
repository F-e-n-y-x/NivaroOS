package route

import (
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/codegen"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/service"
)

type APIRoute struct {
	services *service.Services
}

func NewAPIRoute(services *service.Services) codegen.ServerInterface {
	return &APIRoute{
		services: services,
	}
}
