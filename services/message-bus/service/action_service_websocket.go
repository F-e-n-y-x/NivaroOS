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

type ActionServiceWS struct {
	typeService *ActionTypeService

	ctx  *context.Context
	stop chan struct{}

	// mu guards everything below and ctx/stop: Subscribe and
	// Unsubscribe run on request goroutines while Start reads the map.
	mu sync.Mutex

	inboundChannel     chan model.Action
	subscriberChannels map[string]map[string][]chan model.Action
}

func (s *ActionServiceWS) Trigger(action model.Action) {
	s.mu.Lock()
	inbound, ctx := s.inboundChannel, s.ctx
	s.mu.Unlock()
	if inbound == nil { // not started yet, or stopped
		logger.Error("error when sending action via websocket", zap.Error(ErrInboundChannelNotFound))
		return
	}

	if action.Timestamp == 0 {
		action.Timestamp = time.Now().Unix()
	}

	// Properties aren't checked against the type's list: publishers send
	// extra ones. The route caps their size (readProperties).

	select {
	case inbound <- action:
	case <-(*ctx).Done():
	default: // drop action if no one is listening
	}
}

func (s *ActionServiceWS) Subscribe(sourceID string, names []string) (chan model.Action, error) {
	if len(names) == 0 {
		actionTypes, err := s.typeService.GetActionTypesBySourceID(sourceID)
		if err != nil {
			return nil, err
		}

		for _, actionType := range actionTypes {
			names = append(names, actionType.Name)
		}
	}

	for _, name := range names {
		actionType, err := s.typeService.GetActionType(sourceID, name)
		if err != nil {
			return nil, err
		}

		if actionType == nil {
			return nil, ErrActionNameNotFound
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.subscriberChannels == nil {
		s.subscriberChannels = make(map[string]map[string][]chan model.Action)
	}
	if s.subscriberChannels[sourceID] == nil {
		s.subscriberChannels[sourceID] = make(map[string][]chan model.Action)
	}

	c := make(chan model.Action, 1)
	for _, name := range names {
		s.subscriberChannels[sourceID][name] = append(s.subscriberChannels[sourceID][name], c)
	}
	return c, nil
}

func (s *ActionServiceWS) Unsubscribe(sourceID string, name string, c chan model.Action) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.subscriberChannels == nil {
		return ErrSubscriberChannelsNotFound
	}
	if s.subscriberChannels[sourceID] == nil {
		return ErrActionSourceIDNotFound
	}
	if s.subscriberChannels[sourceID][name] == nil {
		return ErrActionNameNotFound
	}

	for i, subscriber := range s.subscriberChannels[sourceID][name] {
		if subscriber == c {
			s.subscriberChannels[sourceID][name] = append(s.subscriberChannels[sourceID][name][:i], s.subscriberChannels[sourceID][name][i+1:]...)
			return nil
		}
	}
	return nil
}

// send hands action to every subscriber of sourceID/name (all subscribers when
// both are empty), dropping it for any that isn't keeping up.
func (s *ActionServiceWS) send(sourceID, name string, action model.Action) {
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
				case c <- action:
				default: // drop action if no one is listening
				}
			}
		}
	}
}

func (s *ActionServiceWS) Start(ctx *context.Context) {
	s.mu.Lock()
	s.ctx = ctx
	inbound := make(chan model.Action)
	s.inboundChannel = inbound
	s.subscriberChannels = make(map[string]map[string][]chan model.Action)
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

		case action := <-inbound:
			if action.SourceID == "" || action.Name == "" {
				continue // "" would mean "everyone" to send
			}
			s.send(action.SourceID, action.Name, action)

		case <-ticker.C:
			s.send("", "", model.Action{
				SourceID:  common.MessageBusSourceID,
				Name:      common.MessageBusHeartbeatName,
				Timestamp: time.Now().Unix(),
			})
		}
	}
}

func NewActionServiceWS(actionTypeService *ActionTypeService) *ActionServiceWS {
	return &ActionServiceWS{
		typeService: actionTypeService,
	}
}
