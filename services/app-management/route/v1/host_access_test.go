package v1

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
)

// H1: the legacy settings API recreated a container with whatever
// network_model/privileged it was sent - host networking or privileged
// mode (root on this server) needs the admin's explicit OK here too.
func TestUpdateSettingHostAccessNeedsConfirmation(t *testing.T) {
	for _, body := range []string{`{"network_model":"host"}`, `{"privileged":true}`, `{"cap_add":["SYS_ADMIN"]}`, `{"volumes":[{"host":"/var/run/docker.sock","container":"/var/run/docker.sock"}]}`, `{"devices":[{"host":"/dev/sda","container":"/dev/sda"}]}`} {
		r := httptest.NewRequest(http.MethodPut, "/v1/container/x", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		c := echo.New().NewContext(r, w)
		c.SetPathValues(echo.PathValues{{Name: "id", Value: "x"}})
		if err := UpdateSetting(c); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusPreconditionRequired {
			t.Errorf("%s: %d, want 428", body, w.Code)
		}
	}
}
