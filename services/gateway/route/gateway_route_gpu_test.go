package route

import "testing"

func TestGPUPathRouting(t *testing.T) {
	for p, want := range map[string]bool{
		"/v1/gpu":               true,
		"/v1/gpu/gpu-stats":     true,
		"/v1/gpu/driver-status": true,
		"/v1/gpux":              false,
		"/v1/gp":                false,
		"/v1/sys/gpu":           false,
	} {
		if got := isGPUPath(p); got != want {
			t.Errorf("isGPUPath(%q)=%v want %v", p, got, want)
		}
	}
	for in, want := range map[string]string{
		"/v1/gpu":                "/",
		"/v1/gpu/":               "/",
		"/v1/gpu/gpu-stats":      "/gpu-stats",
		"/v1/gpu/driver-install": "/driver-install",
	} {
		if got := stripGPUPrefix(in); got != want {
			t.Errorf("stripGPUPrefix(%q)=%q want %q", in, got, want)
		}
	}
}

func TestDownloadStationRouting(t *testing.T) {
	for p, want := range map[string]bool{
		"/v1/download-station":           true,
		"/v1/download-station/downloads": true,
		"/v1/download-stationx":          false,
		"/v1/download":                   false,
	} {
		if got := isDownloadStationPath(p); got != want {
			t.Errorf("isDownloadStationPath(%q)=%v want %v", p, got, want)
		}
	}
	// The lite browser proxy must never be reachable on the UI's origin.
	for p, want := range map[string]bool{
		"/v1/download-station/b":                true,
		"/v1/download-station/b/x/page":         true,
		"/v1/download-station/browser/sessions": false,
		"/v1/download-station/bookmarks":        false,
	} {
		if got := isDownloadStationBrowserPath(p); got != want {
			t.Errorf("isDownloadStationBrowserPath(%q)=%v want %v", p, got, want)
		}
	}
}
