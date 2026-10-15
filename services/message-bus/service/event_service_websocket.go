package service

import (
	"context"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"

	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/common"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/model"
	"go.uber.org/zap"
)

type EventServiceWS struct {
	typeService *EventTypeService

	ctx  *context.Context
	stop chan struct{}

	// mu guards everything below and ctx/stop: Subscribe and
	// Unsubscribe run on request goroutines while Start reads the map.
	mu sync.Mutex

	inboundChannel     chan model.Event
	subscriberChannels map[string]map[string][]chan model.Event
}

func (s *EventServiceWS) Publish(event model.Event) {
	s.mu.Lock()
	inbound, ctx := s.inboundChannel, s.ctx
	s.mu.Unlock()
	if inbound == nil { // not started yet, or stopped
		logger.Error("error when sending event via websocket", zap.Error(ErrInboundChannelNotFound))
		return
	}

	if event.Timestamp == 0 {
		event.Timestamp = time.Now().Unix()
	}

	// Properties aren't checked against the type's list: publishers send
	// extra ones. The route caps their size (readProperties).

	select {
	case inbound <- event:
	case <-(*ctx).Done():
	default: // drop event if no one is listening
	}
}

func (s *EventServiceWS) Subscribe(sourceID string, names []string) (chan model.Event, error) {
	if len(names) == 0 {
		eventTypes, err := s.typeService.GetEventTypesBySourceID(sourceID)
		if err != nil {
			return nil, err
		}

		for _, eventType := range eventTypes {
			names = append(names, eventType.Name)
		}
	}

	for _, name := range names {
		eventType, err := s.typeService.GetEventType(sourceID, name)
		if err != nil {
			return nil, err
		}

		if eventType == nil {
			return nil, ErrEventNameNotFound
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.subscriberChannels == nil {
		s.subscriberChannels = make(map[string]map[string][]chan model.Event)
	}
	if s.subscriberChannels[sourceID] == nil {
		s.subscriberChannels[sourceID] = make(map[string][]chan model.Event)
	}

	c := make(chan model.Event, 1)
	for _, name := range names {
		s.subscriberChannels[sourceID][name] = append(s.subscriberChannels[sourceID][name], c)
	}
	return c, nil
}

func (s *EventServiceWS) Unsubscribe(sourceID string, name string, c chan model.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.subscriberChannels == nil {
		return ErrSubscriberChannelsNotFound
	}
	if s.subscriberChannels[sourceID] == nil {
		return ErrEventSourceIDNotFound
	}
	if s.subscriberChannels[sourceID][name] == nil {
		return ErrEventNameNotFound
	}

	for i, subscriber := range s.subscriberChannels[sourceID][name] {
		if subscriber == c {
			s.subscriberChannels[sourceID][name] = append(s.subscriberChannels[sourceID][name][:i], s.subscriberChannels[sourceID][name][i+1:]...)
			return nil
		}
	}
	return nil
}

// send hands event to every subscriber of sourceID/name (all subscribers when
// both are empty), dropping it for any that isn't keeping up.
func (s *EventServiceWS) send(sourceID, name string, event model.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sid, source := range s.subscriberChannels {
		if sourceID != "" && sid != sourceID {
			continue
		}
		for n, subscribers := range source {
			if name != "" && n != name {
				continue
			}
			for _, c := range subscribers {
				select {
				case c <- event:
				default: // drop event if no one is listening
				}
			}
		}
	}
}

func (s *EventServiceWS) Start(ctx *context.Context) {
	s.mu.Lock()
	s.ctx = ctx
	inbound := make(chan model.Event)
	s.inboundChannel = inbound
	s.subscriberChannels = make(map[string]map[string][]chan model.Event)
	s.stop = make(chan struct{})
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, source := range s.subscriberChannels {
			for _, subscribers := range source {
				for _, subscriber := range subscribers {
					select {
					case _, ok := <-subscriber:
						if ok {
							close(subscriber)
						}
					default:
					}
				}
			}
		}
		s.subscriberChannels = nil
		// inbound is left open: a concurrent sender holding it must not
		// panic on a closed channel; nil makes new sends bail out.
		s.inboundChannel = nil
		close(s.stop)
		s.stop = nil
	}()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-(*ctx).Done():
			return

		case event := <-inbound:
			if event.SourceID == "" || event.Name == "" {
				continue // "" would mean "everyone" to send
			}
			s.send(event.SourceID, event.Name, event)

		case <-ticker.C:
			s.send("", "", model.Event{
				SourceID:  common.MessageBusSourceID,
				Name:      common.MessageBusHeartbeatName,
				Timestamp: time.Now().Unix(),
			})
		}
	}
}

func NewEventServiceWS(eventTypeService *EventTypeService) *EventServiceWS {
	return &EventServiceWS{
		typeService: eventTypeService,
	}
}
