package common

import "github.com/F-e-n-y-x/NivaroOS/services/local-storage/codegen/message_bus"

// Message-bus events for async storage jobs (POST/PUT /v1/storage).
const (
	StorageJobEventProgress = "local-storage:storage-job:progress"
	StorageJobEventEnd      = "local-storage:storage-job:end"
	StorageJobEventError    = "local-storage:storage-job:error"
)

var storageJobPropertyNames = []string{
	"local-storage:job_id",
	"local-storage:job_kind",
	"local-storage:path",
	"local-storage:state",
	"local-storage:step",
	"local-storage:message",
	"local-storage:mount_point",
}

// StorageJobEventTypes are registered with the message bus at startup.
func StorageJobEventTypes() []message_bus.EventType {
	props := make([]message_bus.PropertyType, 0, len(storageJobPropertyNames))
	for _, n := range storageJobPropertyNames {
		props = append(props, message_bus.PropertyType{Name: n})
	}
	out := []message_bus.EventType{}
	for _, name := range []string{StorageJobEventProgress, StorageJobEventEnd, StorageJobEventError} {
		out = append(out, message_bus.EventType{SourceID: ServiceName, Name: name, PropertyTypeList: props})
	}
	return out
}
