// nivaroos-vm-sidecar exposes a REST/WebSocket API over libvirt for the
// NivaroOS VM Manager windowed app. It talks to the local libvirtd over
// qemu:///system.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"
)

func main() {
	addr := flag.String("addr", ":28641", "address to listen on")
	uri := flag.String("libvirt-uri", "qemu:///system", "libvirt connection URI")
	runtimePath := flag.String("runtime-path", "/var/run/nivaroos", "NivaroOS runtime directory (for locating user-service's JWKS endpoint)")
	flag.Parse()

	// Connection to libvirt is lazy (see LibvirtStore.getConn) - the
	// server starts and serves /setup/status even if libvirtd isn't
	// installed yet.
	store := NewLibvirtStore(*uri)
	defer store.Close()

	EnsureAutoShareDir()
	// VMs from older versions move onto the current /DATA/VMs layout
	// (stopped ones now, others on their next start). In the background:
	// libvirtd may not even be installed yet.
	go store.MigrateLegacyLayout()

	mux := http.NewServeMux()
	RegisterVMRoutes(mux, store)
	RegisterSetupRoutes(mux, store, defaultStorageDir, defaultISODir)
	RegisterISORoutes(mux, store, defaultISODir)
	RegisterGuestToolsRoutes(mux, store)
	RegisterNetworkRoutes(mux, store, defaultBridgeRegistryPath, interfacesDotDDir)
	RegisterConsoleRoutes(mux, store)
	RegisterScreenshotRoutes(mux, store)
	RegisterHostRoutes(mux)
	RegisterHostDesktopInstallRoutes(mux)
	StartHostCapsLockWatcher()

	srv := &http.Server{
		Addr:    *addr,
		Handler: withCORS(requireAuth(mux, *runtimePath)),
		// No overall Read/WriteTimeout: ISO uploads and the console
		// WebSocket are legitimately long-lived. ReadHeaderTimeout alone
		// stops a client from holding a connection open by trickling
		// headers forever.
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("nivaroos-vm-sidecar listening on %s (libvirt: %s)", *addr, *uri)
	log.Fatal(srv.ListenAndServe())
}

// withCORS allows cross-origin calls only from a page served by this same
// host (the NivaroOS web UI, on its own port) - it echoes that exact
// Origin back rather than "*", so any other site a user happens to have
// open can't read responses from (or, with a stolen token, drive) this API.
// Non-browser clients (the mobile app, curl) send no Origin and don't need
// CORS at all.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		allowed := sameHostOrigin(r)
		if allowed {
			w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
		}
		// A cross-origin POST/PUT/DELETE with a JSON body (every write in
		// this API) isn't a CORS "simple request", so the browser sends an
		// OPTIONS preflight first and needs a successful response with
		// these headers before it'll even attempt the real request -
		// without this short-circuit, OPTIONS fell through to the mux,
		// which has no handler for it and returned 405, silently blocking
		// every write (e.g. VM creation) as a "Failed to fetch" in the browser.
		if r.Method == http.MethodOptions {
			if allowed {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
