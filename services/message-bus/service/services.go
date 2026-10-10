package service

import (
	"context"
	"errors"
	"net/http"
	"strings"

	nivaroos_middleware "github.com/F-e-n-y-x/NivaroOS/services/common/middleware"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/model"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/repository"
)

type Services struct {
	EventTypeService *EventTypeService
	EventServiceWS   *EventServiceWS

	ActionTypeService *ActionTypeService
	ActionServiceWS   *ActionServiceWS

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

	if s.NotificationService != nil {
		go s.NotificationService.Start(ctx)
	}
}

// PublishEvent delivers an event to every live /event WebSocket subscriber.
func (s *Services) PublishEvent(event model.Event) {
	go s.EventServiceWS.Publish(event)
}

func NewServices(repository *repository.Repository) Services {
	eventTypeService := NewEventTypeService(repository)
	actionTypeService := NewActionTypeService(repository)

	return Services{
		EventTypeService: eventTypeService,
		EventServiceWS:   NewEventServiceWS(eventTypeService),

		ActionTypeService: actionTypeService,
		ActionServiceWS:   NewActionServiceWS(actionTypeService),
	}
}

// SubscriptionOriginAllowed is the Origin rule for event subscriptions
// (the /event and /action WebSockets):
//
//   - a subscription that carries an access token (Authorization header or
//     ?token=) passes: the router's JWT check decides. Auth is an explicit
//     token, never a cookie, so a cross-site page can't have one, and one
//     that does could use it from anywhere anyway - comparing Origin with
//     Host adds nothing, and it broke every browser behind an outer
//     reverse proxy that rewrites Host (nginx's default proxy_pass sends
//     Host: 127.0.0.1 and no X-Forwarded-Host);
//   - one without a token (only same-host automation gets past the JWT
//     check that way) must come from a non-browser client or a page on
//     this same host (CheckWebSocketOrigin), as before.
func SubscriptionOriginAllowed(r *http.Request) bool {
	if requestCarriesToken(r) {
		return true
	}
	return nivaroos_middleware.CheckWebSocketOrigin(r)
}

func requestCarriesToken(r *http.Request) bool {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")) != "" {
		return true
	}
	return strings.TrimSpace(r.URL.Query().Get("token")) != ""
}
