package v1

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeTailscale answers like the real CLI from canned JSON and records
// every mutating command.
type fakeTailscale struct {
	status, serve string
	serveErr      error
	serveOut      string
	calls         []string
}

func (f *fakeTailscale) run(_ context.Context, args ...string) ([]byte, error) {
	cmd := strings.Join(args, " ")
	switch cmd {
	case "status --json":
		return []byte(f.status), nil
	case "serve status --json":
		return []byte(f.serve), nil
	}
	f.calls = append(f.calls, cmd)
	if strings.HasPrefix(cmd, "serve --bg") {
		if f.serveErr != nil {
			return []byte(f.serveOut), f.serveErr
		}
		f.serve = ourServe
	} else if strings.HasSuffix(cmd, " off") {
		f.serve = "{}"
	}
	return nil, nil
}

const (
	tsReady    = `{"BackendState":"Running","CertDomains":["box.tail1.ts.net"],"Self":{"DNSName":"box.tail1.ts.net."},"CurrentTailnet":{"MagicDNSEnabled":true}}`
	tsNoCerts  = `{"BackendState":"Running","CertDomains":null,"Self":{"DNSName":"box.tail1.ts.net."},"CurrentTailnet":{"MagicDNSEnabled":false}}`
	tsStopped  = `{"BackendState":"Stopped","Self":{"DNSName":""}}`
	ourServe   = `{"TCP":{"443":{"HTTPS":true}},"Web":{"box.tail1.ts.net:443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:80"}}}}}`
	otherServe = `{"TCP":{"443":{"HTTPS":true}},"Web":{"box.tail1.ts.net:443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:3000"}}}}}`
	funnel     = `{"TCP":{"443":{"HTTPS":true}},"Web":{"box.tail1.ts.net:443":{"Handlers":{"/grafana":{"Proxy":"http://127.0.0.1:3000"}}}},"AllowFunnel":{"box.tail1.ts.net:443":true}}`
)

func withFakeTailscale(t *testing.T, f *fakeTailscale) {
	run, port := tailscaleRun, gatewayPort
	tailscaleRun, gatewayPort = f.run, func() string { return "80" }
	t.Cleanup(func() { tailscaleRun, gatewayPort = run, port })
}

func TestTailscaleServe(t *testing.T) {
	ctx := context.Background()

	t.Run("enable then disable runs exactly our commands", func(t *testing.T) {
		f := &fakeTailscale{status: tsReady, serve: "{}"}
		withFakeTailscale(t, f)
		st, err := setTailscaleServe(ctx, true)
		if err != nil || !st.Enabled || st.URL != "https://box.tail1.ts.net" {
			t.Fatalf("enable: %+v %v", st, err)
		}
		if st, err = setTailscaleServe(ctx, true); err != nil || !st.Enabled {
			t.Fatalf("enable again: %+v %v", st, err)
		}
		if st, err = setTailscaleServe(ctx, false); err != nil || st.Enabled {
			t.Fatalf("disable: %+v %v", st, err)
		}
		want := []string{"serve --bg --yes --https=443 http://127.0.0.1:80", "serve --yes --https=443 off"}
		if strings.Join(f.calls, "|") != strings.Join(want, "|") {
			t.Fatalf("calls %q, want %q", f.calls, want)
		}
	})

	t.Run("no certs: clear error with admin link, nothing run", func(t *testing.T) {
		f := &fakeTailscale{status: tsNoCerts, serve: "{}"}
		withFakeTailscale(t, f)
		_, err := setTailscaleServe(ctx, true)
		if err == nil || !strings.Contains(err.Error(), tailscaleDNSAdminURL) || len(f.calls) != 0 {
			t.Fatalf("%v %q", err, f.calls)
		}
	})

	t.Run("owner's own 443 config and funnel are never touched", func(t *testing.T) {
		for name, sc := range map[string]string{"other handler": otherServe, "funnel": funnel} {
			f := &fakeTailscale{status: tsReady, serve: sc}
			withFakeTailscale(t, f)
			st, _ := readTailscaleServe(ctx)
			if st.Enabled || st.Conflict == "" {
				t.Fatalf("%s: %+v", name, st)
			}
			if _, err := setTailscaleServe(ctx, true); err == nil {
				t.Fatalf("%s: enable should refuse", name)
			}
			if _, err := setTailscaleServe(ctx, false); err != nil {
				t.Fatalf("%s: disable of not-ours: %v", name, err)
			}
			if len(f.calls) != 0 {
				t.Fatalf("%s: ran %q", name, f.calls)
			}
		}
	})

	t.Run("stopped or no daemon is a state", func(t *testing.T) {
		for _, s := range []string{tsStopped, ""} {
			f := &fakeTailscale{status: s}
			withFakeTailscale(t, f)
			st, err := readTailscaleServe(ctx)
			if err != nil || st.Running || st.URL != "" {
				t.Fatalf("%q: %+v %v", s, st, err)
			}
		}
	})

	t.Run("serve not allowed on tailnet surfaces its link", func(t *testing.T) {
		f := &fakeTailscale{status: tsReady, serve: "{}", serveErr: errors.New("signal: killed"),
			serveOut: "Serve is not enabled on your tailnet.\nTo enable, visit:\n\n         https://login.tailscale.com/f/serve?node=abc\n"}
		withFakeTailscale(t, f)
		_, err := setTailscaleServe(ctx, true)
		if err == nil || !strings.Contains(err.Error(), "https://login.tailscale.com/f/serve?node=abc") {
			t.Fatal(err)
		}
	})
}
