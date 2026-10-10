// hostdisplay_wayland.go: the Host Desktop resolution menu on a Wayland
// session. xrandr can't change a Wayland output, and each compositor has
// its own way: GNOME mutter's DisplayConfig D-Bus API, KDE's kscreen-doctor,
// wlroots' wlr-randr. All of them must run as the session user, which this
// (root, multi-threaded) service can't become, so it runs itself as
//
//	nivaroos-vm-sidecar host-display-wayland --uid N --backend B [--set WxH]
//
// which drops to that user and prints the display as HostDisplayInfo JSON.
// Only the output Host Desktop streams changes (GNOME: the primary monitor,
// wlroots: the first output, KDE: the highest-priority one), and only to a
// mode the monitor has - Wayland compositors don't take made-up modes.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

func init() {
	if len(os.Args) < 2 || os.Args[1] != "host-display-wayland" {
		return
	}
	os.Exit(runHostDisplayWayland(os.Args[2:]))
}

// waylandHostDisplay: the streamed Wayland session's display, after
// setting it to w x h when w > 0. ok is false when the session isn't
// Wayland (the caller then reports errNoHostDisplay as before).
func waylandHostDisplay(w, h int) (info HostDisplayInfo, ok bool, err error) {
	if readHostDesktopState()["SESSION_TYPE"] != "wayland" {
		return info, false, nil
	}
	sess := activeSeatSession()
	b, _ := os.ReadFile(hostDesktopWaylandStatusPath)
	backend := parseKeyValueLines(string(b))["BACKEND"]
	if sess.UID == "" || backend == "" {
		return info, false, nil
	}
	self, err := os.Executable()
	if err != nil {
		return info, true, err
	}
	args := []string{"host-display-wayland", "--uid", sess.UID, "--backend", backend}
	if w > 0 {
		args = append(args, "--set", fmt.Sprintf("%dx%d", w, h))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var stderr strings.Builder
	cmd := exec.CommandContext(ctx, self, args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return info, true, errors.New(msg)
		}
		return info, true, err
	}
	return info, true, json.Unmarshal(out, &info)
}

