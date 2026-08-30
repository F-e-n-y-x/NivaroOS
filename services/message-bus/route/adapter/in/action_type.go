package in

import (
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/codegen"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/model"
)

func ActionTypeAdapter(actionType codegen.ActionType) model.ActionType {
	propertyTypeList := make([]model.PropertyType, 0)
	for _, propertyType := range actionType.PropertyTypeList {
		propertyTypeList = append(propertyTypeList, PropertyTypeAdapter(propertyType))
	}

	return model.ActionType{
		SourceID:         actionType.SourceID,
		Name:             actionType.Name,
		PropertyTypeList: propertyTypeList,
	}
}
