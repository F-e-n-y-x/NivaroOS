package v2

import (
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/app-management/codegen"
)

func ptr[T any](v T) *T { return &v }

// docker stop leaves 137/143/0 - stopped on purpose; an error code, a
// restart loop or a dead container is a failure.
func TestMarkFailed(t *testing.T) {
	items := []codegen.WebAppGridItem{
		{Name: ptr("comfyui"), Status: ptr("exited")},      // compose app stopped by the owner
		{Name: ptr("broken"), Status: ptr("exited")},       // compose app that crashed
		{Name: ptr("loop"), Status: ptr("restarting")},     // plain container in a restart loop
		{Name: ptr("qbittorrent"), Status: ptr("running")}, // running: untouched
	}
	markFailed(items, []containerSummary{
		{name: "comfyui", project: "comfyui", state: "exited", status: "Exited (137) 2 weeks ago"},
		{name: "broken-db", project: "broken", state: "exited", status: "Exited (0) 1 hour ago"},
		{name: "broken-app", project: "broken", state: "exited", status: "Exited (1) 1 hour ago"},
		{name: "loop", state: "restarting", status: "Restarting (1) 5 seconds ago"},
		{name: "qbittorrent", project: "qbittorrent", state: "running", status: "Up 3 days"},
	})
	if f := items[0].Failed; f == nil || *f || items[0].ExitCode == nil || *items[0].ExitCode != 137 {
		t.Fatalf("stopped app: failed=%v code=%v", f, items[0].ExitCode)
	}
	if f := items[1].Failed; f == nil || !*f {
		t.Fatal("a compose app with one container exited 1 must be failed")
	}
	if f := items[2].Failed; f == nil || !*f {
		t.Fatal("restarting must be failed")
	}
	if items[3].Failed != nil {
		t.Fatal("a running app gets no failed field")
	}
}
