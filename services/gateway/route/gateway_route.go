package route

import (
	"io"
	"net/http"
	"net/http/httputil"
	"strings"

	nivaroos_middleware "github.com/F-e-n-y-x/NivaroOS/services/common/middleware"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/gateway/service"
	"go.uber.org/zap"
)

// gpuPathPrefix is where the UI reaches nivaroos-gpu-sidecar same-origin:
// /v1/gpu/<endpoint> is proxied to http://127.0.0.1:28640/<endpoint>
// (prefix stripped). The sidecar only listens on loopback and validates the
// user's JWT itself (the gateway does not authenticate proxied requests).
const (
	gpuPathPrefix  = "/v1/gpu"
	gpuSidecarAddr = "127.0.0.1:28640"
)

func isGPUPath(p string) bool {
	return p == gpuPathPrefix || strings.HasPrefix(p, gpuPathPrefix+"/")
}

func stripGPUPrefix(p string) string {
	p = strings.TrimPrefix(p, gpuPathPrefix)
	if p == "" {
		return "/"
	}
	return p
}

var gpuProxy = &httputil.ReverseProxy{
	Director: func(r *http.Request) {
		r.URL.Scheme = "http"
		r.URL.Host = gpuSidecarAddr
		r.URL.Path = stripGPUPrefix(r.URL.Path)
		if r.URL.RawPath != "" {
			r.URL.RawPath = stripGPUPrefix(r.URL.RawPath)
		}
		if _, ok := r.Header["User-Agent"]; !ok {
			r.Header.Set("User-Agent", "")
		}
	},
	ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"gpu sidecar unavailable"}`))
	},
}

// Download Station's sidecar, same-origin at /v1/download-station/* (needed
// when the UI is served over https: the sidecar itself speaks plain http on
// :28642). The sidecar validates the JWT itself and accepts the prefixed
// paths. Its built-in browser proxy (/b/...) is never served from here: on
// the UI's origin, proxied web pages could read the user's token.
const (
	dsPathPrefix  = "/v1/download-station"
	dsSidecarAddr = "127.0.0.1:28642"
)

func isDownloadStationPath(p string) bool {
	return p == dsPathPrefix || strings.HasPrefix(p, dsPathPrefix+"/")
}

func isDownloadStationBrowserPath(p string) bool {
	rest := strings.TrimPrefix(p, dsPathPrefix)
	return rest == "/b" || strings.HasPrefix(rest, "/b/")
}

var dsProxy = &httputil.ReverseProxy{
	Director: func(r *http.Request) {
		r.URL.Scheme = "http"
		r.URL.Host = dsSidecarAddr
		if _, ok := r.Header["User-Agent"]; !ok {
			r.Header.Set("User-Agent", "")
		}
	},
	ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"download station unavailable"}`))
	},
}

// Backup & Sync (nivaroos-backup, an optional module), same-origin at
// /v1/backup/*. The service listens on loopback only, accepts the
// prefixed paths, validates the JWT itself and answers /v1/backup/health
// without one (the UI's "is it installed?" check). It honours the local
// automation header, so the gateway sets or strips it here exactly as for
// its own backends; the installer's update check relies on that.
const (
	backupPathPrefix  = "/v1/backup"
	backupServiceAddr = "127.0.0.1:28643"
)

func isBackupPath(p string) bool {
	return p == backupPathPrefix || strings.HasPrefix(p, backupPathPrefix+"/")
}

