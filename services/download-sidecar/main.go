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
	flag.Parse()

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
		Handler:           withCORS(route(browser, requireAuth(mux, *runtimePath))),
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

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, browserPrefix) {
			// Proxied content is same-origin with itself; it must not get
			// the API's blanket CORS grant.
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		// Same preflight short-circuit as vm-sidecar: every JSON write is a
		// non-simple CORS request.
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
