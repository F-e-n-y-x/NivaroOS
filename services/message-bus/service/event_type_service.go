package service

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/model"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/repository"
)

type EventTypeService struct {
	repository *repository.Repository
}

var (
	ErrEventSourceIDNotFound = errors.New("event source id not found")
	ErrEventNameNotFound     = errors.New("event name not found")
)

func (s *EventTypeService) GetEventTypes() ([]model.EventType, error) {
	return (*s.repository).GetEventTypes()
}

// typeNameRE: source IDs, type names and property names go in URL paths
// and query strings unescaped by most clients. Every name in use matches
// (e.g. "nivaroos-download", "docker:image:pull-progress", "app:name").
var typeNameRE = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

const maxPropertyTypes = 64

// ValidateType checks an event or action type before it is registered.
func ValidateType(sourceID, name string, props []model.PropertyType) error {
	for _, n := range [][2]string{{"source ID", sourceID}, {"name", name}} {
		if !typeNameRE.MatchString(n[1]) {
			return fmt.Errorf("invalid %s %q: use 1-128 letters, digits, '.', '_', ':' or '-'", n[0], n[1])
		}
	}
	if len(props) > maxPropertyTypes {
		return fmt.Errorf("%s: too many property types (%d, max %d)", name, len(props), maxPropertyTypes)
	}
	for _, p := range props {
		if !typeNameRE.MatchString(p.Name) {
			return fmt.Errorf("%s: invalid property name %q: use 1-128 letters, digits, '.', '_', ':' or '-'", name, p.Name)
		}
	}
	return nil
}

func (s *EventTypeService) RegisterEventType(eventType model.EventType) (*model.EventType, error) {
	if err := ValidateType(eventType.SourceID, eventType.Name, eventType.PropertyTypeList); err != nil {
		return nil, err
	}
	return (*s.repository).RegisterEventType(eventType)
}

func (s *EventTypeService) GetEventTypesBySourceID(sourceID string) ([]model.EventType, error) {
	return (*s.repository).GetEventTypesBySourceID(sourceID)
}

func (s *EventTypeService) GetEventType(sourceID string, name string) (*model.EventType, error) {
	return (*s.repository).GetEventType(sourceID, name)
}

func NewEventTypeService(repository *repository.Repository) *EventTypeService {
	return &EventTypeService{
		repository: repository,
	}
}
