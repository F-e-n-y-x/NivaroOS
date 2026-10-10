package service

import (
	"errors"

	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/model"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/repository"
)

type ActionTypeService struct {
	repository *repository.Repository
}

var (
	ErrActionSourceIDNotFound = errors.New("event source id not found")
	ErrActionNameNotFound     = errors.New("event name not found")
)

func (s *ActionTypeService) GetActionTypes() ([]model.ActionType, error) {
	return (*s.repository).GetActionTypes()
}

func (s *ActionTypeService) RegisterActionType(actionType model.ActionType) (*model.ActionType, error) {
	if err := ValidateType(actionType.SourceID, actionType.Name, actionType.PropertyTypeList); err != nil {
		return nil, err
	}
	return (*s.repository).RegisterActionType(actionType)
}

func (s *ActionTypeService) GetActionTypesBySourceID(sourceID string) ([]model.ActionType, error) {
	return (*s.repository).GetActionTypesBySourceID(sourceID)
}

func (s *ActionTypeService) GetActionType(sourceID string, name string) (*model.ActionType, error) {
	return (*s.repository).GetActionType(sourceID, name)
}

func NewActionTypeService(repository *repository.Repository) *ActionTypeService {
	return &ActionTypeService{
		repository: repository,
	}
}
