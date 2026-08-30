package in

import (
	"github.com/F-e-n-y-x/recasa/services/message-bus/codegen"
	"github.com/F-e-n-y-x/recasa/services/message-bus/model"
)

func ActionAdapter(action codegen.Action) model.Action {
	var timestamp int64
	if action.Timestamp != nil {
		timestamp = action.Timestamp.Unix()
	}

	return model.Action{
		SourceID:   action.SourceID,
		Name:       action.Name,
		Properties: action.Properties,
		Timestamp:  timestamp,
	}
}
