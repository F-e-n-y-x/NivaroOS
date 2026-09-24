package service

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/app-management/pkg/config"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/compose-spec/compose-go/types"
	"github.com/docker/compose/v2/pkg/api"
	"gotest.tools/v3/assert"
)

func init() {
	logger.LogInitConsoleOnly()
}

const composeWithNullXCasaOS = `name: nullx
services:
  web:
    image: nginx:1.25
x-casaos:
`

// `x-casaos:` with no value used to panic (unchecked type assertion) and
// took down the catalog build goroutine or POST /compose.
func TestNullXCasaOSDoesNotPanic(t *testing.T) {
	for _, skipInterpolation := range []bool{true, false} {
		app, err := NewComposeAppFromYAML([]byte(composeWithNullXCasaOS), skipInterpolation, true)
		assert.NilError(t, err)

		// the null extension is replaced by one holding the default title
		storeInfo, err := app.StoreInfo(true)
		assert.NilError(t, err)
		assert.Equal(t, storeInfo.Title["en_us"], "nullx")

		// and a null extension set after parsing is "not found", not a panic
		app.Extensions["x-casaos"] = nil
		_, err = app.StoreInfo(true)
		assert.Equal(t, err, ErrComposeExtensionNameXCasaOSNotFound)

		_, ok := app.IsUncontrolled()
		assert.Assert(t, !ok)
		assert.Equal(t, app.StoreAppID(), "nullx")
		assert.Equal(t, app.AuthorType() != "", true)
	}

	// a store catalog entry like that parses (and is kept) instead of panicking
	app, err := parseCatalogComposeApp([]byte(composeWithNullXCasaOS))
	assert.NilError(t, err)
	assert.Equal(t, app.Name, "nullx")

	// x-casaos that is not a mapping
	app, err = NewComposeAppFromYAML([]byte("name: listx\nservices:\n  web:\n    image: nginx\nx-casaos: [1, 2]\n"), true, true)
	assert.NilError(t, err)
	_, ok := app.XCasaOS()
	assert.Assert(t, !ok)
	assert.Assert(t, app.SetUncontrolled(true) != nil)
}

func TestUninstallKeepDataKeepsVolumesAndImages(t *testing.T) {
	keep := uninstallDownOptions(false)
	assert.Equal(t, keep.Volumes, false)
	assert.Equal(t, keep.Images, "")

	remove := uninstallDownOptions(true)
	assert.Equal(t, remove.Volumes, true)
	assert.Equal(t, remove.Images, "all")
	assert.Equal(t, remove.RemoveOrphans, true)
}

func TestAppDataPathsToRemove(t *testing.T) {
	oldRoot, oldApps := AppDataRoot, config.AppInfo.AppsPath
	defer func() { AppDataRoot, config.AppInfo.AppsPath = oldRoot, oldApps }()

	AppDataRoot = "/DATA/AppData"
	config.AppInfo.AppsPath = "/var/lib/nivaroos/apps"

	cases := []struct {
		name       string
		app        string
		sources    []string
		workingDir string
		want       []string
	}{
		{"own appdata subfolders", "tv", []string{"/DATA/AppData/tv/config", "/DATA/AppData/tv/data"}, "", []string{"/DATA/AppData/tv"}},
		{"own appdata folder itself", "tv", []string{"/DATA/AppData/tv"}, "", []string{"/DATA/AppData/tv"}},
		{"media folder named like the app", "tv", []string{"/DATA/Media/tv", "/DATA/Media/tv/shows"}, "", []string{}},
		{"mount point named like the app", "data", []string{"/mnt/data", "/mnt/data/x"}, "", []string{}},
		{"DATA itself", "DATA", []string{"/DATA", "/DATA/AppData"}, "", []string{}},
		{"relative paths", "tv", []string{"./tv", "tv", "data/tv"}, "", []string{}},
		{"prefix is not a path segment", "tv", []string{"/DATA/AppData/tv2/config", "/DATA/AppData/tvheadend"}, "", []string{}},
		{"other app's folder", "tv", []string{"/DATA/AppData/plex/tv"}, "", []string{}},
		{"escape with ..", "tv", []string{"/DATA/AppData/tv/../plex"}, "", []string{}},
		{"appdata root", "tv", []string{"/DATA/AppData"}, "", []string{}},
		{"empty / anonymous", "tv", []string{""}, "", []string{}},
		{"managed working dir", "tv", nil, "/var/lib/nivaroos/apps/tv", []string{"/var/lib/nivaroos/apps/tv"}},
		{"foreign working dir", "tv", nil, "/home/user/tv", []string{}},
		{"apps root as working dir", "tv", nil, "/var/lib/nivaroos/apps", []string{}},
		{"v1 container name", "/tv", []string{"/DATA/AppData/tv/config", "/DATA/Media/tv"}, "", []string{"/DATA/AppData/tv"}},
		{"bad names", "..", []string{"/DATA/AppData/x"}, "", []string{}},
		{"name with slash", "a/b", []string{"/DATA/AppData/a/b"}, "", []string{}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.DeepEqual(t, AppDataPathsToRemove(c.app, c.sources, c.workingDir), c.want)
		})
	}
}

