package in

import (
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/codegen"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/model"
)

func PropertyTypeAdapter(propertyType codegen.PropertyType) model.PropertyType {
	return model.PropertyType{
		Name: propertyType.Name,
	}
}
