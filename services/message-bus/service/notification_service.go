package service

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/common"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/model"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/repository"
	"go.uber.org/zap"
)

// Feed events, published by the message bus itself (source "message-bus")
// on socket.io and on /event/message-bus:
//
//   - NotificationCreatedEvent: a new feed entry; its properties are the
//     entry (id, time, source_id, event_name, category, level, title,
//     message, key, args, action, icon), so a live client can add it
//     without fetching.
//   - NotificationStateEvent: user_id's read / dismissed state changed
//     (change = read | read_all | dismiss | dismiss_all, ids, up_to), so
//     that user's other devices refetch.
const (
	NotificationCreatedEvent = "message-bus:notification:created"
	NotificationStateEvent   = "message-bus:notification:state"
)

// Retention: whichever limit is reached first.
const (
	NotificationMaxAge   = 30 * 24 * time.Hour
	NotificationMaxItems = 1000

	notificationPruneEvery = time.Hour
	// An identical entry again within this window is the same thing
	// reported twice (a retry, two instances), not news.
	notificationDedupWindow = 2 * time.Second

	// Per-source ingestion limit: at most notificationSourceBurst new
	// entries per source in any notificationSourceWindow. Real producers
	// stay far below it (a backup run raises one or two, installing a
	// dozen apps a dozen); a flood from a publisher gone wrong - or one
	// spoofing a NivaroOS source - can then fill at most a small part of
	// the NotificationMaxItems history instead of evicting all of it.
	notificationSourceBurst  = 60
	notificationSourceWindow = 10 * time.Minute
)

var notificationEventTypes = []model.EventType{
	{SourceID: common.MessageBusSourceID, Name: NotificationCreatedEvent, PropertyTypeList: propertyTypes(
		"id", "time", "source_id", "event_name", "category", "level", "title", "message", "key", "args", "action", "icon")},
	{SourceID: common.MessageBusSourceID, Name: NotificationStateEvent, PropertyTypeList: propertyTypes(
		"user_id", "change", "ids", "up_to")},
}

func propertyTypes(names ...string) []model.PropertyType {
	out := make([]model.PropertyType, 0, len(names))
	for _, n := range names {
		out = append(out, model.PropertyType{Name: n})
	}
	return out
}

// NotificationService is the persisted notification feed: it keeps the
// events ClassifyNotification selects, applies retention and serves each
// user's view of it.
type NotificationService struct {
	store   *repository.NotificationStore
	publish func(model.Event)
	now     func() time.Time

	mu        sync.Mutex
	recent    map[string]time.Time // dedup fingerprints
	sinceTrim int
	// sourceBurst (0: unlimited) entries per source per sourceWindow.
	sourceBurst  int
	sourceWindow time.Duration
	perSource    map[string]*sourceWindowCount
}

type sourceWindowCount struct {
	start  time.Time
	count  int
	warned bool
}

// NewNotificationService: publish delivers the feed's own events to live
// subscribers (nil: none).
func NewNotificationService(store *repository.NotificationStore, publish func(model.Event)) *NotificationService {
	return &NotificationService{
		store: store, publish: publish, now: time.Now, recent: map[string]time.Time{},
		sourceBurst: notificationSourceBurst, sourceWindow: notificationSourceWindow,
		perSource: map[string]*sourceWindowCount{},
	}
}

// RegisterEventTypes registers the feed's own event types, so clients can
// subscribe to them on /event/message-bus like any other event.
func (s *NotificationService) RegisterEventTypes(types *EventTypeService) error {
	for _, t := range notificationEventTypes {
		if _, err := types.RegisterEventType(t); err != nil {
			return err
		}
	}
	return nil
}

// Start applies retention now and then every hour until ctx ends.
func (s *NotificationService) Start(ctx *context.Context) {
	s.prune()
	ticker := time.NewTicker(notificationPruneEvery)
	defer ticker.Stop()
	for {
		select {
		case <-(*ctx).Done():
			return
		case <-ticker.C:
			s.prune()
		}
	}
}

func (s *NotificationService) prune() {
	if n, err := s.store.Prune(s.now(), NotificationMaxAge, NotificationMaxItems); err != nil {
		logger.Error("notification feed: retention failed", zap.Error(err))
	} else if n > 0 {
		logger.Info("notification feed: retention removed entries", zap.Int64("count", n))
	}
}