func TestIsPathWithin(t *testing.T) {
	assert.Assert(t, isPathWithin("/a/b", "/a/b"))
	assert.Assert(t, isPathWithin("/a/b", "/a/b/c"))
	assert.Assert(t, !isPathWithin("/a/b", "/a/bc"))
	assert.Assert(t, !isPathWithin("/a/b", "/a"))
	assert.Assert(t, !isPathWithin("/a/b", "/x/a/b"))
}

// Update used to overwrite the images of the installed app in place, so the
// rollback started the new (failed) images again.
func TestUpdatedServiceImagesDoesNotMutateLocal(t *testing.T) {
	local := types.Services{{Name: "web", Image: "nginx:1.24"}, {Name: "db", Image: "postgres:15"}, {Name: "cache", Image: "redis:latest"}}
	store := types.Services{{Name: "web", Image: "nginx:1.25"}, {Name: "db", Image: "postgres:16"}, {Name: "cache", Image: "redis:latest"}}

	updated := updatedServiceImages(local, store)

	assert.Equal(t, local[0].Image, "nginx:1.24")
	assert.Equal(t, local[1].Image, "postgres:15")
	assert.Equal(t, updated[0].Image, "nginx:1.25")
	assert.Equal(t, updated[1].Image, "postgres:16")
	assert.Equal(t, updated[2].Image, "redis:latest")

	app := &ComposeApp{Name: "x", Services: local}
	copied := app.withServices(updated)
	assert.Equal(t, app.Services[0].Image, "nginx:1.24")
	assert.Equal(t, copied.Services[0].Image, "nginx:1.25")
}

func TestStoreFingerprint(t *testing.T) {
	h := http.Header{}
	assert.Equal(t, storeFingerprint(h, -1), "") // codeload: unknown
	assert.Equal(t, storeFingerprint(h, 0), "")
	assert.Equal(t, storeFingerprint(h, 1234), "size:1234")

	h.Set("Last-Modified", "Tue, 22 Sep 2026 08:58:18 GMT")
	assert.Equal(t, storeFingerprint(h, 1234), "last-modified:Tue, 22 Sep 2026 08:58:18 GMT")

	h.Set("ETag", `"abc"`)
	assert.Equal(t, storeFingerprint(h, -1), `etag:"abc"`)

	assert.Assert(t, storeUnchanged(`etag:"abc"`, `etag:"abc"`, true))
	assert.Assert(t, !storeUnchanged(`etag:"abc"`, `etag:"abc"`, false), "missing workdir must re-download")
	assert.Assert(t, !storeUnchanged("", "", true), "unknown must download")
	assert.Assert(t, !storeUnchanged(`etag:"abc"`, `etag:"def"`, true))
}

func TestReferencedGlobalKeys(t *testing.T) {
	global := map[string]string{"OPENAI_API_KEY": "sk", "OTHER_SECRET": "x", "TOKEN": "t", "ESCAPED": "e"}
	raw := []byte("services:\n  a:\n    environment:\n      KEY: ${OPENAI_API_KEY:-}\n    command: echo $TOKEN $$ESCAPED\n")

	keys := referencedGlobalKeys(raw, types.MappingWithEquals{"DECLARED": nil}, global)
	assert.DeepEqual(t, keys, []string{"OPENAI_API_KEY", "TOKEN"})

	// declared by name in environment (value from the global settings)
	keys = referencedGlobalKeys(nil, types.MappingWithEquals{"OTHER_SECRET": nil}, global)
	assert.DeepEqual(t, keys, []string{"OTHER_SECRET"})

	assert.DeepEqual(t, referencedGlobalKeys([]byte("image: nginx"), nil, global), []string{})
}

