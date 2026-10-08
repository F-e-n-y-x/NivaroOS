package oapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/labstack/echo/v5"
)

const spec = `openapi: 3.0.0
info: {title: t, version: "1"}
paths:
  /items:
    get:
      parameters:
        - {name: limit, in: query, required: true, schema: {type: integer}}
      responses: {"200": {description: ok}}
`

func TestRequestValidator(t *testing.T) {
	swagger, err := openapi3.NewLoader().LoadFromData([]byte(spec))
	if err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	e.Use(RequestValidator(swagger, Options{
		Options: openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
		Skipper: func(c *echo.Context) bool { return c.Request().Header.Get("X-Skip") != "" },
	}))
	e.GET("/items", func(c *echo.Context) error { return c.NoContent(http.StatusOK) })
	e.GET("/other", func(c *echo.Context) error { return c.NoContent(http.StatusOK) })

	for _, tc := range []struct {
		path, skip string
		code       int
		body       string
	}{
		{"/items?limit=3", "", 200, ""},
		{"/items?limit=x", "", 400, `{"message":"parameter \"limit\" in query has an error: value x: an invalid integer: invalid syntax"}`},
		{"/other", "", 400, `{"message":"no matching operation was found"}`},
		{"/other", "1", 200, ""},
	} {
		r := httptest.NewRequest(http.MethodGet, tc.path, nil)
		if tc.skip != "" {
			r.Header.Set("X-Skip", tc.skip)
		}
		w := httptest.NewRecorder()
		e.ServeHTTP(w, r)
		if w.Code != tc.code || strings.TrimSpace(w.Body.String()) != tc.body {
			t.Errorf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
	}
}
