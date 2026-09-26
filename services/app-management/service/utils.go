package service

import (
	"regexp"
	"strings"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils"
	"gopkg.in/yaml.v3"
)

var nonAlphaNumeric = regexp.MustCompile(`[^a-z0-9]+`)

func Standardize(text string) string {
	if text == "" {
		return "unknown"
	}

	result := strings.ToLower(text)

	// Replace any non-alphanumeric characters with a single hyphen
	result = nonAlphaNumeric.ReplaceAllString(result, "-")

	for strings.Contains(result, "--") {
		result = strings.Replace(result, "--", "-", -1)
	}

	// Remove any leading or trailing hyphens
	result = strings.Trim(result, "-")

	return result
}

// GenerateYAMLFromComposeApp writes a compose app that was loaded WITH
// interpolation (so a literal `$` in the file, written `$$`, now reads `$`)
// back as a compose file, escaping `$` in environment values again.
//
// Only for an interpolated app, such as the installed one MyComposeApp
// hands out. A file parsed with interpolation skipped (the body of
// ApplyComposeAppSettings) still holds its `$$` escapes: write that with
// MarshalUninterpolatedComposeApp, or every save doubles them.
func GenerateYAMLFromComposeApp(compose ComposeApp) ([]byte, error) {
	// to duplicate Specify Chars
	for _, service := range compose.Services {
		// it should duplicate all values that contains $. But for now, we only duplicate the env values
		for key, value := range service.Environment {
			if strings.ContainsAny(*value, "$") {
				service.Environment[key] = utils.Ptr(strings.Replace(*value, "$", "$$", -1))
			}
		}
	}
	return yaml.Marshal(compose)
}

// MarshalUninterpolatedComposeApp writes a compose app parsed with
// interpolation skipped back as it came: its `$$` escapes are still in
// place, so nothing is escaped again. GET compose/{id} (YAML) → PUT the
// same text is then the identity, and `pa$$word` in the file stays
// `pa$word` in the container however often the app's settings are saved.
func MarshalUninterpolatedComposeApp(compose ComposeApp) ([]byte, error) {
	return yaml.Marshal(compose)
}