func TestInjectOnlyReferencedGlobals(t *testing.T) {
	oldGlobal := config.GlobalSnapshot()
	defer func() {
		for k := range config.GlobalSnapshot() {
			config.DeleteGlobal(k)
		}
		for k, v := range oldGlobal {
			_ = config.SetGlobal(k, v)
		}
	}()
	assert.NilError(t, config.SetGlobal("OPENAI_API_KEY", "sk-test"))
	assert.NilError(t, config.SetGlobal("UNRELATED", "nope"))

	dir := t.TempDir()
	composeFile := filepath.Join(dir, "docker-compose.yml")
	assert.NilError(t, os.WriteFile(composeFile, []byte("services:\n  a:\n    image: x\n    command: run $$OPENAI_API_KEY\n  b:\n    image: y\n    environment:\n      K: $OPENAI_API_KEY\n"), 0o600))

	app := &ComposeApp{
		Name:         "inj",
		ComposeFiles: []string{composeFile},
		Services:     types.Services{{Name: "a"}, {Name: "b", Environment: types.MappingWithEquals{}}},
	}
	app.injectEnvVariableToComposeApp()

	// the file references OPENAI_API_KEY - it is injected (also into a service
	// with a nil environment map, which used to panic); UNRELATED is not
	for _, s := range app.Services {
		assert.Equal(t, *s.Environment["OPENAI_API_KEY"], "sk-test")
		_, has := s.Environment["UNRELATED"]
		assert.Assert(t, !has)
	}
}

func TestLoadComposeAppIgnoresProcessEnv(t *testing.T) {
	t.Setenv("NIVAROOS_TEST_HOST_SECRET", "leaked")

	oldGlobal := config.GlobalSnapshot()
	defer func() {
		config.DeleteGlobal("NIVAROOS_TEST_GLOBAL")
		for k, v := range oldGlobal {
			_ = config.SetGlobal(k, v)
		}
	}()
	assert.NilError(t, config.SetGlobal("NIVAROOS_TEST_GLOBAL", "global-value"))

	dir := t.TempDir()
	composeFile := filepath.Join(dir, "docker-compose.yml")
	override := filepath.Join(dir, "docker-compose.override.yml")
	assert.NilError(t, os.WriteFile(composeFile, []byte(`name: envtest
services:
  web:
    image: nginx
    environment:
      HOST: "${NIVAROOS_TEST_HOST_SECRET}"
      ID: "$AppID"
      PUID: "$PUID"
      G: "${NIVAROOS_TEST_GLOBAL}"
`), 0o600))
	assert.NilError(t, os.WriteFile(override, []byte("services:\n  web:\n    labels:\n      from-override: \"yes\"\n"), 0o600))

	app, err := LoadComposeAppFromConfigFiles("envtest", stackConfigFiles(composeFile+","+override))
	assert.NilError(t, err)

	env := app.Services[0].Environment
	assert.Equal(t, *env["HOST"], "")
	assert.Equal(t, *env["ID"], "envtest")
	assert.Equal(t, *env["PUID"], baseInterpolationMap()["PUID"])
	assert.Equal(t, *env["G"], "global-value")

	// both files of the project are loaded, and compose labels are set
	assert.Equal(t, app.Services[0].Labels["from-override"], "yes")
	assert.Equal(t, app.Services[0].CustomLabels[api.ProjectLabel], "envtest")
	assert.Equal(t, len(app.ComposeFiles), 2)

	// the install-time parser keeps references for later instead of
	// resolving them from the process environment
	parsed, err := NewComposeAppFromYAML([]byte("name: p\nservices:\n  web:\n    image: nginx\n    environment:\n      HOST: ${NIVAROOS_TEST_HOST_SECRET}\n"), false, true)
	assert.NilError(t, err)
	assert.Assert(t, !strings.Contains(*parsed.Services[0].Environment["HOST"], "leaked"))
}

func TestStackConfigFiles(t *testing.T) {
	assert.DeepEqual(t, stackConfigFiles("/a/docker-compose.yml"), []string{"/a/docker-compose.yml"})
	assert.DeepEqual(t, stackConfigFiles("/a/x.yml, /a/y.yml,"), []string{"/a/x.yml", "/a/y.yml"})
	assert.DeepEqual(t, stackConfigFiles(""), []string{})
}

