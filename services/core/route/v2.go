package route

import (
	"crypto/ecdsa"
	"log"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/F-e-n-y-x/NivaroOS/services/core/codegen"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/config"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/file"

	"github.com/F-e-n-y-x/NivaroOS/services/common/external"
	nivaroos_middleware "github.com/F-e-n-y-x/NivaroOS/services/common/middleware"
	"github.com/F-e-n-y-x/NivaroOS/services/common/middleware/oapi"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/jwt"
	v1 "github.com/F-e-n-y-x/NivaroOS/services/core/route/v1"
	v2Route "github.com/F-e-n-y-x/NivaroOS/services/core/route/v2"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	echojwt "github.com/labstack/echo-jwt/v5"
	"github.com/labstack/echo/v5"
	echo_middleware "github.com/labstack/echo/v5/middleware"
)

var (
	_swagger *openapi3.T

	V2APIPath string
	V2DocPath string
)

func init() {
	swagger, err := codegen.GetSwagger()
	if err != nil {
		panic(err)
	}

	_swagger = swagger

	u, err := url.Parse(_swagger.Servers[0].URL)
	if err != nil {
		panic(err)
	}

	V2APIPath = strings.TrimRight(u.Path, "/")
	V2DocPath = "/doc" + V2APIPath
}

func InitV2Router() http.Handler {
	appManagement := v2Route.NewNivaroOS()

	e := echo.New()

	e.Use(nivaroos_middleware.Cors())

	e.Use(echo_middleware.Gzip())

	// path-only request log: never log ?token=
	e.Use(nivaroos_middleware.RequestLogger())

	e.Use(echojwt.WithConfig(echojwt.Config{
		// socket-peer based loopback check (c.RealIP() trusted
		// X-Forwarded-For/X-Real-IP) - see common/middleware.IsLocalAutomation
		Skipper: nivaroos_middleware.LocalAutomationSkipper(),
		ParseTokenFunc: func(c *echo.Context, token string) (interface{}, error) {
			valid, claims, err := jwt.Validate(token, func() (*ecdsa.PublicKey, error) { return external.GetPublicKey(config.CommonInfo.RuntimePath) })
			if err != nil || !valid {
				if _, revoked := jwt.RevocationReason(err); revoked {
					// The session was ended (its phone removed): say so,
					// with the reason, instead of a bare 401.
					_ = c.JSON(http.StatusUnauthorized, jwt.UnauthorizedResult(err))
				}
				return nil, echo.ErrUnauthorized
			}
			c.Request().Header.Set("user_id", strconv.Itoa(claims.ID))

			return claims, nil
		},
		TokenLookupFuncs: []echo_middleware.ValuesExtractor{
			func(ctx *echo.Context) ([]string, echo_middleware.ExtractorSource, error) {
				if len(ctx.Request().Header.Get(echo.HeaderAuthorization)) > 0 {
					return []string{ctx.Request().Header.Get(echo.HeaderAuthorization)}, echo_middleware.ExtractorSourceHeader, nil
				}
				return []string{ctx.QueryParam("token")}, echo_middleware.ExtractorSourceQuery, nil
			},
		},
	}))

	// e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
	// 	return func(c *echo.Context) error {
	// 		switch c.Request().Header.Get(echo.HeaderContentType) {
	// 		case common.MIMEApplicationYAML: // in case request contains a compose content in YAML
	// 			return oapi.RequestValidator(_swagger, oapi.Options{
	// 				Options: openapi3filter.Options{
	// 					AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
	// 					// ExcludeRequestBody:  true,
	// 					// ExcludeResponseBody: true,
	// 				},
	// 			})(next)(c)

	// 		default:
	// 			return oapi.RequestValidator(_swagger, oapi.Options{
	// 				Options: openapi3filter.Options{
	// 					AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
	// 				},
	// 			})(next)(c)
	// 		}
	// 	}
	// })

	e.Use(oapi.RequestValidator(_swagger, oapi.Options{
		Skipper: func(c *echo.Context) bool {
			// jump validate when upload file
			// because file upload can't pass validate
			// issue: https://github.com/deepmap/oapi-codegen/issues/514
			// Header.Get, not Header[...][0]: a request with no Content-Type
			// at all (every plain GET, e.g. the uploader's resume check)
			// indexed an empty slice and panicked.
			return strings.Contains(c.Request().Header.Get(echo.HeaderContentType), "multipart/form-data")
		},
		Options: openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
	}))

	codegen.RegisterHandlersWithBaseURL(e, appManagement, V2APIPath)

	return e
}

