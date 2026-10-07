package common

import (
	"github.com/F-e-n-y-x/NivaroOS/services/core/codegen/message_bus"
)

// devtype -> action -> event
var EventTypes = []message_bus.EventType{
	{Name: "nivaroos:system:utilization", SourceID: SERVICENAME, PropertyTypeList: []message_bus.PropertyType{}},
	// The 500 ms readings between the 5 s ones while a client holds the fast lease (route/utilization_rate.go).
	{Name: "nivaroos:system:utilization:live", SourceID: SERVICENAME, PropertyTypeList: []message_bus.PropertyType{}},
	{Name: "nivaroos:file:recover", SourceID: SERVICENAME, PropertyTypeList: []message_bus.PropertyType{}},
	{Name: "nivaroos:file:operate", SourceID: SERVICENAME, PropertyTypeList: []message_bus.PropertyType{}},
	{Name: "nivaroos:file:changed", SourceID: SERVICENAME, PropertyTypeList: []message_bus.PropertyType{}},
}
