package route

import (
	"github.com/F-e-n-y-x/recasa/services/message-bus/codegen"
	"github.com/F-e-n-y-x/recasa/services/message-bus/service"
	jsoniter "github.com/json-iterator/go"
)

type APIRoute struct {
	services *service.Services
}

var json = jsoniter.ConfigCompatibleWithStandardLibrary

func NewAPIRoute(services *service.Services) codegen.ServerInterface {
	return &APIRoute{
		services: services,
	}
}
