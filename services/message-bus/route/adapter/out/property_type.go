package out

import (
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/codegen"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/model"
)

func PropertyTypeAdapter(propertyType model.PropertyType) codegen.PropertyType {
	return codegen.PropertyType{
		Name: propertyType.Name,
	}
}
