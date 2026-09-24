package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"gotest.tools/v3/assert"
)

func TestGlobalEnvFileIsNextToTheConfigFile(t *testing.T) {
	assert.Equal(t, GlobalEnvFilePathFor(""), AppManagementGlobalEnvFilePath)
	assert.Equal(t, GlobalEnvFilePathFor("/etc/nivaroos/app-management.conf"), "/etc/nivaroos/env")
	assert.Equal(t, GlobalEnvFilePathFor("/tmp/x/custom.conf"), "/tmp/x/env")
}

func TestValidateGlobal(t *testing.T) {
	assert.NilError(t, ValidateGlobal("OPENAI_API_KEY", "sk-123=abc"))
	assert.NilError(t, ValidateGlobal("_x1", ""))

	for _, key := range []string{"", "1ABC", "A-B", "A B", "A=B", "A\nB"} {
		assert.Assert(t, ValidateGlobal(key, "v") != nil, key)
	}

	for _, value := range []string{"a\nEVIL=1", "a\rb", "a\x00b"} {
		assert.Assert(t, ValidateGlobal("KEY", value) != nil, value)
	}
}

func TestSaveGlobalWritesTheConfiguredFileAndRoundTrips(t *testing.T) {
	oldPath, oldGlobal := GlobalEnvFilePath, GlobalSnapshot()
	defer func() {
		GlobalEnvFilePath = oldPath
		globalMu.Lock()
		Global = oldGlobal
		globalMu.Unlock()
	}()

	dir := t.TempDir()
	configFile := filepath.Join(dir, "app-management.conf")
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "env"), []byte("# comment\nA=1\n"), 0o600))

	globalMu.Lock()
	Global = map[string]string{}
	globalMu.Unlock()

	InitGlobal(configFile)
	assert.Equal(t, GlobalEnvFilePath, filepath.Join(dir, "env"))
	value, ok := GetGlobal("A")
	assert.Assert(t, ok)
	assert.Equal(t, value, "1")

	// the ini config file itself is never parsed as env
	_, ok = GetGlobal("[common]")
	assert.Assert(t, !ok)

	assert.Assert(t, SetGlobal("B", "x\nC=2") != nil)

	// concurrent writers and savers do not race (run with -race)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _ = SetGlobal("B", "2") }()
		go func() { defer wg.Done(); assert.NilError(t, SaveGlobal()) }()
	}
	wg.Wait()
	assert.NilError(t, SaveGlobal())

	content, err := os.ReadFile(filepath.Join(dir, "env"))
	assert.NilError(t, err)
	assert.Equal(t, string(content), "A=1\nB=2\n")
}

func TestAppStoreListAddRemove(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "app-management.conf")
	InitSetup(configFile, "[server]\n")

	added, err := AddAppStore("https://a.example/store.zip")
	assert.NilError(t, err)
	assert.Assert(t, added)

	added, err = AddAppStore("HTTPS://A.EXAMPLE/store.zip")
	assert.NilError(t, err)
	assert.Assert(t, !added, "duplicates are refused case-insensitively")

	_, _ = AddAppStore("https://b.example/store.zip")

	// the returned list is a copy
	list := AppStoreList()
	list[0] = "changed"
	assert.DeepEqual(t, AppStoreList(), []string{"https://a.example/store.zip", "https://b.example/store.zip"})

	removed, err := RemoveAppStore("https://a.example/store.zip")
	assert.NilError(t, err)
	assert.Assert(t, removed)

	removed, err = RemoveAppStore("https://a.example/store.zip")
	assert.NilError(t, err)
	assert.Assert(t, !removed)

	// persisted
	ReloadConfig()
	assert.DeepEqual(t, AppStoreList(), []string{"https://b.example/store.zip"})
}
