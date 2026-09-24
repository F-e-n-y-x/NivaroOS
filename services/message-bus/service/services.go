package service

import (
	"context"
	"errors"

	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/model"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/repository"
)

type Services struct {
	EventTypeService *EventTypeService
	EventServiceWS   *EventServiceWS

	ActionTypeService *ActionTypeService
	ActionServiceWS   *ActionServiceWS

	SocketIOService *SocketIOService

	// NotificationService is the persisted notification feed; nil when
	// the process runs without one (the feed endpoints then answer 503).
	NotificationService *NotificationService
}

var (
	ErrInboundChannelNotFound     = errors.New("inbound channel not found")
	ErrSubscriberChannelsNotFound = errors.New("subscriber channels not found")
	ErrAlreadySubscribed          = errors.New("already subscribed")
)

func (s *Services) Start(ctx *context.Context) {
	go s.EventServiceWS.Start(ctx)
	go s.ActionServiceWS.Start(ctx)

	go s.SocketIOService.Start(ctx)

	if s.NotificationService != nil {
		go s.NotificationService.Start(ctx)
	}
}

// PublishEvent delivers an event to every live subscriber (socket.io and
// the /event WebSockets).
func (s *Services) PublishEvent(event model.Event) {
	go s.SocketIOService.Publish(event)
	go s.EventServiceWS.Publish(event)
}

func NewServices(repository *repository.Repository) Services {
	eventTypeService := NewEventTypeService(repository)
	actionTypeService := NewActionTypeService(repository)

	return Services{
		EventTypeService: eventTypeService,
		EventServiceWS:   NewEventServiceWS(eventTypeService),
		SocketIOService:  NewSocketIOService(),

		ActionTypeService: actionTypeService,
		ActionServiceWS:   NewActionServiceWS(actionTypeService),
	}
}
