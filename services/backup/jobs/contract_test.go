package jobs

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
	"github.com/F-e-n-y-x/NivaroOS/services/backup/internal/fixturetest"
)

// repoFile returns path (relative to the repository root) or skips the
// test when this checkout doesn't include it (a service-only tarball).
func repoFile(t *testing.T, rel string) string {
	t.Helper()
	p := filepath.Join("..", "..", "..", filepath.FromSlash(rel))
	if _, err := os.Stat(filepath.Dir(p)); err != nil {
		t.Skipf("%s not in this checkout", filepath.Dir(rel))
	}
	return p
}

// Empty is the {} request/response.
type Empty struct{}

// apiTypes maps backup-api.json request_type/response_type names to Go
// types. "[]X" is a list of X.
var apiTypes = map[string]func() interface{}{
	"Empty":                 func() interface{} { return new(Empty) },
	"ServiceHealth":         func() interface{} { return new(ServiceHealth) },
	"Capabilities":          func() interface{} { return new(Capabilities) },
	"Location":              func() interface{} { return new(Location) },
	"BrowseResult":          func() interface{} { return new(BrowseResult) },
	"ResolvePathRequest":    func() interface{} { return new(ResolvePathRequest) },
	"ResolvePathResult":     func() interface{} { return new(ResolvePathResult) },
	"Job":                   func() interface{} { return new(Job) },
	"JobListItem":           func() interface{} { return new(JobListItem) },
	"JobDetail":             func() interface{} { return new(JobDetail) },
	"DeleteJobResult":       func() interface{} { return new(DeleteJobResult) },
	"ToggleRequest":         func() interface{} { return new(ToggleRequest) },
	"ReconnectRequest":      func() interface{} { return new(ReconnectRequest) },
	"RunRequest":            func() interface{} { return new(RunRequest) },
	"RunStarted":            func() interface{} { return new(RunStarted) },
	"ValidateResult":        func() interface{} { return new(ValidateResult) },
	"CronPreviewRequest":    func() interface{} { return new(CronPreviewRequest) },
	"CronPreview":           func() interface{} { return new(CronPreview) },
	"RunList":               func() interface{} { return new(RunList) },
	"RunDetail":             func() interface{} { return new(RunDetail) },
	"LogPage":               func() interface{} { return new(LogPage) },
	"DecideRequest":         func() interface{} { return new(DecideRequest) },
	"PreviewPage":           func() interface{} { return new(PreviewPage) },
	"Version":               func() interface{} { return new(Version) },
	"RestoreRequest":        func() interface{} { return new(RestoreRequest) },
	"DownloadRequest":       func() interface{} { return new(DownloadRequest) },
	"DownloadToken":         func() interface{} { return new(DownloadToken) },
	"AppSettings":           func() interface{} { return new(AppSettings) },
	"MigrationReport":       func() interface{} { return new(MigrationReport) },
	"RememberedDrive":       func() interface{} { return new(RememberedDrive) },
	"RememberedDriveUpdate": func() interface{} { return new(RememberedDriveUpdate) },
	"BusyResult":            func() interface{} { return new(BusyResult) },
	"Device":                func() interface{} { return new(Device) },
	"DeviceEnrollRequest":   func() interface{} { return new(DeviceEnrollRequest) },
	"DeviceEnrollment":      func() interface{} { return new(DeviceEnrollment) },
	"DevicePing":            func() interface{} { return new(DevicePing) },
}

func newAPIValue(t *testing.T, name string) interface{} {
	t.Helper()
	if elem, ok := strings.CutPrefix(name, "[]"); ok {
		mk, ok := apiTypes[elem]
		if !ok {
			t.Fatalf("unknown type %q", name)
		}
		return reflect.New(reflect.SliceOf(reflect.TypeOf(mk()).Elem())).Interface()
	}
	mk, ok := apiTypes[name]
	if !ok {
		t.Fatalf("unknown type %q", name)
	}
	return mk()
}

