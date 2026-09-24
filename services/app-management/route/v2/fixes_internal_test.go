package v2

import (
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/app-management/codegen"
	"github.com/compose-spec/compose-go/types"
	"github.com/docker/compose/v2/pkg/api"
	"gotest.tools/v3/assert"
)

// only apps that are running now are recreated when a global setting changes
func TestOnlyRunningAppsAreReapplied(t *testing.T) {
	assert.Assert(t, !isRunning(nil))
	assert.Assert(t, !isRunning([]api.ContainerSummary{{State: "exited"}, {State: "created"}}))
	assert.Assert(t, isRunning([]api.ContainerSummary{{State: "exited"}, {State: "running"}}))
}

func TestWebAppGridItemImageIsTheMainService(t *testing.T) {
	main := "web"
	app := &codegen.ComposeAppWithStoreInfo{
		Compose: &codegen.ComposeApp{
			Name:     "demo",
			Services: types.Services{{Name: "db", Image: "postgres:16"}, {Name: "web", Image: "nginx:1.25"}},
		},
		StoreInfo: &codegen.ComposeAppStoreInfo{Main: &main},
	}

	// the main service is not the first one: its image used to be dropped
	item, err := WebAppGridItemAdapterV2(app)
	assert.NilError(t, err)
	assert.Assert(t, item.Image != nil)
	assert.Equal(t, *item.Image, "nginx:1.25")

	// no main service set: no panic
	app.StoreInfo.Main = nil
	item, err = WebAppGridItemAdapterV2(app)
	assert.NilError(t, err)
	assert.Assert(t, item.Image == nil)
}