// Ingest stores event in the feed when it is a notification, and tells
// live clients. It reports whether it stored one.
func (s *NotificationService) Ingest(event model.Event) (model.Notification, bool) {
	n, ok := ClassifyNotification(event)
	if !ok {
		return n, false
	}
	now := s.now()
	if s.duplicate(n, now) || !s.allowSource(n.SourceID, now) {
		return n, false
	}
	n.CreatedAt = now.UnixMilli()
	stored, err := s.store.Insert(n)
	if err != nil {
		logger.Error("notification feed: storing entry failed", zap.Error(err), zap.String("source_id", n.SourceID), zap.String("name", n.EventName))
		return n, false
	}

	// The count cap is cheap to keep exactly (a PK range delete); the
	// age limit is left to the hourly pass.
	if _, err := s.store.Prune(now, 0, NotificationMaxItems); err != nil {
		logger.Error("notification feed: retention failed", zap.Error(err))
	}

	s.emit(NotificationCreatedEvent, map[string]string{
		"id":         strconv.FormatInt(stored.ID, 10),
		"time":       time.UnixMilli(stored.CreatedAt).UTC().Format(time.RFC3339Nano),
		"source_id":  stored.SourceID,
		"event_name": stored.EventName,
		"category":   stored.Category,
		"level":      stored.Level,
		"title":      stored.Title,
		"message":    stored.Message,
		"key":        stored.Key,
		"args":       stored.Args,
		"action":     stored.Action,
		"icon":       stored.Icon,
	})
	return stored, true
}

func (s *NotificationService) duplicate(n model.Notification, now time.Time) bool {
	fp := strings.Join([]string{n.SourceID, n.EventName, n.Title, n.Message, n.Key, n.Args}, "\x00")
	s.mu.Lock()
	defer s.mu.Unlock()
	if at, ok := s.recent[fp]; ok && now.Sub(at) < notificationDedupWindow {
		return true
	}
	s.recent[fp] = now
	s.sinceTrim++
	if s.sinceTrim >= 64 {
		s.sinceTrim = 0
		for k, at := range s.recent {
			if now.Sub(at) >= notificationDedupWindow {
				delete(s.recent, k)
			}
		}
	}
	return false
}

// allowSource counts an entry against its source's window and reports
// whether it may be stored.
func (s *NotificationService) allowSource(source string, now time.Time) bool {
	if s.sourceBurst <= 0 {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.perSource[source]
	if w == nil || now.Sub(w.start) >= s.sourceWindow || now.Before(w.start) {
		w = &sourceWindowCount{start: now}
		s.perSource[source] = w // one entry per allowed source: bounded
	}
	if w.count >= s.sourceBurst {
		if !w.warned {
			w.warned = true
			logger.Error("notification feed: too many notifications from one source, dropping until the window ends",
				zap.String("source_id", source), zap.Int("limit", s.sourceBurst), zap.Duration("window", s.sourceWindow))
		}
		return false
	}
	w.count++
	return true
}

func (s *NotificationService) emit(name string, props map[string]string) {
	if s.publish == nil {
		return
	}
	s.publish(model.Event{SourceID: common.MessageBusSourceID, Name: name, Properties: props, Timestamp: s.now().Unix()})
}

func (s *NotificationService) emitState(userID int, change string, ids []int64, upTo int64) {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	props := map[string]string{"user_id": strconv.Itoa(userID), "change": change, "ids": strings.Join(parts, ","), "up_to": ""}
	if upTo > 0 {
		props["up_to"] = strconv.FormatInt(upTo, 10)
	}
	s.emit(NotificationStateEvent, props)
}

// List returns a page of a user's feed.
func (s *NotificationService) List(q model.NotificationQuery) (model.NotificationPage, error) {
	return s.store.List(q)
}

// MarkRead marks ids read for userID; all=true marks everything up to
// upTo (0: everything that exists now) instead.
func (s *NotificationService) MarkRead(userID int, ids []int64, all bool, upTo int64) (int64, error) {
	if all {
		applied, err := s.store.MarkAllRead(userID, upTo)
		if err != nil {
			return 0, err
		}
		s.emitState(userID, "read_all", nil, applied)
	} else {
		if err := s.store.MarkRead(userID, ids, s.now()); err != nil {
			return 0, err
		}
		s.emitState(userID, "read", ids, 0)
	}
	return s.store.UnreadCount(userID)
}

// Dismiss hides ids from userID's feed; all=true hides everything up to
// upTo (0: everything that exists now) instead.
func (s *NotificationService) Dismiss(userID int, ids []int64, all bool, upTo int64) (int64, error) {
	if all {
		applied, err := s.store.DismissAll(userID, upTo)
		if err != nil {
			return 0, err
		}
		s.emitState(userID, "dismiss_all", nil, applied)
	} else {
		if err := s.store.Dismiss(userID, ids, s.now()); err != nil {
			return 0, err
		}
		s.emitState(userID, "dismiss", ids, 0)
	}
	return s.store.UnreadCount(userID)
}
