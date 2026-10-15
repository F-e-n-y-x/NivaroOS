package route

import (
	"net/http"
	"net/http/httputil"
	"strings"
)

// rclone's OAuth sign-in (Settings > cloud drives, audit L8). `rclone
// authorize` serves its sign-in link and the provider's redirect back on
// 127.0.0.1:53682 only, so a browser on another device can't reach it. The
// gateway relays /v1/rclone-auth/* there (the terminal rewrites the printed
// link to it), on the dashboard's own port: nothing listens on 53682 off
// loopback, and nothing answers while no sign-in runs.
//
// Any local user could hold loopback 53682, so the relay never forwards the
// dashboard's credentials and its pages get a sandboxed (opaque) origin:
// no script there can read the dashboard's storage.
const (
	rcloneAuthPrefix = "/v1/rclone-auth"
	rcloneAuthAddr   = "127.0.0.1:53682"
)

func isRcloneAuthPath(p string) bool {
	return p == rcloneAuthPrefix || strings.HasPrefix(p, rcloneAuthPrefix+"/")
}

var rcloneAuthProxy = newRcloneAuthProxy(rcloneAuthAddr)

func newRcloneAuthProxy(addr string) http.Handler {
	strip := func(p string) string {
		if p = strings.TrimPrefix(p, rcloneAuthPrefix); p == "" {
			return "/"
		}
		return p
	}
	p := &httputil.ReverseProxy{
		Director: func(r *http.Request) {
			r.URL.Scheme, r.URL.Host = "http", addr
			r.URL.Path = strip(r.URL.Path)
			if r.URL.RawPath != "" {
				r.URL.RawPath = strip(r.URL.RawPath)
			}
			r.Header.Del("Authorization")
			r.Header.Del("Cookie")
		},
		ModifyResponse: func(res *http.Response) error {
			res.Header.Set("Content-Security-Policy", "sandbox")
			res.Header.Set("X-Content-Type-Options", "nosniff")
			res.Header.Del("Set-Cookie")
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "No rclone sign-in is running. Start it again from Settings.", http.StatusBadGateway)
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		p.ServeHTTP(w, r)
	})
}
