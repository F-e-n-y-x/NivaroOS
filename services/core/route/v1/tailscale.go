package v1

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// GetTailscaleInstalled lets the frontend show an "Install Tailscale"
// prompt instead of just failing when the other tailscale endpoints error
// out because the binary isn't there at all.
func GetTailscaleInstalled(ctx echo.Context) error {
	_, err := exec.LookPath("tailscale")
	return ok(ctx, map[string]bool{"installed": err == nil})
}

var tailscaleLoginURLPattern = regexp.MustCompile(`https://login\.tailscale\.com/\S+`)

// startTailscaleUpAndCaptureLoginURL runs `tailscale up` in the background
// (it blocks until the node is authenticated or the command is killed, so
// it must never be waited on synchronously from an HTTP handler) and scans
// its combined output for the login link it prints once, early on. The
// scanning goroutine keeps draining the pipe for the process's whole
// lifetime regardless of whether anyone is still waiting for the URL - an
// io.Pipe's writes block until read, so a scanner that stopped early would
// eventually stall `tailscale up` itself mid-authentication.
func startTailscaleUpAndCaptureLoginURL() (string, error) {
	r, w := io.Pipe()
	cmd := exec.Command("tailscale", "up")
	cmd.Stdout = w
	cmd.Stderr = w

	if err := cmd.Start(); err != nil {
		w.Close()
		return "", err
	}

	urlCh := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			if m := tailscaleLoginURLPattern.FindString(scanner.Text()); m != "" {
				select {
				case urlCh <- m:
				default:
				}
			}
		}
	}()
	go func() {
		_ = cmd.Wait()
		w.Close()
	}()

	select {
	case loginURL := <-urlCh:
		return loginURL, nil
	case <-time.After(8 * time.Second):
		return "", fmt.Errorf("timed out waiting for a login link - this device may already be authenticated, check its status")
	}
}

