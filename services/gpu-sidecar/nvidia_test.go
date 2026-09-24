package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseNvidiaQueryMultiGPU(t *testing.T) {
	out := "0, NVIDIA GeForce RTX 3090, 550.54.14, 12, 1024, 24576, 45, 120.50, 350.00\n" +
		"1, NVIDIA GeForce RTX 3060, 550.54.14, 3, 512, 12288, 38, [N/A], [N/A]\n"
	gpus, err := parseNvidiaQuery(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(gpus) != 2 {
		t.Fatalf("got %d gpus", len(gpus))
	}
	if gpus[0].Name != "NVIDIA GeForce RTX 3090" || gpus[0].MemoryTotalMiB != 24576 || gpus[0].PowerDrawW == nil || *gpus[0].PowerDrawW != 120.5 {
		t.Errorf("gpu0 = %+v", gpus[0])
	}
	if gpus[1].Index != 1 || gpus[1].PowerDrawW != nil || gpus[1].PowerLimitW != nil || gpus[1].TemperatureC != 38 {
		t.Errorf("gpu1 = %+v", gpus[1])
	}
	top := summarizeGPUs(gpus)
	if top.GPUCount != 2 || top.Name != gpus[0].Name || len(top.GPUs) != 2 {
		t.Errorf("summary = %+v", top)
	}
	b, _ := json.Marshal(gpus[1])
	if !strings.Contains(string(b), `"power_draw_w":null`) {
		t.Errorf("N/A power should serialize as null: %s", b)
	}
}

func TestParseNvidiaQueryErrors(t *testing.T) {
	if _, err := parseNvidiaQuery(""); err == nil {
		t.Error("empty output must be an error")
	}
	if _, err := parseNvidiaQuery("0, only, three\n"); err == nil {
		t.Error("short line must be an error")
	}
}

func TestParsePmon(t *testing.T) {
	out := `# gpu         pid   type     sm    mem    enc    dec    jpg    ofa    command
# Idx           #    C/G      %      %      %      %      %      %    name
    0       1234     C     55     20      -      -      -      -    python3
    0          -     -      -      -      -      -      -      -    -
    1       5678     G      7      1      -      -      -      -    Xorg
`
	procs := parsePmon(out)
	if len(procs) != 2 {
		t.Fatalf("got %+v", procs)
	}
	if procs[0].PID != 1234 || procs[0].Command != "python3" || procs[0].UtilizationPercent != 55 || procs[0].GPU != 0 {
		t.Errorf("proc0 = %+v", procs[0])
	}
	if procs[1].GPU != 1 || procs[1].Command != "Xorg" {
		t.Errorf("proc1 = %+v", procs[1])
	}

	// older driver: no jpg/ofa columns
	old := `# gpu        pid  type    sm   mem   enc   dec   command
# Idx          #   C/G     %     %     %     %   name
    0      4321     C    10     5     -     -   ollama
`
	procs = parsePmon(old)
	if len(procs) != 1 || procs[0].Command != "ollama" || procs[0].UtilizationPercent != 10 {
		t.Errorf("old format = %+v", procs)
	}
}

func TestRequireAuth(t *testing.T) {
	h := requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }), t.TempDir())
	cases := []struct {
		name    string
		remote  string
		headers map[string]string
		want    int
	}{
		{"direct local script", "127.0.0.1:1111", nil, 204},
		{"proxied via gateway, no token", "127.0.0.1:1111", map[string]string{"X-Forwarded-For": "192.168.1.5"}, 401},
		{"browser on host", "127.0.0.1:1111", map[string]string{"Sec-Fetch-Site": "cross-site"}, 401},
		{"lan", "192.168.1.5:1111", nil, 401},
		{"lan bogus token", "192.168.1.5:1111", map[string]string{"Authorization": "abc"}, 401},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodPost, "/driver-install", nil)
		r.RemoteAddr = c.remote
		for k, v := range c.headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("%s: got %d want %d", c.name, w.Code, c.want)
		}
	}
}

func TestDriverInstallRejectsBadVendor(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/driver-install", strings.NewReader(`{"vendor":"nvidia; rm -rf /"}`))
	w := httptest.NewRecorder()
	handleDriverInstall(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("no CORS header expected")
	}
}
