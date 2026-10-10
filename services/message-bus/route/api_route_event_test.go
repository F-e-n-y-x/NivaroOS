package route

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/model"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/repository"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/service"
	jsoniter "github.com/json-iterator/go"
	"github.com/labstack/echo/v5"
	"go.uber.org/goleak"
	"gotest.tools/assert"
)

var json2 = jsoniter.ConfigCompatibleWithStandardLibrary

func TestEventRoute(t *testing.T) {
	defer goleak.VerifyNone(t)

	sourceID := "Foo"
	name := "Bar"

	expectedEventTypes := []model.EventType{{
		SourceID:         sourceID,
		Name:             name,
		PropertyTypeList: []model.PropertyType{{Name: "Property1"}, {Name: "Property2"}},
	}}

	eventTypesJSON, err := json2.Marshal(expectedEventTypes)
	assert.NilError(t, err)

	repository, err := repository.NewDatabaseRepositoryInMemory()
	assert.NilError(t, err)
	defer repository.Close()

	services := service.NewServices(&repository)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	services.Start(&ctx)

	apiRoute := NewAPIRoute(&services)

	e := echo.New()

	// register event type
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/", bytes.NewReader(eventTypesJSON))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)

	err = apiRoute.RegisterEventTypes(e.NewContext(req, rec))
	assert.NilError(t, err)
	assert.Equal(t, rec.Code, http.StatusOK)

	// get event types
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)

	err = apiRoute.GetEventTypes(e.NewContext(req, rec))
	assert.NilError(t, err)
	assert.Equal(t, rec.Code, http.StatusOK)

	var actualEventTypes []model.EventType
	err = json2.UnmarshalFromString(rec.Body.String(), &actualEventTypes)
	assert.NilError(t, err)
	assert.DeepEqual(t, actualEventTypes, expectedEventTypes)

	// get event type
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)

	err = apiRoute.GetEventType(e.NewContext(req, rec), sourceID, name)
	assert.NilError(t, err)
	assert.Equal(t, rec.Code, http.StatusOK)

	var actualEventType model.EventType
	err = json2.UnmarshalFromString(rec.Body.String(), &actualEventType)
	assert.NilError(t, err)
	assert.DeepEqual(t, actualEventType, expectedEventTypes[0])
}

// Bad type names and oversized or malformed bodies are clear 400s, and a
// batch with one bad entry registers none of it.
func TestEventRouteValidation(t *testing.T) {
	repository, err := repository.NewDatabaseRepositoryInMemory()
	assert.NilError(t, err)
	defer repository.Close()
	services := service.NewServices(&repository)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	services.Start(&ctx)
	apiRoute := NewAPIRoute(&services)
	e := echo.New()
	call := func(body string, h func(*echo.Context) error) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(body)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		assert.NilError(t, h(e.NewContext(req, rec)))
		return rec
	}
	register := func(body string) int { return call(body, apiRoute.RegisterEventTypes).Code }

	for _, bad := range []string{
		`[{"sourceID":"ok","name":"good:one"},{"sourceID":"a/b","name":"x"}]`,
		`[{"sourceID":"ok","name":"has space"}]`,
		`[{"sourceID":"","name":"x"}]`,
		`[{"sourceID":"ok","name":"x","propertyTypeList":[{"name":"p?q"}]}]`,
		`[{"sourceID":"ok","name":"` + strings.Repeat("n", 129) + `"}]`,
	} {
		if c := register(bad); c != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, c)
		}
	}
	if types, _ := services.EventTypeService.GetEventTypes(); len(types) != 0 {
		t.Fatalf("half a bad batch registered: %+v", types)
	}
	assert.Equal(t, register(`[{"sourceID":"nivaroos-download","name":"nivaroos-download:download:notify","propertyTypeList":[{"name":"title"},{"name":"app:name"}]}]`), http.StatusOK)

	publish := func(body string) *httptest.ResponseRecorder {
		return call(body, func(c *echo.Context) error {
			return apiRoute.PublishEvent(c, "nivaroos-download", "nivaroos-download:download:notify")
		})
	}
	assert.Equal(t, publish(`{"title":"hi","extra":"not in the type is fine"}`).Code, http.StatusOK)
	for _, bad := range []string{`{"title":` + `"` + strings.Repeat("x", maxPropertiesBytes) + `"}`, `{"title":1}`, `[]`, `{"":"x"}`} {
		if rec := publish(bad); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "propert") {
			t.Errorf("%.40s: %d %s", bad, rec.Code, rec.Body)
		}
	}
}