var backupProxy = &httputil.ReverseProxy{
	Director: func(r *http.Request) {
		r.URL.Scheme = "http"
		r.URL.Host = backupServiceAddr
		if _, ok := r.Header["User-Agent"]; !ok {
			r.Header.Set("User-Agent", "")
		}
	},
	ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"backup service unavailable"}`))
	},
}

type GatewayRoute struct {
	management *service.Management
}

func NewGatewayRoute(management *service.Management) *GatewayRoute {
	return &GatewayRoute{
		management: management,
	}
}

// the function is to ensure the request source IP is correct.
func rewriteRequestSourceIP(r *http.Request) {
	// we may receive two kinds of requests. a request from reverse proxy. a request from client.

	// in reverse proxy, X-Forwarded-For will like
	// `X-Forwarded-For:[192.168.6.102]`(normal)
	// `X-Forwarded-For:[::1, 192.168.6.102]`(hacked) Note: the ::1 is inject by attacker.
	// `X-Forwarded-For:[::1]`(normal or hacked) local request. But it from browser have JWT. So we can and need to verify it
	// `X-Forwarded-For:[::1,::1]`(normal or hacked) attacker can build the request to bypass the verification.
	// But in the case. the remoteAddress should be the real ip. So we can use remoteAddress to verify it.

	ipList := []string{}

	// when r.Header.Get("X-Forwarded-For") is "". the ipList should be empty.
	// fix https://github.com/IceWhaleTech/CasaOS/issues/1247
	if r.Header.Get("X-Forwarded-For") != "" {
		ipList = strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	}

	// when r.Header.Get("X-Forwarded-For") is "". to clean the ipList.
	// fix https://github.com/IceWhaleTech/CasaOS/issues/1247
	if len(ipList) == 1 && ipList[0] == "" {
		ipList = []string{}
	}

	r.Header.Del("X-Forwarded-For")
	r.Header.Del("X-Real-IP")

	// Note: the X-Forwarded-For depend the correct config from reverse proxy.
	// otherwise the X-Forwarded-For may be empty.
	remoteIP := r.RemoteAddr[:strings.LastIndex(r.RemoteAddr, ":")]
	if len(ipList) > 0 && (remoteIP == "127.0.0.1" || remoteIP == "::1") {
		// to process the request from reverse proxy

		// in reverse proxy, X-Forwarded-For will container multiple IPs.
		// if the request is from reverse proxy, the r.RemoteAddr will be 127.0.0.1.
		// So we need get ip from X-Forwarded-For
		r.Header.Add("X-Forwarded-For", ipList[len(ipList)-1])
	}
	// to process the request from client.
	// the gateway will add the X-Forwarded-For to request header.
	// So we didn't need to add it.
}

func (g *GatewayRoute) GetRoute() *http.ServeMux {
	gatewayMux := http.NewServeMux()
	gatewayMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" || r.URL.Path == "/speedtest/ping" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write([]byte("pong from gateway service")); err != nil {
				logger.Error("Failed to `pong` in response to `ping`", zap.Any("error", err))
			}
			return
		}

		if r.URL.Path == "/speedtest/download" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.WriteHeader(http.StatusOK)
			buf := make([]byte, 128*1024)
			for i := range buf {
				buf[i] = byte(i & 0xFF)
			}
			flusher, ok := w.(http.Flusher)
			for i := 0; i < 4000; i++ {
				if _, err := w.Write(buf); err != nil {
					return
				}
				if ok && i%8 == 0 {
					flusher.Flush()
				}
			}
			return
		}

		if r.URL.Path == "/speedtest/upload" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "POST, PUT, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "*")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}
			io.Copy(io.Discard, r.Body)
			r.Body.Close()
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
			return
		}

		if isGPUPath(r.URL.Path) {
			nivaroos_middleware.MarkLocalAutomation(r)
			rewriteRequestSourceIP(r)
			gpuProxy.ServeHTTP(w, r)
			return
		}

		if isDownloadStationPath(r.URL.Path) {
			if isDownloadStationBrowserPath(r.URL.Path) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			rewriteRequestSourceIP(r)
			dsProxy.ServeHTTP(w, r)
			return
		}

		if isBackupPath(r.URL.Path) {
			nivaroos_middleware.MarkLocalAutomation(r)
			rewriteRequestSourceIP(r)
			backupProxy.ServeHTTP(w, r)
			return
		}

		proxy := g.management.GetProxy(r.URL.Path)

		if proxy == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// to fix https://github.com/IceWhaleTech/CasaOS/security/advisories/GHSA-32h8-rgcj-2g3c#event-102885
		// API V1 and V2 both read ip from request header. So the fix is effective for v1 and v2.
		// Must run before rewriteRequestSourceIP drops the client's
		// X-Forwarded-For: vouches (to loopback backends) only for
		// same-host non-browser callers such as nivaroos-cli, and strips
		// any client-supplied copy of the header.
		nivaroos_middleware.MarkLocalAutomation(r)
		rewriteRequestSourceIP(r)

		proxy.ServeHTTP(w, r)
	})

	return gatewayMux
}