func TestBindSourceToCreate(t *testing.T) {
	named := types.Volumes{"dbdata": types.VolumeConfig{}}

	assert.Equal(t, bindSourceToCreate(types.ServiceVolumeConfig{Type: "volume", Source: "", Target: "/data"}, named), "")     // anonymous
	assert.Equal(t, bindSourceToCreate(types.ServiceVolumeConfig{Type: "tmpfs", Target: "/tmp"}, named), "")                   // tmpfs
	assert.Equal(t, bindSourceToCreate(types.ServiceVolumeConfig{Type: "volume", Source: "dbdata", Target: "/db"}, named), "") // named
	assert.Equal(t, bindSourceToCreate(types.ServiceVolumeConfig{Type: "bind", Source: "/DATA/AppData/x/config"}, named), "/DATA/AppData/x/config")
	assert.Equal(t, bindSourceToCreate(types.ServiceVolumeConfig{Type: "bind", Source: "relative"}, named), "")
	assert.Equal(t, bindSourceToCreate(types.ServiceVolumeConfig{Type: "npipe", Source: "/x"}, named), "")
}

func TestImagePullProgressNeverGoesBackwards(t *testing.T) {
	// the old formula (fraction * current/total) went 50% -> 0% when the
	// second of two images started
	assert.Equal(t, imagePullProgress(1, 1, 1, 2), 50)
	assert.Equal(t, imagePullProgress(0, 3, 2, 2), 50)
	assert.Equal(t, imagePullProgress(3, 3, 2, 2), 100)
	assert.Equal(t, imagePullProgress(0, 0, 1, 1), 0)
	assert.Equal(t, imagePullProgress(5, 3, 1, 1), 100)
	assert.Equal(t, imagePullProgress(1, 1, 1, 0), 0)

	last := 0
	for current := 1; current <= 3; current++ {
		for done := 0; done <= 4; done++ {
			p := imagePullProgress(done, 4, current, 3)
			assert.Assert(t, p >= last, "image %d layer %d: %d < %d", current, done, p, last)
			last = p
		}
	}
	assert.Equal(t, last, 100)
}

func TestGitRunsAsTheRepoOwner(t *testing.T) {
	home := func(uid uint32) string { return "/home/u" }

	// a repo owned by an unprivileged user: git runs as that user, with
	// their HOME, and without any root-only overrides
	runAs := gitIdentityFor(1000, 1001, home)
	assert.DeepEqual(t, runAs, &gitRunAs{UID: 1000, GID: 1001, Home: "/home/u"})
	args := strings.Join(gitArgs("/srv/app", runAs, "fetch", "origin"), " ")
	assert.Equal(t, args, "-C /srv/app fetch origin")

	// root's own repo: root, trusted by exact path only, no fsmonitor/hooks
	assert.Assert(t, gitIdentityFor(0, 0, home) == nil)
	args = strings.Join(gitArgs("/srv/app", nil, "pull", "--ff-only"), " ")
	assert.Assert(t, !strings.Contains(args, "safe.directory=*"), args)
	assert.Assert(t, strings.Contains(args, "safe.directory=/srv/app"), args)
	assert.Assert(t, strings.Contains(args, "core.fsmonitor=false"), args)
	assert.Assert(t, strings.Contains(args, "core.hooksPath=/dev/null"), args)
	assert.Assert(t, strings.HasSuffix(args, "pull --ff-only"), args)

	// gitCmd wires the owner into the process credentials
	dir := t.TempDir()
	if os.Geteuid() == 0 {
		assert.NilError(t, os.Chown(dir, 12345, 12345))
		cmd := gitCmd(context.Background(), dir, "status")
		assert.Assert(t, cmd.SysProcAttr != nil && cmd.SysProcAttr.Credential != nil)
		assert.DeepEqual(t, *cmd.SysProcAttr.Credential, syscall.Credential{Uid: 12345, Gid: 12345})
		assert.Assert(t, strings.Contains(strings.Join(cmd.Env, "\n"), "HOME="))
	}
}

func TestInstallRefusesAConcurrentInstallOfTheSameApp(t *testing.T) {
	s := NewComposeService()
	s.installationInProgress.Store("dup", true)

	err := s.Install(context.Background(), &ComposeApp{Name: "dup"})
	assert.Equal(t, err, ErrComposeAppAlreadyInstalling)

	// the reservation of the running install is left alone
	assert.Assert(t, s.IsInstalling("dup"))
}