type apiDoc struct {
	Base     string `json:"base"`
	Envelope struct {
		Examples map[string]struct {
			Success int             `json:"success"`
			Message string          `json:"message"`
			Data    json.RawMessage `json:"data"`
		} `json:"examples"`
	} `json:"envelope"`
	Events struct {
		SourceID string              `json:"source_id"`
		Names    map[string][]string `json:"names"`
	} `json:"events"`
	Endpoints []struct {
		ID           string          `json:"id"`
		Method       string          `json:"method"`
		Path         string          `json:"path"`
		Auth         string          `json:"auth"`
		Envelope     bool            `json:"envelope"`
		Status       int             `json:"status"`
		RequestType  *string         `json:"request_type"`
		Request      json.RawMessage `json:"request"`
		ResponseType string          `json:"response_type"`
		Response     json.RawMessage `json:"response"`
	} `json:"endpoints"`
}

func loadAPIDoc(t *testing.T) apiDoc {
	t.Helper()
	raw, err := os.ReadFile(repoFile(t, "docs/specs/backup-api.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc apiDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestAPIFixtures(t *testing.T) {
	doc := loadAPIDoc(t)
	if doc.Base != APIBase {
		t.Errorf("base %q, want %q", doc.Base, APIBase)
	}
	ids, routes := map[string]bool{}, map[string]bool{}
	for _, ep := range doc.Endpoints {
		ep := ep
		route := ep.Method + " " + ep.Path
		if ids[ep.ID] || routes[route] {
			t.Errorf("duplicate endpoint %s (%s)", ep.ID, route)
		}
		ids[ep.ID], routes[route] = true, true
		switch ep.Auth {
		case "none", "jwt", "token", "device":
		default:
			t.Errorf("%s: auth %q", ep.ID, ep.Auth)
		}
		t.Run(ep.ID, func(t *testing.T) {
			if ep.RequestType != nil {
				if err := fixturetest.CheckJSON(ep.Request, newAPIValue(t, *ep.RequestType)); err != nil {
					t.Errorf("request: %v", err)
				}
			} else if string(ep.Request) != "null" {
				t.Errorf("request example without request_type")
			}
			if ep.ResponseType == "binary" {
				return
			}
			if err := fixturetest.CheckJSON(ep.Response, newAPIValue(t, ep.ResponseType)); err != nil {
				t.Errorf("response: %v", err)
			}
		})
	}
	for _, must := range []string{"GET /health", "GET /capabilities", "GET /jobs", "POST /jobs", "PUT /jobs/:id", "GET /runs", "POST /runs/:id/decide", "POST /jobs/:id/restore", "GET /devices", "POST /devices", "DELETE /devices/:id", "GET /devices/:id/ping"} {
		if !routes[must] {
			t.Errorf("missing route %s", must)
		}
	}
	for name, ex := range doc.Envelope.Examples {
		if ex.Success >= 400 {
			if err := fixturetest.CheckJSON(ex.Data, new(ErrorBody)); err != nil {
				t.Errorf("envelope example %s: %v", name, err)
			}
			var body ErrorBody
			_ = json.Unmarshal(ex.Data, &body)
			if ex.Message != string(body.ErrorCode) {
				t.Errorf("envelope example %s: message %q != error_code %q", name, ex.Message, body.ErrorCode)
			}
			if _, ok := Info(body.ErrorCode); !ok {
				t.Errorf("envelope example %s: unknown code %q", name, body.ErrorCode)
			}
		}
	}
}

func TestAPIEventsMatchGo(t *testing.T) {
	doc := loadAPIDoc(t)
	if doc.Events.SourceID != EventSourceID {
		t.Errorf("source_id %q", doc.Events.SourceID)
	}
	got := map[string][]string{}
	for _, et := range EventTypes() {
		for _, p := range et.PropertyTypeList {
			got[et.Name] = append(got[et.Name], p.Name)
		}
	}
	if !reflect.DeepEqual(got, doc.Events.Names) {
		t.Errorf("backup-api.json events differ from EventTypes():\n doc %v\n go  %v", doc.Events.Names, got)
	}
}

func TestEveryEngineCodeHasAClass(t *testing.T) {
	for _, c := range engine.EngineCodes {
		if _, ok := Info(c); !ok {
			t.Errorf("engine code %q has no class in codeTable", c)
		}
	}
	for _, c := range AllErrorCodes() {
		info, _ := Info(c)
		if info.Class == "" {
			t.Errorf("%q has no class", c)
		}
		if info.Class != ClassAPI && len(info.Actions) == 0 {
			t.Errorf("run error %q offers no action", c)
		}
		if !regexp.MustCompile(`^[a-z][a-z0-9_]*$`).MatchString(string(c)) {
			t.Errorf("code %q is not snake_case", c)
		}
	}
}

func TestRetryable(t *testing.T) {
	cases := []struct {
		code  ErrorCode
		unmet WhenUnmet
		want  bool
	}{
		{ErrNetworkUnreachable, UnmetSkip, true},
		{ErrDestOffline, UnmetWait, true},
		{ErrDestOffline, UnmetSkip, false},
		{ErrDestOffline, UnmetFail, false},
		{ErrCloudQuotaDaily, UnmetWait, false},
		{ErrDeleteGuard, UnmetWait, false},
		{ErrCloudAuth, UnmetWait, false},
		{ErrInterrupted, UnmetWait, false},
		{"no_such_code", UnmetWait, false},
	}
	for _, c := range cases {
		if got := Retryable(c.code, c.unmet); got != c.want {
			t.Errorf("Retryable(%s, %s) = %v", c.code, c.unmet, got)
		}
	}
	if ClassOf("no_such_code") != ClassLifecycle {
		t.Error("unknown codes must be treated as internal")
	}
}

func TestUIContractInSync(t *testing.T) {
	paths := make([]string, 0, len(UIContractFiles))
	for p := range UIContractFiles {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		p := repoFile(t, rel)
		have, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("%s: %v (run `go generate ./jobs`)", rel, err)
			continue
		}
		if !bytes.Equal(have, UIContractFiles[rel]()) {
			t.Errorf("%s is out of date: run `go generate ./jobs` in services/backup", rel)
		}
	}
}

var placeholderRE = regexp.MustCompile(`\{([a-zA-Z0-9_]+)\}`)

func TestI18nKeysInEnUS(t *testing.T) {
	raw, err := os.ReadFile(repoFile(t, "ui/src/assets/lang/en_US.json"))
	if err != nil {
		t.Fatal(err)
	}
	var en map[string]interface{}
	if err := json.Unmarshal(raw, &en); err != nil {
		t.Fatal(err)
	}
	if _, nested := en["backup"]; nested {
		t.Fatal(`en_US.json has a nested "backup" object; backup keys are flat ("backup.x.y")`)
	}
	for _, key := range SortedI18nKeys() {
		v, ok := en[key]
		if !ok {
			t.Errorf("en_US.json lacks %q", key)
			continue
		}
		text, ok := v.(string)
		if !ok || strings.TrimSpace(text) == "" {
			t.Errorf("en_US.json %q is empty or not a string", key)
			continue
		}
		allowed := map[string]bool{}
		for _, a := range AllI18nKeys()[key] {
			allowed[a] = true
		}
		for _, m := range placeholderRE.FindAllStringSubmatch(text, -1) {
			if !allowed[m[1]] {
				t.Errorf("en_US.json %q uses {%s}, which the backend never sends (args: %v)", key, m[1], AllI18nKeys()[key])
			}
		}
	}
}

func TestLogKeyCodes(t *testing.T) {
	for key := range LogKeys {
		if !strings.HasPrefix(key, "backup.log.") {
			t.Errorf("log key %q", key)
		}
	}
}

func TestJobRowRoundTrip(t *testing.T) {
	from := "task-1"
	now := time.Date(2026, 9, 24, 3, 0, 0, 0, time.UTC)
	j := Job{
		ID: "bk_1a2b3c4d5e6f", Name: "Photos", Type: TypeMirror, Enabled: true, Revision: 2,
		Sources:      []Endpoint{{Kind: EPVolume, RefID: "uuid", SubPath: "photos"}},
		Dest:         Endpoint{Kind: EPUSB, RefID: "3A4F-1C22", Match: &DevMatch{Serial: "s", SizeBytes: 10}},
		Triggers:     []Trigger{{Kind: TriggerSchedule, Cron: "0 3 * * 0", CatchUp: true}},
		Conditions:   Conditions{DestAvailable: true, WhenUnmet: UnmetSkip, Window: &TimeWindow{Start: "22:00", End: "07:00"}},
		Filters:      Filters{ExcludePresets: []string{"caches"}, Exclude: []string{"*.tmp"}},
		Guards:       Guards{EmptySourcePct: 50, DeletePct: 10, ChangePct: 30},
		Retention:    Retention{VersionsDays: 30},
		Hooks:        []Hook{{Phase: HookPre, Action: HookStopApps, Apps: []string{"immich"}, TimeoutSec: 300, FailPolicy: FailAbort}},
		Retry:        Retry{Max: 3, BackoffSec: DefaultRetryBackoffSec},
		Notify:       NotifyPrefs{OnFailure: true, StaleAfterHours: 48},
		DestFolderID: "dfid", NeedsAttention: AttentionImported, MigratedFrom: &from, CreatedAt: now, UpdatedAt: now,
	}
	row, err := JobToRow(j)
	if err != nil {
		t.Fatal(err)
	}
	back, err := row.ToJob()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(j, back) {
		t.Fatalf("round trip changed the job:\n %#v\n %#v", j, back)
	}

	// A row with empty JSON columns (older schema) still loads, with []
	// rather than null lists.
	old, err := JobRow{ID: "bk_x", Type: "copy"}.ToJob()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(old)
	for _, f := range []string{`"sources":[]`, `"triggers":[]`, `"hooks":[]`} {
		if !strings.Contains(string(raw), f) {
			t.Errorf("empty row encodes without %s: %s", f, raw)
		}
	}
	if _, err := (JobRow{ID: "bk_bad", Sources: "{"}).ToJob(); err == nil {
		t.Error("corrupt column must fail")
	}
}

func TestMessageEncoding(t *testing.T) {
	m := Message{Key: "backup.run.summary.ok", Args: map[string]interface{}{"added": float64(3), "bytes": float64(10)}}
	s := EncodeMessage(m)
	if !strings.HasPrefix(s, "backup.run.summary.ok|{") {
		t.Fatalf("encoded %q", s)
	}
	if got := DecodeMessage(s); !reflect.DeepEqual(*got, m) {
		t.Fatalf("decoded %#v", got)
	}
	if got := DecodeMessage("backup.run.summary.interrupted"); got.Key != "backup.run.summary.interrupted" || got.Args != nil {
		t.Fatalf("bare key decoded %#v", got)
	}
	if DecodeMessage("") != nil {
		t.Fatal("empty summary must decode to nil")
	}
	if got := DecodeMessage("k|not json"); got.Key != "k" || got.Args != nil {
		t.Fatalf("bad args decoded %#v", got)
	}
}

func TestRunStatusFinal(t *testing.T) {
	for _, s := range RunStatuses {
		resting := s == StatusQueued || s == StatusRunning || s == StatusWaitingUser
		if s.Final() == resting {
			t.Errorf("%s.Final() = %v", s, s.Final())
		}
	}
}

func TestCronParser(t *testing.T) {
	for _, ok := range []string{"0 3 * * 0", "*/15 * * * *", "0 22 * * 1-5", "@daily", "@every 6h"} {
		if _, err := CronParser.Parse(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "0 3 * *", "0 0 3 * * *", "@reboot", "61 * * * *"} {
		if _, err := CronParser.Parse(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestDefaultAppSettings(t *testing.T) {
	s := DefaultAppSettings()
	if s.MaxConcurrent != 2 || !s.CatchUpDefault || s.DefaultDeletePct != 10 || s.DefaultChangePct != 30 || s.DefaultVersionsDays != 30 || s.LogRetentionDays != 180 {
		t.Fatalf("defaults %+v", s)
	}
}