func runHostDisplayWayland(args []string) int {
	fs := flag.NewFlagSet("host-display-wayland", flag.ContinueOnError)
	uid := fs.String("uid", "", "session user's uid")
	backend := fs.String("backend", "", "mutter, wayvnc or portal (host-vnc-wayland's pick)")
	set := fs.String("set", "", "WxH to switch to")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	fail := func(err error) int {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var w, h int
	if *set != "" {
		if _, err := fmt.Sscanf(*set, "%dx%d", &w, &h); err != nil {
			return fail(fmt.Errorf("bad --set %q", *set))
		}
	}
	u, err := user.LookupId(*uid)
	if err != nil {
		return fail(err)
	}
	wl := findWaylandDisplay("/run/user/" + u.Uid)
	conn, err := becomeSessionUser(u, wl)
	if err != nil {
		return fail(err)
	}
	defer conn.Close()

	var d waylandDisplay
	switch *backend {
	case "mutter":
		d = mutterDisplay{conn.Object("org.gnome.Mutter.DisplayConfig", "/org/gnome/Mutter/DisplayConfig")}
	case "wayvnc":
		d = wlrDisplay{}
	default:
		if _, err := exec.LookPath("kscreen-doctor"); err != nil {
			return fail(errors.New("this desktop's resolution can't be changed from here - use its own display settings"))
		}
		d = kscreenDisplay{}
	}
	if w > 0 {
		cur, err := d.query()
		if err != nil {
			return fail(err)
		}
		m, ok := pickMode(cur.Resolutions, w, h)
		if !ok {
			return fail(errors.New("the display reports no modes"))
		}
		if fmt.Sprintf("%dx%d", m.Width, m.Height) != cur.Current {
			if err := d.set(m.Width, m.Height); err != nil {
				return fail(err)
			}
		}
	}
	info, err := d.query()
	if err != nil {
		return fail(err)
	}
	info.Display = wl
	_ = json.NewEncoder(os.Stdout).Encode(info)
	return 0
}

type waylandDisplay interface {
	query() (HostDisplayInfo, error)
	set(w, h int) error // w x h is one of query's Resolutions
}

// pickMode: w x h itself when the output has it, else the nearest.
func pickMode(modes []DisplayResolution, w, h int) (DisplayResolution, bool) {
	for _, m := range modes {
		if m.Width == w && m.Height == h {
			return m, true
		}
	}
	return nearestDisplayMode(modes, w, h)
}

// addMode appends w x h unless it's there already (outputs list a size once
// per refresh rate).
func addMode(list []DisplayResolution, w, h int) []DisplayResolution {
	for _, r := range list {
		if r.Width == w && r.Height == h {
			return list
		}
	}
	return append(list, DisplayResolution{Width: w, Height: h, Label: resolutionLabel(w, h)})
}

func displayInfo(output string, w, h int, modes []DisplayResolution) HostDisplayInfo {
	sort.SliceStable(modes, func(i, j int) bool { return modes[i].Width*modes[i].Height > modes[j].Width*modes[j].Height })
	return HostDisplayInfo{Current: fmt.Sprintf("%dx%d", w, h), Width: w, Height: h, Output: output, Resolutions: modes}
}

// ---------------------------------------------------------------------------
// GNOME: org.gnome.Mutter.DisplayConfig
// ---------------------------------------------------------------------------

type mutterMonSpec struct{ Connector, Vendor, Product, Serial string }

type mutterMode struct {
	ID             string
	W, H           int32
	Refresh        float64
	PreferredScale float64
	Scales         []float64
	Props          map[string]dbus.Variant
}

type mutterMonitor struct {
	Spec  mutterMonSpec
	Modes []mutterMode
	Props map[string]dbus.Variant
}

type mutterLogical struct {
	X, Y      int32
	Scale     float64
	Transform uint32
	Primary   bool
	Monitors  []mutterMonSpec
	Props     map[string]dbus.Variant
}

// ApplyMonitorsConfig's a(iiduba(ssa{sv})).
type mutterLogicalCfg struct {
	X, Y      int32
	Scale     float64
	Transform uint32
	Primary   bool
	Monitors  []mutterMonCfg
}

type mutterMonCfg struct {
	Connector, Mode string
	Props           map[string]dbus.Variant
}

type mutterDisplay struct{ obj dbus.BusObject }

func (m mutterDisplay) state() (uint32, []mutterMonitor, []mutterLogical, error) {
	var serial uint32
	var mons []mutterMonitor
	var logical []mutterLogical
	var props map[string]dbus.Variant
	err := m.obj.Call("org.gnome.Mutter.DisplayConfig.GetCurrentState", 0).Store(&serial, &mons, &logical, &props)
	return serial, mons, logical, err
}

func (m mutterDisplay) query() (HostDisplayInfo, error) {
	_, mons, logical, err := m.state()
	if err != nil {
		return HostDisplayInfo{}, fmt.Errorf("mutter DisplayConfig: %w", err)
	}
	return mutterInfo(mons, logical)
}

func (m mutterDisplay) set(w, h int) error {
	serial, mons, logical, err := m.state()
	if err != nil {
		return fmt.Errorf("mutter DisplayConfig: %w", err)
	}
	cfg, err := mutterPlan(mons, logical, w, h)
	if err != nil {
		return err
	}
	// Method 1 = temporary: applied now, not saved to monitors.xml and no
	// "Keep these display settings?" countdown on the host's screen.
	// ponytail: other monitors keep their positions; a layout that no
	// longer fits together is refused by mutter (the error says so).
	if err := m.obj.Call("org.gnome.Mutter.DisplayConfig.ApplyMonitorsConfig", 0, serial, uint32(1), cfg, map[string]dbus.Variant{}).Err; err != nil {
		return fmt.Errorf("mutter ApplyMonitorsConfig: %w", err)
	}
	return nil
}

func isCurrent(props map[string]dbus.Variant) bool {
	b, _ := props["is-current"].Value().(bool)
	return b
}

// mutterStreamed: the monitor RecordMonitor("") streams - the primary
// logical monitor's first monitor.
func mutterStreamed(mons []mutterMonitor, logical []mutterLogical) (*mutterMonitor, error) {
	var lm *mutterLogical
	for i := range logical {
		if logical[i].Primary || lm == nil {
			lm = &logical[i]
		}
		if logical[i].Primary {
			break
		}
	}
	if lm == nil || len(lm.Monitors) == 0 {
		return nil, errors.New("mutter reports no active monitor")
	}
	for i := range mons {
		if mons[i].Spec.Connector == lm.Monitors[0].Connector {
			return &mons[i], nil
		}
	}
	return nil, fmt.Errorf("mutter: monitor %s not found", lm.Monitors[0].Connector)
}

func mutterInfo(mons []mutterMonitor, logical []mutterLogical) (HostDisplayInfo, error) {
	mon, err := mutterStreamed(mons, logical)
	if err != nil {
		return HostDisplayInfo{}, err
	}
	var modes []DisplayResolution
	var w, h int
	for _, md := range mon.Modes {
		modes = addMode(modes, int(md.W), int(md.H))
		if isCurrent(md.Props) {
			w, h = int(md.W), int(md.H)
		}
	}
	return displayInfo(mon.Spec.Connector, w, h, modes), nil
}

// mutterPlan: the current layout with the streamed monitor switched to a
// w x h mode (the current one's refresh rate if it has it, else the
// highest); every other monitor keeps its current mode.
func mutterPlan(mons []mutterMonitor, logical []mutterLogical, w, h int) ([]mutterLogicalCfg, error) {
	target, err := mutterStreamed(mons, logical)
	if err != nil {
		return nil, err
	}
	var curRefresh float64
	for _, md := range target.Modes {
		if isCurrent(md.Props) {
			curRefresh = md.Refresh
		}
	}
	var pick *mutterMode
	for i := range target.Modes {
		md := &target.Modes[i]
		if int(md.W) != w || int(md.H) != h {
			continue
		}
		if pick == nil || refreshBetter(md.Refresh, pick.Refresh, curRefresh) {
			pick = md
		}
	}
	if pick == nil {
		return nil, fmt.Errorf("%s has no %dx%d mode", target.Spec.Connector, w, h)
	}
	currentMode := map[string]string{}
	for _, mon := range mons {
		for _, md := range mon.Modes {
			if isCurrent(md.Props) {
				currentMode[mon.Spec.Connector] = md.ID
			}
		}
	}
	currentMode[target.Spec.Connector] = pick.ID
	var cfg []mutterLogicalCfg
	for _, lm := range logical {
		c := mutterLogicalCfg{X: lm.X, Y: lm.Y, Scale: lm.Scale, Transform: lm.Transform, Primary: lm.Primary}
		for _, ms := range lm.Monitors {
			if ms.Connector == target.Spec.Connector && !scaleSupported(pick.Scales, lm.Scale) {
				c.Scale = pick.PreferredScale
			}
			c.Monitors = append(c.Monitors, mutterMonCfg{Connector: ms.Connector, Mode: currentMode[ms.Connector], Props: map[string]dbus.Variant{}})
		}
		cfg = append(cfg, c)
	}
	return cfg, nil
}

// refreshBetter: a is a better refresh rate than b - the current one (cur)
// first, else the highest.
func refreshBetter(a, b, cur float64) bool {
	near := func(x float64) bool { return x-cur < 0.01 && cur-x < 0.01 }
	if near(a) != near(b) {
		return near(a)
	}
	return a > b
}

func scaleSupported(scales []float64, s float64) bool {
	for _, x := range scales {
		if x-s < 1e-6 && s-x < 1e-6 {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// KDE Plasma: kscreen-doctor
// ---------------------------------------------------------------------------

type kscreenDisplay struct{}

type kscreenOutput struct {
	Name          string `json:"name"`
	Enabled       bool   `json:"enabled"`
	Connected     bool   `json:"connected"`
	Priority      int    `json:"priority"`
	CurrentModeID string `json:"currentModeId"`
	Modes         []struct {
		ID   string `json:"id"`
		Size struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"size"`
		RefreshRate float64 `json:"refreshRate"`
	} `json:"modes"`
}

// kscreenStreamed: the enabled output with the best (lowest, 1 = primary)
// priority.
func kscreenStreamed(js []byte) (*kscreenOutput, error) {
	var doc struct {
		Outputs []kscreenOutput `json:"outputs"`
	}
	if err := json.Unmarshal(js, &doc); err != nil {
		return nil, fmt.Errorf("kscreen-doctor -j: %w", err)
	}
	var best *kscreenOutput
	for i := range doc.Outputs {
		o := &doc.Outputs[i]
		if o.Enabled && o.Connected && (best == nil || (o.Priority > 0 && (best.Priority == 0 || o.Priority < best.Priority))) {
			best = o
		}
	}
	if best == nil {
		return nil, errors.New("KDE reports no enabled output")
	}
	return best, nil
}

func (kscreenDisplay) output() (*kscreenOutput, error) {
	out, err := exec.Command("kscreen-doctor", "-j").Output()
	if err != nil {
		return nil, fmt.Errorf("kscreen-doctor -j: %w", err)
	}
	return kscreenStreamed(out)
}

func (k kscreenDisplay) query() (HostDisplayInfo, error) {
	o, err := k.output()
	if err != nil {
		return HostDisplayInfo{}, err
	}
	var modes []DisplayResolution
	var w, h int
	for _, m := range o.Modes {
		modes = addMode(modes, m.Size.Width, m.Size.Height)
		if m.ID == o.CurrentModeID {
			w, h = m.Size.Width, m.Size.Height
		}
	}
	return displayInfo(o.Name, w, h, modes), nil
}

func (k kscreenDisplay) set(w, h int) error {
	o, err := k.output()
	if err != nil {
		return err
	}
	id, best := "", -1.0
	for _, m := range o.Modes {
		if m.Size.Width == w && m.Size.Height == h && m.RefreshRate > best {
			id, best = m.ID, m.RefreshRate
		}
	}
	if out, err := exec.Command("kscreen-doctor", "output."+o.Name+".mode."+id).CombinedOutput(); err != nil {
		return fmt.Errorf("kscreen-doctor: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// wlroots (sway, Hyprland, labwc...): wlr-randr
// ---------------------------------------------------------------------------

type wlrDisplay struct{}

type wlrOutput struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Modes   []struct {
		Width   int     `json:"width"`
		Height  int     `json:"height"`
		Refresh float64 `json:"refresh"`
		Current bool    `json:"current"`
	} `json:"modes"`
}

// wlrStreamed: the first enabled output - the one wayvnc picks by default.
func wlrStreamed(js []byte) (*wlrOutput, error) {
	var outs []wlrOutput
	if err := json.Unmarshal(js, &outs); err != nil {
		return nil, fmt.Errorf("wlr-randr --json: %w", err)
	}
	for i := range outs {
		if outs[i].Enabled {
			return &outs[i], nil
		}
	}
	return nil, errors.New("the compositor reports no enabled output")
}

func (wlrDisplay) output() (*wlrOutput, error) {
	out, err := exec.Command("wlr-randr", "--json").Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, errors.New("wlr-randr is not installed - reinstall Host Desktop from the dashboard to add it")
		}
		return nil, fmt.Errorf("wlr-randr --json: %w", err)
	}
	return wlrStreamed(out)
}

func (d wlrDisplay) query() (HostDisplayInfo, error) {
	o, err := d.output()
	if err != nil {
		return HostDisplayInfo{}, err
	}
	var modes []DisplayResolution
	var w, h int
	for _, m := range o.Modes {
		modes = addMode(modes, m.Width, m.Height)
		if m.Current {
			w, h = m.Width, m.Height
		}
	}
	return displayInfo(o.Name, w, h, modes), nil
}

func (d wlrDisplay) set(w, h int) error {
	o, err := d.output()
	if err != nil {
		return err
	}
	mode := strconv.Itoa(w) + "x" + strconv.Itoa(h)
	if out, err := exec.Command("wlr-randr", "--output", o.Name, "--mode", mode).CombinedOutput(); err != nil {
		return fmt.Errorf("wlr-randr: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return nil
}
