package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/core/service"
	"github.com/labstack/echo/v5"
	"github.com/tidwall/gjson"
)

// HTTPS for the dashboard on the tailnet via `tailscale serve`: tailscaled
// terminates TLS with a real cert for <machine>.<tailnet>.ts.net on :443
// and proxies to the gateway on loopback. Only our "/" handler on 443 is
// ever touched; other serve config the owner has is left alone and Funnel
// (public internet) is never enabled.

const tailscaleDNSAdminURL = "https://login.tailscale.com/admin/dns"

// tailscaleRun runs the tailscale CLI and returns stdout; a failure carries
// the CLI's own message. Tests replace it.
var tailscaleRun = func(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "tailscale", args...)
	out, err := cmd.Output()
	if err != nil {
		return out, commandError(err, out)
	}
	return out, nil
}

// gatewayPort is the dashboard's port on this host. Tests replace it.
var gatewayPort = func() string {
	if err, raw := service.MyService.Gateway().GetPort(); err == nil {
		if p := gjson.Get(raw, "data").String(); p != "" {
			return p
		}
	}
	return "80"
}

type tailscaleServeStatus struct {
	Running    bool   `json:"running"`     // logged in and connected
	Enabled    bool   `json:"enabled"`     // our 443 "/" handler is configured
	URL        string `json:"url"`         // https://<machine>.<tailnet>.ts.net
	CertsReady bool   `json:"certs_ready"` // tailnet has MagicDNS + HTTPS certificates on
	MagicDNS   bool   `json:"magic_dns"`
	Conflict   string `json:"conflict"` // why enabling would clobber the owner's config
	Target     string `json:"target"`   // what 443 proxies to when enabled
	AdminURL   string `json:"admin_url"`
}

type tsServeConfig struct {
	TCP map[string]struct {
		HTTPS      bool   `json:"HTTPS"`
		TCPForward string `json:"TCPForward"`
	} `json:"TCP"`
	Web map[string]struct {
		Handlers map[string]json.RawMessage `json:"Handlers"`
	} `json:"Web"`
	AllowFunnel map[string]bool `json:"AllowFunnel"`
}

func readTailscaleServe(ctx context.Context) (tailscaleServeStatus, error) {
	st := tailscaleServeStatus{Target: "http://127.0.0.1:" + gatewayPort(), AdminURL: tailscaleDNSAdminURL}
	// Status prints JSON even when stopped or logged out; no JSON at all
	// means no daemon (or no CLI) - a state, not an error.
	out, _ := tailscaleRun(ctx, "status", "--json")
	if !json.Valid(out) || len(out) == 0 {
		return st, nil
	}
	var s struct {
		BackendState string   `json:"BackendState"`
		CertDomains  []string `json:"CertDomains"`
		Self         struct {
			DNSName string `json:"DNSName"`
		} `json:"Self"`
		CurrentTailnet struct {
			MagicDNSEnabled bool `json:"MagicDNSEnabled"`
		} `json:"CurrentTailnet"`
	}
	if err := json.Unmarshal(out, &s); err != nil {
		return st, err
	}
	host := strings.TrimSuffix(s.Self.DNSName, ".")
	st.Running = s.BackendState == "Running" && host != ""
	st.MagicDNS = s.CurrentTailnet.MagicDNSEnabled
	for _, d := range s.CertDomains {
		if strings.EqualFold(d, host) {
			st.CertsReady = true
		}
	}
	if !st.Running {
		return st, nil
	}
	st.URL = "https://" + host

	out, err := tailscaleRun(ctx, "serve", "status", "--json")
	if err != nil {
		return st, err
	}
	var sc tsServeConfig
	if len(strings.TrimSpace(string(out))) > 0 {
		if err := json.Unmarshal(out, &sc); err != nil {
			return st, fmt.Errorf("reading serve config: %w", err)
		}
	}
	hp := host + ":443"
	if raw, ok := sc.Web[hp].Handlers["/"]; ok {
		if gjson.GetBytes(raw, "Proxy").String() == st.Target {
			st.Enabled = true
		} else {
			st.Conflict = "Port 443 on this machine already serves something else at / (see `tailscale serve status`)."
		}
	} else if tcp, ok := sc.TCP["443"]; ok && tcp.TCPForward != "" {
		st.Conflict = "Port 443 on this machine already forwards to " + tcp.TCPForward + " (see `tailscale serve status`)."
	}
	if sc.AllowFunnel[hp] {
		// Our handler on a funneled port would put the dashboard on the
		// public internet.
		st.Conflict = "Tailscale Funnel is on for port 443 - the dashboard would be public. Turn Funnel off first (`tailscale funnel status`)."
	}
	return st, nil
}

// GetTailscaleServe reports whether the dashboard is served over HTTPS on
// the tailnet and whether it can be.
func GetTailscaleServe(ctx *echo.Context) error {
	c, cancel := context.WithTimeout(ctx.Request().Context(), 10*time.Second)
	defer cancel()
	st, err := readTailscaleServe(c)
	if err != nil {
		return serviceError(ctx, err)
	}
	return ok(ctx, st)
}

// PutTailscaleServe turns the HTTPS address on (:state = "on") or off.
func PutTailscaleServe(ctx *echo.Context) error {
	state := ctx.Param("state")
	if state != "on" && state != "off" {
		return badParams(ctx, "state must be 'on' or 'off'")
	}
	if !requestIsAdmin(ctx) {
		return ctx.JSON(http.StatusForbidden, map[string]string{"message": "Changing Tailscale HTTPS needs an administrator account"})
	}
	// Enabling can wait on the tailnet to issue the first certificate.
	c, cancel := context.WithTimeout(ctx.Request().Context(), 45*time.Second)
	defer cancel()
	st, err := setTailscaleServe(c, state == "on")
	if err != nil {
		return serviceError(ctx, err)
	}
	return ok(ctx, st)
}

func setTailscaleServe(ctx context.Context, on bool) (tailscaleServeStatus, error) {
	st, err := readTailscaleServe(ctx)
	if err != nil {
		return st, err
	}
	if st.Enabled == on {
		return st, nil
	}
	if !on {
		// Removes only the "/" handler on 443 (ours, checked above); the
		// port goes away only if nothing else is mounted on it.
		if _, err := tailscaleRun(ctx, "serve", "--yes", "--https=443", "off"); err != nil {
			return st, err
		}
		return readTailscaleServe(ctx)
	}
	switch {
	case !st.Running:
		return st, fmt.Errorf("Tailscale isn't connected - turn it on first")
	case !st.CertsReady:
		return st, fmt.Errorf("HTTPS certificates aren't enabled for your tailnet. Turn on MagicDNS and HTTPS Certificates at %s, then try again", tailscaleDNSAdminURL)
	case st.Conflict != "":
		return st, fmt.Errorf("%s", st.Conflict)
	}
	out, err := tailscaleRun(ctx, "serve", "--bg", "--yes", "--https=443", st.Target)
	if err != nil {
		// The CLI waits (until our timeout) for Serve to be allowed on the
		// tailnet and prints the link that allows it.
		if link := tailscaleLoginURLPattern.FindString(string(out) + " " + err.Error()); link != "" {
			return st, fmt.Errorf("Serve isn't allowed on your tailnet yet. Allow it at %s, then try again", link)
		}
		if ctx.Err() != nil {
			return st, fmt.Errorf("tailscale serve didn't finish in time - try again in a moment")
		}
		return st, err
	}
	return readTailscaleServe(ctx)
}
