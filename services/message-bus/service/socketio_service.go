package service

import (
	"context"
	"net/http"
	"strings"

	nivaroos_middleware "github.com/F-e-n-y-x/NivaroOS/services/common/middleware"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/model"
	socketio "github.com/googollee/go-socket.io"
	"github.com/googollee/go-socket.io/engineio"
	"github.com/googollee/go-socket.io/engineio/transport"
	"github.com/googollee/go-socket.io/engineio/transport/polling"
	"github.com/googollee/go-socket.io/engineio/transport/websocket"
	"go.uber.org/zap"
)

type SocketIOService struct {
	server *socketio.Server
}

func (s *SocketIOService) Publish(message interface{}) {
	if event, ok := message.(model.Event); ok {
		s.server.BroadcastToRoom("/", "event", event.Name, event)
		return
	}

	if action, ok := message.(model.Action); ok {
		s.server.BroadcastToRoom("/", "action", action.Name, action)
		return
	}

	logger.Error("unknown message type", zap.Any("message", message))
}

func (s *SocketIOService) Start(ctx *context.Context) {
	if err := s.server.Serve(); err != nil {
		logger.Error("error when serving socketio for events", zap.Error(err))
	}
}

func (s *SocketIOService) Server() *socketio.Server {
	return s.server
}

func NewSocketIOService() *SocketIOService {
	return &SocketIOService{
		server: buildServer(),
	}
}

// SubscriptionOriginAllowed is the Origin rule for event subscriptions
// (socket.io on both transports, /event and /action WebSockets):
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

func buildServer() *socketio.Server {
	// Same rule as the router's guard, which runs (and requires a valid
	// token) before a request ever gets here.
	websocketTransport := websocket.Default
	websocketTransport.CheckOrigin = SubscriptionOriginAllowed

	pollingTransport := polling.Default
	pollingTransport.CheckOrigin = SubscriptionOriginAllowed

	server := socketio.NewServer(&engineio.Options{
		Transports: []transport.Transport{
			websocketTransport,
			pollingTransport,
		},
	})

	server.OnConnect("/", func(s socketio.Conn) error {
		s.SetContext("")
		logger.Info("a socketio connection has started", zap.Any("remote_addr", s.RemoteAddr()))

		s.Join("event")
		s.Join("action")

		return nil
	})

	server.OnError("/", func(s socketio.Conn, e error) {
		logger.Error("error in socketio connnection", zap.Any("error", e))
	})

	server.OnDisconnect("/", func(s socketio.Conn, reason string) {
		logger.Info("a socketio connection is disconnected", zap.Any("reason", reason))
	})

	return server
}
