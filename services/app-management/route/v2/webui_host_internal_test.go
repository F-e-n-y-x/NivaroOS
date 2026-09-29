package v2

import (
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/app-management/service"
	"github.com/compose-spec/compose-go/types"
)

func TestWebUIBoundHostIP(t *testing.T) {
	app := &service.ComposeApp{
		Services: types.Services{
			{Name: "web", Ports: []types.ServicePortConfig{
				{HostIP: "192.168.1.10", Published: "2283", Target: 2283},
				{HostIP: "0.0.0.0", Published: "8080", Target: 80},
				{Published: "9000", Target: 9000},
				{HostIP: "127.0.0.1", Published: "5432", Target: 5432},
				{HostIP: "fd00::10", Published: "7000", Target: 7000},
			}},
			{Name: "db", Ports: []types.ServicePortConfig{{HostIP: "192.168.1.11", Published: "3306", Target: 3306}}},
		},
	}
	main := "web"
	cases := []struct {
		main    *string
		portMap string
		want    string
	}{
		{&main, "2283", "192.168.1.10"},
		{&main, " 2283 ", "192.168.1.10"},
		{&main, "8080", ""},   // all addresses: keep the dashboard's host
		{&main, "9000", ""},   // no host ip
		{&main, "5432", ""},   // loopback-only is not reachable remotely either way
		{&main, "7000", "[fd00::10]"},
		{&main, "3306", ""},   // other service than main
		{nil, "3306", "192.168.1.11"},
		{&main, "", ""},
	}
	for _, c := range cases {
		if got := webUIBoundHostIP(app, c.main, c.portMap); got != c.want {
			t.Errorf("portMap %q main %v: got %q want %q", c.portMap, c.main, got, c.want)
		}
	}
	if webUIBoundHostIP(nil, &main, "2283") != "" {
		t.Error("nil app")
	}
}
