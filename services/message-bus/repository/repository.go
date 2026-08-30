package repository

import "github.com/F-e-n-y-x/recasa/services/message-bus/model"

type Repository interface {
	GetEventTypes() ([]model.EventType, error)
	RegisterEventType(eventType model.EventType) (*model.EventType, error)
	GetEventTypesBySourceID(sourceID string) ([]model.EventType, error)
	GetEventType(sourceID string, name string) (*model.EventType, error)

	GetActionTypes() ([]model.ActionType, error)
	RegisterActionType(actionType model.ActionType) (*model.ActionType, error)
	GetActionTypesBySourceID(sourceID string) ([]model.ActionType, error)
	GetActionType(sourceID string, name string) (*model.ActionType, error)

	Close()
}
