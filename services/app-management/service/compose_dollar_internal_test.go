package service

import (
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
)

// A literal `$` in an env value (written `$$` in the file) must survive
// the app settings round trip: GET compose/{id} as YAML, PUT the same text.
func TestComposeSettingsRoundTripKeepsDollar(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "docker-compose.yml")
	const original = `name: dollar
services:
  app:
    image: nginx:latest
    environment:
      PASS: pa$$word
      HASH: $$2y$$10$$abc
`
	assert.NilError(t, os.WriteFile(file, []byte(original), 0o600))

	for round := 0; round < 3; round++ {
		// MyComposeApp: the installed file, interpolated, written as YAML.
		installed, err := LoadComposeAppFromConfigFiles("dollar", []string{file})
		assert.NilError(t, err)
		env := installed.Services[0].Environment
		assert.Equal(t, *env["PASS"], "pa$word", "round %d", round)
		assert.Equal(t, *env["HASH"], "$2y$10$abc", "round %d", round)
		got, err := GenerateYAMLFromComposeApp(*installed)
		assert.NilError(t, err)

		// ApplyComposeAppSettings: the same text back, uninterpolated.
		app, err := NewComposeAppFromYAML(got, true, true)
		assert.NilError(t, err)
		saved, err := MarshalUninterpolatedComposeApp(*app)
		assert.NilError(t, err)
		assert.NilError(t, os.WriteFile(file, saved, 0o600))
	}
}
