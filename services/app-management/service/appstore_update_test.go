package service_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/app-management/common"
	"github.com/F-e-n-y-x/NivaroOS/services/app-management/pkg/config"
	"github.com/F-e-n-y-x/NivaroOS/services/app-management/service"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"gotest.tools/v3/assert"
)

func storeZip(t *testing.T, apps map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, compose := range apps {
		f, err := w.Create("store-main/Apps/" + name + "/docker-compose.yml")
		assert.NilError(t, err)
		_, err = f.Write([]byte(compose))
		assert.NilError(t, err)
	}
	assert.NilError(t, w.Close())

	return buf.Bytes()
}

// a store server that counts downloads; etag "" sends no validators at all
// (like GitHub codeload's HEAD: no Content-Length either)
func storeServer(t *testing.T, body []byte, etag *atomic.Value, gets *atomic.Int32) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tag, _ := etag.Load().(string); tag != "" {
			w.Header().Set("ETag", tag)
		}

		if r.Method == http.MethodHead {
			// chunked-style: unknown length
			w.WriteHeader(http.StatusOK)
			return
		}

		gets.Add(1)
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)

	return server
}

const nullXCasaOSApp = "name: broken\nservices:\n  web:\n    image: nginx\nx-casaos:\n"

const goodApp = `name: good
services:
  web:
    image: nginx:1.25
x-casaos:
  main: web
  title:
    en_us: Good
`

func TestUpdateCatalogUsesETagAndSurvivesBadApps(t *testing.T) {
	logger.LogInitConsoleOnly()

	config.AppInfo.AppStorePath = t.TempDir()

	var etag atomic.Value
	etag.Store(`"v1"`)
	var gets atomic.Int32

	server := storeServer(t, storeZip(t, map[string]string{"good": goodApp, "broken": nullXCasaOSApp}), &etag, &gets)

	appStore, err := service.AppStoreByURL(server.URL + "/store/main.zip")
	assert.NilError(t, err)

	// first run downloads; the app with `x-casaos:` (null) does not crash it
	assert.NilError(t, appStore.UpdateCatalog())
	assert.Equal(t, gets.Load(), int32(1))

	catalog, err := appStore.Catalog()
	assert.NilError(t, err)
	_, ok := catalog["good"]
	assert.Assert(t, ok)

	// same ETag: skipped
	assert.NilError(t, appStore.UpdateCatalog())
	assert.Equal(t, gets.Load(), int32(1))

	// new ETag: downloaded
	etag.Store(`"v2"`)
	assert.NilError(t, appStore.UpdateCatalog())
	assert.Equal(t, gets.Load(), int32(2))

	// workdir deleted: downloaded again even though the ETag is the same
	workdir, err := appStore.WorkDir()
	assert.NilError(t, err)
	assert.NilError(t, os.RemoveAll(workdir))
	assert.NilError(t, appStore.UpdateCatalog())
	assert.Equal(t, gets.Load(), int32(3))

	// no ETag / Last-Modified / length (codeload): always downloaded
	etag.Store("")
	assert.NilError(t, appStore.UpdateCatalog())
	assert.NilError(t, appStore.UpdateCatalog())
	assert.Equal(t, gets.Load(), int32(5))
}

func TestRegisterAppStoreValidatesSynchronously(t *testing.T) {
	logger.LogInitConsoleOnly()

	file, err := os.CreateTemp("", "app-management.conf")
	assert.NilError(t, err)
	defer os.Remove(file.Name())

	config.InitSetup(file.Name(), "")
	config.AppInfo.AppStorePath = t.TempDir()

	management := service.NewAppStoreManagement()
	ctx := common.WithProperties(context.Background(), map[string]string{})

	for _, bad := range []string{"ftp://example.com/x.zip", "not a url", "file:///etc/passwd", "https://"} {
		err := management.RegisterAppStore(ctx, bad)
		assert.Assert(t, errors.Is(err, service.ErrAppStoreInvalidURL), "%s: %v", bad, err)
	}

	// already registered (case-insensitive) -> exists, before any download
	added, err := config.AddAppStore("https://example.com/Store.zip")
	assert.NilError(t, err)
	assert.Assert(t, added)

	err = management.RegisterAppStore(ctx, "https://EXAMPLE.com/store.zip")
	assert.Equal(t, err, service.ErrAppStoreSourceExists)

	// unregister by url, which does not depend on list positions
	assert.NilError(t, management.UnregisterAppStoreByURL("https://example.com/store.zip"))
	assert.Equal(t, len(config.AppStoreList()), 0)
	assert.Equal(t, management.UnregisterAppStoreByURL("https://example.com/store.zip"), service.ErrAppStoreSourceNotFound)
}
