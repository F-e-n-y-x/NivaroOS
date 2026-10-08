// Copyright 2019 DeepMap, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package oapi is the OpenAPI request validator from
// github.com/deepmap/oapi-codegen v1.12.4 pkg/middleware, ported to echo v5
// (neither deepmap nor oapi-codegen/echo-middleware ship an echo v5 one).
// Same router (gorillamux), same status codes and messages; the unused
// ErrorHandler/MultiErrorHandler/UserData hooks were dropped.
package oapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/labstack/echo/v5"
)

type Options struct {
	Options openapi3filter.Options
	Skipper func(c *echo.Context) bool
}

// RequestValidator rejects requests that don't match swagger: 400 with the
// first line of the validation error, 403 for security requirement errors.
func RequestValidator(swagger *openapi3.T, options Options) echo.MiddlewareFunc {
	router, err := gorillamux.NewRouter(swagger)
	if err != nil {
		panic(err)
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if options.Skipper != nil && options.Skipper(c) {
				return next(c)
			}
			if err := validate(c.Request(), router, &options.Options); err != nil {
				return err
			}
			return next(c)
		}
	}
}

func validate(req *http.Request, router routers.Router, opts *openapi3filter.Options) error {
	route, pathParams, err := router.FindRoute(req)
	if err != nil {
		var re *routers.RouteError
		if errors.As(err, &re) {
			// the path requested doesn't match either server, or path, or something
			return echo.NewHTTPError(http.StatusBadRequest, re.Reason)
		}
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("error validating route: %s", err.Error()))
	}

	err = openapi3filter.ValidateRequest(context.Background(), &openapi3filter.RequestValidationInput{
		Request:    req,
		PathParams: pathParams,
		Route:      route,
		Options:    opts,
	})
	if err == nil {
		return nil
	}
	me := openapi3.MultiError{}
	if errors.As(err, &me) {
		return echo.NewHTTPError(http.StatusBadRequest, me.Error()).Wrap(me)
	}
	switch e := err.(type) {
	case *openapi3filter.RequestError:
		// openapi errors are multi-line with a decent message on the first
		return echo.NewHTTPError(http.StatusBadRequest, strings.Split(e.Error(), "\n")[0]).Wrap(err)
	case *openapi3filter.SecurityRequirementsError:
		for _, err := range e.Errors {
			var he *echo.HTTPError
			if errors.As(err, &he) {
				return he
			}
		}
		return echo.NewHTTPError(http.StatusForbidden, e.Error()).Wrap(err)
	default:
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("error validating request: %s", err)).Wrap(err)
	}
}
