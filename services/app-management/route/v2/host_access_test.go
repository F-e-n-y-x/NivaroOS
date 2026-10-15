package v2

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/app-management/service"
	"github.com/labstack/echo/v5"
	"gotest.tools/v3/assert"
)

func composeOf(t *testing.T, yaml string) *service.ComposeApp {
	t.Helper()
	app, err := service.NewComposeAppFromYAML([]byte(yaml), true, true)
	assert.NilError(t, err)
	return app
}

// H1: an app with network_mode: host is on the host's loopback (which
// every NivaroOS API trusts) and a privileged one is root outright - so
// installing one needs the admin's explicit OK (?allow_host_access=true).
func TestHostAccessNeedsConfirmation(t *testing.T) {
	plain := composeOf(t, "name: plain\nservices:\n  web:\n    image: nginx\n")
	host := composeOf(t, "name: ha\nservices:\n  db:\n    image: postgres\n  web:\n    image: ha\n    network_mode: host\n    privileged: true\n")
	assert.DeepEqual(t, plain.HostAccess(), []string(nil))
	assert.DeepEqual(t, host.HostAccess(), []string{"web: host network", "web: privileged"})

	call := func(target string, app, before *service.ComposeApp) (bool, *httptest.ResponseRecorder) {
		w := httptest.NewRecorder()
		refused, err := refuseHostAccess(echo.New().NewContext(httptest.NewRequest(http.MethodPost, target, nil), w), app, before)
		assert.NilError(t, err)
		return refused, w
	}
	refused, w := call("/v2/app_management/compose", host, nil)
	assert.Assert(t, refused)
	assert.Equal(t, w.Code, http.StatusPreconditionRequired)
	var body struct {
		Data struct {
			HostAccess []string `json:"host_access"`
		} `json:"data"`
	}
	assert.NilError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.DeepEqual(t, body.Data.HostAccess, []string{"web: host network", "web: privileged"})

	refused, _ = call("/v2/app_management/compose?allow_host_access=true", host, nil)
	assert.Assert(t, !refused)
	refused, _ = call("/v2/app_management/compose", plain, nil)
	assert.Assert(t, !refused)
	// Saving settings of an app that already had it: no new question.
	refused, _ = call("/v2/app_management/compose/ha", host, host)
	assert.Assert(t, !refused)
	refused, _ = call("/v2/app_management/compose/plain", host, plain)
	assert.Assert(t, refused)
}
