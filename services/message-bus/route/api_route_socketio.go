package route

import (
	"net/http"

	nivaroos_middleware "github.com/F-e-n-y-x/NivaroOS/services/common/middleware"
	"github.com/labstack/echo/v4"
)

func (r *APIRoute) SubscribeSIO(ctx echo.Context) error {
	r.serveSIO(ctx)
	return nil
}

// unfortunately need to duplicate the func to support both `/socket.io` and `/socket.io/` (with a trailing slash) API endpoints
func (r *APIRoute) SubscribeSIO2(ctx echo.Context) error {
	return r.SubscribeSIO(ctx)
}

func (r *APIRoute) PollSIO(ctx echo.Context) error {
	r.serveSIO(ctx)
	return nil
}

// unfortunately need to duplicate the func to support both `/socket.io` and `/socket.io/` (with a trailing slash) API endpoints
func (r *APIRoute) PollSIO2(ctx echo.Context) error {
	return r.PollSIO(ctx)
}

// serveSIO hands the request to socket.io. Its polling transport echoes
// any Origin back in Access-Control-Allow-Origin (with credentials); the
// router's CORS policy is same-host pages only, so those headers are
// dropped for any other page (a browser then won't let that page read the
// answer). WebSocket upgrades are passed through untouched.
func (r *APIRoute) serveSIO(ctx echo.Context) {
	server := r.services.SocketIOService.Server()
	req := ctx.Request()
	var w http.ResponseWriter = ctx.Response()
	if req.Header.Get("Origin") != "" && !nivaroos_middleware.SameHostOrigin(req) && req.URL.Query().Get("transport") != "websocket" {
		w = &noCrossSiteCORS{ResponseWriter: w}
	}
	server.ServeHTTP(w, req)
}

type noCrossSiteCORS struct {
	http.ResponseWriter
	wrote bool
}

func (w *noCrossSiteCORS) strip() {
	if w.wrote {
		return
	}
	w.wrote = true
	h := w.ResponseWriter.Header()
	for _, k := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Credentials", "Access-Control-Allow-Headers"} {
		h.Del(k)
	}
}

func (w *noCrossSiteCORS) WriteHeader(code int) {
	w.strip()
	w.ResponseWriter.WriteHeader(code)
}

func (w *noCrossSiteCORS) Write(b []byte) (int, error) {
	w.strip()
	return w.ResponseWriter.Write(b)
}

func (w *noCrossSiteCORS) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		w.strip()
		f.Flush()
	}
}
