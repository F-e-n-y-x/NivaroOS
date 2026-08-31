// nivaroos-vm-sidecar exposes a REST/WebSocket API over libvirt for the
// NivaroOS VM Manager windowed app. It talks to the local libvirtd over
// qemu:///system.
package main

import (
	"flag"
	"log"
	"net/http"
)

func main() {
	addr := flag.String("addr", ":28641", "address to listen on")
	uri := flag.String("libvirt-uri", "qemu:///system", "libvirt connection URI")
	flag.Parse()

	// Connection to libvirt is lazy (see LibvirtStore.getConn) - the
	// server starts and serves /setup/status even if libvirtd isn't
	// installed yet.
	store := NewLibvirtStore(*uri)
	defer store.Close()

	mux := http.NewServeMux()
	RegisterVMRoutes(mux, store)
	RegisterSetupRoutes(mux, store, defaultStorageDir, defaultISODir)
	RegisterISORoutes(mux, defaultISODir)
	RegisterNetworkRoutes(mux, store, defaultBridgeRegistryPath, interfacesDotDDir)
	RegisterConsoleRoutes(mux, store)
	RegisterScreenshotRoutes(mux, store)
	RegisterHostRoutes(mux)

	log.Printf("nivaroos-vm-sidecar listening on %s (libvirt: %s)", *addr, *uri)
	log.Fatal(http.ListenAndServe(*addr, withCORS(mux)))
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		// A cross-origin POST/PUT/DELETE with a JSON body (every write in
		// this API) isn't a CORS "simple request", so the browser sends an
		// OPTIONS preflight first and needs a successful response with
		// these headers before it'll even attempt the real request -
		// without this short-circuit, OPTIONS fell through to the mux,
		// which has no handler for it and returned 405, silently blocking
		// every write (e.g. VM creation) as a "Failed to fetch" in the browser.
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
