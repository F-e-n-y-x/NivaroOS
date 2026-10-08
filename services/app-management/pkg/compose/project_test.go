package compose

import (
	"encoding/json"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
)

// The web reads x-casaos from the compose app JSON.
func TestProjectJSONKeepsExtensions(t *testing.T) {
	s := types.ServiceConfig{Name: "web"}
	s.Image = "nginx"
	p := &Project{
		Name:       "demo",
		Services:   types.Services{"web": s},
		Extensions: types.Extensions{"x-casaos": map[string]any{"main": "web"}},
	}
	for _, v := range []any{p, *p, struct{ C *Project }{p}} {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if c, ok := got["C"].(map[string]any); ok {
			got = c
		}
		x, _ := got["x-casaos"].(map[string]any)
		svc, _ := got["services"].(map[string]any)["web"].(map[string]any)
		if x["main"] != "web" || svc["image"] != "nginx" || got["name"] != "demo" {
			t.Fatalf("%s", raw)
		}
	}
}