// PostTailscaleInstall installs the tailscale package via the official
// install script (the standard, distro-detecting way to get it - not worth
// reimplementing per-distro package names here) if it isn't already
// present, then starts `tailscale up` and returns the one-time login link
// it prints, so the frontend can show it immediately instead of the admin
// needing to open a terminal to find it.
func PostTailscaleInstall(ctx echo.Context) error {
	if _, err := exec.LookPath("tailscale"); err == nil {
		loginURL, upErr := startTailscaleUpAndCaptureLoginURL()
		return ok(ctx, map[string]string{"login_url": loginURL, "note": errString(upErr)})
	}

	installCmd := exec.Command("sh", "-c", "curl -fsSL https://tailscale.com/install.sh | sh")
	out, err := installCmd.CombinedOutput()
	if err != nil {
		return serviceError(ctx, fmt.Errorf("installing tailscale: %s", strings.TrimSpace(string(out))))
	}

	// The install script enables/starts tailscaled itself on systemd
	// distros, but do it explicitly too in case it didn't (e.g. a distro
	// the script doesn't recognize as systemd-based).
	_ = exec.Command("systemctl", "enable", "--now", "tailscaled").Run()

	loginURL, upErr := startTailscaleUpAndCaptureLoginURL()
	return ok(ctx, map[string]string{"login_url": loginURL, "note": errString(upErr)})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// GetTailscaleStatus shells `tailscale status --json` and passes its output
// straight through as the Data field - Tailscale's own JSON schema is a
// stable, documented external contract, not something worth re-modeling
// into Go structs just to re-serialize it back to JSON for the frontend.
func GetTailscaleStatus(ctx echo.Context) error {
	out, err := exec.Command("tailscale", "status", "--json").Output()
	if err != nil {
		return serviceError(ctx, err)
	}
	return ok(ctx, json.RawMessage(out))
}

// PutTailscaleState runs `tailscale up` or `tailscale down` depending on
// the :state path param - mirrors PutSystemState's :state-param shape
// (route/v1/system.go) used for restart/shutdown.
func PutTailscaleState(ctx echo.Context) error {
	state := ctx.Param("state")
	if state != "up" && state != "down" {
		return badParams(ctx, "state must be 'up' or 'down'")
	}
	if err := exec.Command("tailscale", state).Run(); err != nil {
		return serviceError(ctx, err)
	}
	return ok(ctx, state)
}

// rawTailscalePrefs mirrors only the fields of `tailscale debug prefs`
// (an unofficial/internal but functional way to read current preferences -
// there's no stable public equivalent) that are safe and useful to expose.
// Deliberately NOT the full output, which also carries private key material
// (PrivateNodeKey, NetworkLockKey) and account info (UserProfile/LoginName)
// that must never reach the frontend.
type rawTailscalePrefs struct {
	RouteAll               bool     `json:"RouteAll"`
	CorpDNS                bool     `json:"CorpDNS"`
	RunSSH                 bool     `json:"RunSSH"`
	ShieldsUp              bool     `json:"ShieldsUp"`
	ExitNodeAllowLANAccess bool     `json:"ExitNodeAllowLANAccess"`
	AdvertiseRoutes        []string `json:"AdvertiseRoutes"`
	Hostname               string   `json:"Hostname"`
}

type tailscalePrefs struct {
	AcceptRoutes           bool     `json:"accept_routes"`
	AcceptDNS              bool     `json:"accept_dns"`
	RunSSH                 bool     `json:"run_ssh"`
	ShieldsUp              bool     `json:"shields_up"`
	ExitNodeAllowLANAccess bool     `json:"exit_node_allow_lan_access"`
	AdvertiseRoutes        []string `json:"advertise_routes"`
	Hostname               string   `json:"hostname"`
}

// GetTailscalePrefs returns the subset of Tailscale's advanced preferences
// that are safe to show/edit from the web UI.
func GetTailscalePrefs(ctx echo.Context) error {
	out, err := exec.Command("tailscale", "debug", "prefs").Output()
	if err != nil {
		return serviceError(ctx, err)
	}

	var raw rawTailscalePrefs
	if err := json.Unmarshal(out, &raw); err != nil {
		return serviceError(ctx, err)
	}

	return ok(ctx, tailscalePrefs{
		AcceptRoutes:           raw.RouteAll,
		AcceptDNS:              raw.CorpDNS,
		RunSSH:                 raw.RunSSH,
		ShieldsUp:              raw.ShieldsUp,
		ExitNodeAllowLANAccess: raw.ExitNodeAllowLANAccess,
		AdvertiseRoutes:        raw.AdvertiseRoutes,
		Hostname:               raw.Hostname,
	})
}

// tailscalePrefsUpdate uses pointers so only the fields actually present in
// the request body are changed - mirrors `tailscale set`'s own philosophy
// ("only settings explicitly mentioned will be set"), and lets the frontend
// flip one switch at a time without needing to resend every other setting.
type tailscalePrefsUpdate struct {
	AcceptRoutes           *bool `json:"accept_routes"`
	AcceptDNS              *bool `json:"accept_dns"`
	RunSSH                 *bool `json:"run_ssh"`
	ShieldsUp              *bool `json:"shields_up"`
	ExitNodeAllowLANAccess *bool `json:"exit_node_allow_lan_access"`
}

// PutTailscalePrefs runs `tailscale set` with only the flags present in the
// request body - deliberately does not touch AdvertiseRoutes/exit-node
// setup, which this box may already have configured by hand and which
// `tailscale set --advertise-routes=...` would replace wholesale rather
// than merge.
func PutTailscalePrefs(ctx echo.Context) error {
	var body tailscalePrefsUpdate
	if err := ctx.Bind(&body); err != nil {
		return badParams(ctx, "invalid body")
	}

	args := []string{"set"}
	if body.AcceptRoutes != nil {
		args = append(args, fmt.Sprintf("--accept-routes=%t", *body.AcceptRoutes))
	}
	if body.AcceptDNS != nil {
		args = append(args, fmt.Sprintf("--accept-dns=%t", *body.AcceptDNS))
	}
	if body.RunSSH != nil {
		args = append(args, fmt.Sprintf("--ssh=%t", *body.RunSSH))
	}
	if body.ShieldsUp != nil {
		args = append(args, fmt.Sprintf("--shields-up=%t", *body.ShieldsUp))
	}
	if body.ExitNodeAllowLANAccess != nil {
		args = append(args, fmt.Sprintf("--exit-node-allow-lan-access=%t", *body.ExitNodeAllowLANAccess))
	}
	if len(args) == 1 {
		return badParams(ctx, "no preference specified")
	}

	if out, err := exec.Command("tailscale", args...).CombinedOutput(); err != nil {
		return serviceError(ctx, fmt.Errorf("%s", strings.TrimSpace(string(out))))
	}

	return ok(ctx, nil)
}
