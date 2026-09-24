// nivaroos-download-sidecar backs the Download Station windowed app: an
// IDM-style multi-connection download manager, plus the rewriting proxy
// behind its built-in lite browser and that browser's ad blocker (uBlock
// Origin filter lists and syntax).
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const version = "1.0.0"

// serverCtx is cancelled on SIGTERM - long-lived background work (list
// updates kicked off by a request) hangs off it rather than the request's
// own context, which ends as soon as the response is written.
var serverCtx context.Context

func main() {
	addr := flag.String("addr", ":28642", "address to listen on")
	dataDir := flag.String("data-dir", "/var/lib/nivaroos/download-station", "where download state, settings and filter lists are kept")
	defaultDir := flag.String("download-dir", "/DATA/Downloads", "default folder new downloads are saved to")
	runtimePath := flag.String("runtime-path", "/var/run/nivaroos", "NivaroOS runtime directory (for locating user-service's JWKS endpoint)")
	storageRoots := flag.String("storage-roots", "/DATA,/media,/mnt", "comma-separated folders downloads may be saved under (mounted data drives are added automatically)")
	flag.Parse()
	pathPolicy.SetBase(strings.Split(*storageRoots, ","))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	serverCtx = ctx

	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		log.Fatalf("data dir: %v", err)
	}
	transport := newTransport(false)
	settings := NewSettingsStore(*dataDir, *defaultDir)
	manager := NewManager(*dataDir, settings, transport)
	adblock := NewAdblocker(*dataDir, settings, transport)
	browser := NewBrowser(transport, adblock, settings)
	history := NewHistory(*dataDir)

	mux := http.NewServeMux()
	RegisterRoutes(mux, manager, settings, adblock, browser, history)

	manager.Start(ctx)
	adblock.Start(ctx)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           withCORS(stripGatewayPrefix(route(browser, requireAuth(mux, *runtimePath)))),
		ReadHeaderTimeout: 20 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	log.Printf("nivaroos-download-sidecar %s listening on %s", version, *addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	// Let the ticker's final save run.
	manager.Wait()
	history.flush()
}

// route sends lite-browser traffic (authorised by its session capability,
// not a JWT - see browser.go) to the proxy, and everything else through
// normal JWT auth to the API.
func route(br *Browser, api http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, browserPrefix) {
			br.ServeProxy(w, r)
			return
		}
		// Unauthenticated liveness/install probe for the desktop (Dock, app
		// list) - says only that Download Station is installed and running.
		if r.URL.Path == "/health" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			writeJSON(w, 200, map[string]interface{}{"installed": true, "running": true, "service": "download-station", "version": version})
			return
		}
		// A request with no JWT whose Referer is a proxied page is a
		// root-relative URL the page built itself - bounce it into the
		// proxy. Checked only when unauthenticated, so the real UI (which
		// always sends its token) can never be redirected this way.
		if r.Header.Get("Authorization") == "" && r.URL.Query().Get("token") == "" && br.TryRefererRedirect(w, r) {
			return
		}
		api.ServeHTTP(w, r)
	})
}

// gatewayPrefix is where the NivaroOS gateway mounts this API for pages
// served over HTTPS (the sidecar itself only speaks plain HTTP, which an
// https:// page may not call). Accepted with or without the gateway
// stripping it.
const gatewayPrefix = "/v1/download-station"

// stripGatewayPrefix maps /v1/download-station/x onto /x. The lite
// browser's proxy (/b/...) is refused on that path: served through the
// gateway it would run arbitrary web pages on the NivaroOS UI's own origin,
// where they could read the user's access token from localStorage. It must
// only ever be reached on this service's own port (its own origin).
func stripGatewayPrefix(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == gatewayPrefix || strings.HasPrefix(r.URL.Path, gatewayPrefix+"/") {
			rest := strings.TrimPrefix(r.URL.Path, gatewayPrefix)
			if rest == "" {
				rest = "/"
			}
			if strings.HasPrefix(rest, browserPrefix) {
				http.Error(w, "the lite browser is only served on Download Station's own port", http.StatusForbidden)
				return
			}
			r2 := r.Clone(context.WithValue(r.Context(), viaGatewayKey{}, true))
			r2.URL.Path = rest
			r2.URL.RawPath = ""
			next.ServeHTTP(w, r2)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withCORS allows cross-origin calls only from a page served by this same
// host (the NivaroOS web UI, on its own port) - it echoes that exact Origin
// back rather than "*", so any other site a user happens to have open can't
// read responses from (or, with a stolen token, drive) this API. Same as
// vm-sidecar's.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, browserPrefix) {
			// Proxied content is same-origin with itself; it must not get
			// the API's CORS grant.
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Add("Vary", "Origin")
		allowed := sameHostOrigin(r)
		if allowed {
			w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
		}
		// Every JSON write is a non-simple CORS request, so the browser
		// preflights it; answer here rather than 405 from the mux.
		if r.Method == http.MethodOptions {
			if allowed {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Max-Age", "600")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