func InitV2DocRouter(docHTML string, docYAML string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == V2DocPath {
			if _, err := w.Write([]byte(docHTML)); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
			}
			return
		}

		if r.URL.Path == V2DocPath+"/openapi.yaml" {
			if _, err := w.Write([]byte(docYAML)); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
			}
		}
	})
}

func InitFile() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if len(token) == 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"message": "token not found"}`))
			return
		}

		valid, _, errs := jwt.Validate(token, func() (*ecdsa.PublicKey, error) { return external.GetPublicKey(config.CommonInfo.RuntimePath) })
		if errs != nil || !valid {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"message": "validation failure"}`))
			return
		}
		filePath := r.URL.Query().Get("path")
		if dev, phonePath := v1.GetCompanionDeviceByStoragePath(filePath); dev != nil {
			if !file.Exists(filePath) {
				if err := v1.ProxyCompanionStream(dev, phonePath, w, r); err == nil {
					return
				}
			}
		}

		fileName := path.Base(filePath)
		w.Header().Add("Content-Disposition", "attachment; filename*=utf-8''"+url.PathEscape(fileName))
		http.ServeFile(w, r, filePath)
		// http.ServeFile(w, r, filePath)
	})
}

func InitDir() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if len(token) == 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"message": "token not found"}`))
			return
		}

		valid, _, errs := jwt.Validate(token, func() (*ecdsa.PublicKey, error) { return external.GetPublicKey(config.CommonInfo.RuntimePath) })
		if errs != nil || !valid {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"message": "validation failure"}`))
			return
		}
		t := r.URL.Query().Get("format")
		files := r.URL.Query().Get("files")

		if len(files) == 0 {
			// w.JSON(common_err.CLIENT_ERROR, model.Result{
			// 	Success: common_err.INVALID_PARAMS,
			// 	Message: common_err.GetMsg(common_err.INVALID_PARAMS),
			// })
			return
		}
		list := strings.Split(files, ",")
		for _, v := range list {
			if !file.Exists(v) {
				// return ctx.JSON(common_err.SERVICE_ERROR, model.Result{
				// 	Success: common_err.FILE_DOES_NOT_EXIST,
				// 	Message: common_err.GetMsg(common_err.FILE_DOES_NOT_EXIST),
				// })
				return
			}
		}
		w.Header().Add("Content-Type", "application/octet-stream")
		w.Header().Add("Content-Transfer-Encoding", "binary")
		w.Header().Add("Cache-Control", "no-cache")
		// handles only single files not folders and multiple files
		//		if len(list) == 1 {

		// filePath := list[0]
		//			info, err := os.Stat(filePath)
		//			if err != nil {

		// w.JSON(http.StatusOK, model.Result{
		// 	Success: common_err.FILE_DOES_NOT_EXIST,
		// 	Message: common_err.GetMsg(common_err.FILE_DOES_NOT_EXIST),
		// })
		//return
		//			}
		//}

		extension, ar, err := file.GetCompressionAlgorithm(t)
		if err != nil {
			// w.JSON(common_err.CLIENT_ERROR, model.Result{
			// 	Success: common_err.INVALID_PARAMS,
			// 	Message: common_err.GetMsg(common_err.INVALID_PARAMS),
			// })
			return
		}

		err = ar.Create(w)
		if err != nil {
			//  return ctx.JSON(common_err.SERVICE_ERROR, model.Result{
			// 	Success: common_err.SERVICE_ERROR,
			// 	Message: common_err.GetMsg(common_err.SERVICE_ERROR),
			// 	Data:    err.Error(),
			// })
			return
		}
		defer ar.Close()
		commonDir := file.CommonPrefix(filepath.Separator, list...)

		currentPath := filepath.Base(commonDir)

		name := "_" + currentPath
		name += extension
		w.Header().Add("Content-Disposition", "attachment; filename*=utf-8''"+url.PathEscape(name))
		for _, fname := range list {
			err = file.AddFile(ar, fname, commonDir)
			if err != nil {
				log.Printf("Failed to archive %s: %v", fname, err)
			}
		}
	})
}
