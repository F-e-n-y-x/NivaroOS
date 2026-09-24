package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"libvirt.org/go/libvirt"
)

// NivaroOS Guest Tools: one CD for every guest OS, built on this server
// from the upstream virtio-win ISO (all Windows VirtIO drivers + the QEMU
// guest agent), the WinFsp MSI (needed by VirtIO-FS on Windows), the SPICE
// agent MSI (console copy/paste on Windows, over the VM's qemu-vdagent
// channel) and the setup scripts in guesttools/ that ship with this
// service.
//
// The ISO used to be a hand-remastered virtio-win.iso that lived only on
// one box: its Windows script installed a winfsp.msi that wasn't on the
// disc (so the shared folder service could never start), and Linux had no
// setup at all.
//
// Setup is as automatic as the guest allows: once the QEMU guest agent is
// running inside the VM (the tools install it), /guest-tools/auto-setup
// runs the right script in the guest itself - the user clicks one button.
// Before that, the user runs one script from the inserted CD.

//go:generate env GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H windowsgui" -o guesttools/windows/NivaroOS-Setup.exe ./guesttools/windows/launcher

//go:embed guesttools
var guestToolsFS embed.FS

// Bump when anything in guesttools/ changes, so existing ISOs get rebuilt.
const guestToolsVersion = "5"

const (
	guestToolsISOName = "nivaroos-guest-tools.iso"
	guestToolsVolID   = "NIVAROOS_TOOLS"
	virtioWinURL      = "https://fedorapeople.org/groups/virt/virtio-win/direct-downloads/stable-virtio/virtio-win.iso"
	winfspURL         = "https://github.com/winfsp/winfsp/releases/download/v2.1/winfsp-2.1.25156.msi"
	winfspSize        = 2191360
	// The SPICE Windows guest agent (vdagent + vdservice) - the current
	// stable release, pinned by the sha256 spice-space.org publishes
	// alongside it (vdagent-win-0.10.0/sha256sum).
	spiceVdagentURL    = "https://www.spice-space.org/download/windows/vdagent/vdagent-win-0.10.0/spice-vdagent-x64-0.10.0.msi"
	spiceVdagentSize   = 1988608
	spiceVdagentSHA256 = "77629435705bc27dd7d2525e9d2084f72dbab5fdbf310e812f91332fe18d00eb"
)

type guestToolsState struct {
	Present  bool   `json:"present"`
	Current  bool   `json:"current"` // built from this service's scripts
	Version  string `json:"version,omitempty"`
	Building bool   `json:"building"`
	Step     string `json:"step,omitempty"`
	Error    string `json:"error,omitempty"`
	Path     string `json:"path,omitempty"`
}

var (
	gtMu    sync.Mutex
	gtState guestToolsState
)

func guestToolsPath() string { return filepath.Join(defaultISODir, guestToolsISOName) }

// guestToolsStatus reports whether an up-to-date ISO exists. The version
// is kept in a small sidecar file next to the ISO (reading it from inside
// the ISO would need a mount).
func guestToolsStatus() guestToolsState {
	gtMu.Lock()
	st := gtState
	gtMu.Unlock()
	st.Path = guestToolsPath()
	if _, err := os.Stat(st.Path); err == nil {
		st.Present = true
		if v, err := os.ReadFile(st.Path + ".version"); err == nil {
			st.Version = strings.TrimSpace(string(v))
			st.Current = st.Version == guestToolsVersion
		}
	}
	return st
}

func setGTStep(step string) {
	gtMu.Lock()
	gtState.Step = step
	gtMu.Unlock()
}

// startGuestToolsBuild builds the ISO in the background (the virtio-win
// download alone is ~700 MB). Returns false if a build is already running.
func startGuestToolsBuild() bool {
	gtMu.Lock()
	if gtState.Building {
		gtMu.Unlock()
		return false
	}
	gtState = guestToolsState{Building: true, Step: "starting"}
	gtMu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
		defer cancel()
		err := buildGuestToolsISO(ctx)
		gtMu.Lock()
		gtState.Building, gtState.Step = false, ""
		if err != nil {
			gtState.Error = err.Error()
			log.Printf("guest tools build failed: %v", err)
		}
		gtMu.Unlock()
	}()
	return true
}

// isVirtioWinISO reports whether path is an upstream-style virtio-win ISO
// (it has the all-in-one guest tools installer at its root).
func isVirtioWinISO(ctx context.Context, path string) bool {
	out, err := exec.CommandContext(ctx, "xorriso", "-indev", path, "-find", "/virtio-win-guest-tools.exe").CombinedOutput()
	return err == nil && strings.Contains(string(out), "virtio-win-guest-tools.exe")
}

func buildGuestToolsISO(ctx context.Context) error {
	if _, err := exec.LookPath("xorriso"); err != nil {
		// Installs from before xorriso was part of setup: install it now
		// rather than asking the user to.
		setGTStep("installing xorriso")
		if installErr := installPackage(ctx, "xorriso"); installErr != nil {
			return fmt.Errorf("xorriso is needed to build the Guest Tools disc and could not be installed: %v", installErr)
		}
	}
	cache := filepath.Join(defaultISODir, ".cache")
	if err := os.MkdirAll(cache, 0755); err != nil {
		return err
	}

	// 1. virtio-win: reuse one already on the server, else download.
	setGTStep("virtio-win")
	base := ""
	for _, p := range []string{filepath.Join(cache, "virtio-win.iso"), filepath.Join(defaultISODir, "virtio-win.iso")} {
		if isVirtioWinISO(ctx, p) {
			base = p
			break
		}
	}
	if base == "" {
		base = filepath.Join(cache, "virtio-win.iso")
		if err := downloadFile(ctx, virtioWinURL, base, 0); err != nil {
			return fmt.Errorf("downloading virtio-win drivers: %w", err)
		}
		if !isVirtioWinISO(ctx, base) {
			return errors.New("the downloaded virtio-win.iso doesn't look like a virtio-win driver disc")
		}
	}

	// 2. WinFsp MSI.
	setGTStep("winfsp")
	msi := filepath.Join(cache, "winfsp.msi")
	if fi, err := os.Stat(msi); err != nil || fi.Size() != winfspSize {
		if err := downloadFile(ctx, winfspURL, msi, winfspSize); err != nil {
			return fmt.Errorf("downloading WinFsp: %w", err)
		}
	}

	// 3. SPICE agent MSI. Optional: without it the disc still builds (and
	// the setup skips that step) - only Windows copy/paste is missing.
	setGTStep("spice-vdagent")
	vdagent := filepath.Join(cache, "spice-vdagent-x64.msi")
	if err := ensureVerifiedDownload(ctx, spiceVdagentURL, vdagent, spiceVdagentSize, spiceVdagentSHA256); err != nil {
		log.Printf("guest tools: SPICE agent unavailable, building the disc without it: %v", err)
		vdagent = ""
	}

	// 4. Scripts from guesttools/ (Windows gets CRLF line endings).
	setGTStep("scripts")
	stage, err := os.MkdirTemp(cache, "stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	files := map[string]string{
		"guesttools/windows/NivaroOS-Guest-Tools-Setup.bat": "NivaroOS-Guest-Tools-Setup.bat",
		"guesttools/linux/nivaroos-guest-setup.sh":          "linux/nivaroos-guest-setup.sh",
		"guesttools/README.txt":                             "README-NivaroOS.txt",
	}
	for src, dst := range files {
		b, err := guestToolsFS.ReadFile(src)
		if err != nil {
			return err
		}
		if strings.HasSuffix(dst, ".bat") || strings.HasSuffix(dst, ".txt") {
			b = []byte(strings.ReplaceAll(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n", "\r\n"))
		}
		out := filepath.Join(stage, dst)
		if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(out, b, 0755); err != nil {
			return err
		}
	}
	// Double-clicking the CD runs the setup through NivaroOS-Setup.exe:
	// AutoRun can only launch an .exe (open= pointing at the .bat failed
	// with "This file does not have an app associated").
	launcher, err := guestToolsFS.ReadFile("guesttools/windows/NivaroOS-Setup.exe")
	if err != nil {
		return errors.New("NivaroOS-Setup.exe is missing from this build (run go generate in services/vm-sidecar)")
	}
	if err := os.WriteFile(filepath.Join(stage, "NivaroOS-Setup.exe"), launcher, 0755); err != nil {
		return err
	}
	autorun := "[AutoRun]\r\nopen=NivaroOS-Setup.exe\r\nicon=NivaroOS-Setup.exe\r\nlabel=NivaroOS Guest Tools\r\naction=Install NivaroOS Guest Tools\r\n"
	if err := os.WriteFile(filepath.Join(stage, "autorun.inf"), []byte(autorun), 0644); err != nil {
		return err
	}

	// 5. New ISO = virtio-win + our files, written next to the final path
	// and renamed into place, so a VM never sees a half-written disc.
	setGTStep("iso")
	final := guestToolsPath()
	tmp := filepath.Join(cache, "guest-tools.iso.part")
	_ = os.Remove(tmp)
	// Joliet is what Windows reads long file names from: without it (the
	// first build) Windows saw only 8.3 names and the setup script
	// reported virtio-win-guest-tools.exe as missing.
	args := []string{"-indev", base, "-outdev", tmp, "-volid", guestToolsVolID, "-boot_image", "any", "keep",
		"-joliet", "on", "-compliance", "joliet_long_names:joliet_long_paths",
		"-map", msi, "/winfsp.msi",
		"-map", filepath.Join(stage, "autorun.inf"), "/autorun.inf",
		"-map", filepath.Join(stage, "NivaroOS-Setup.exe"), "/NivaroOS-Setup.exe",
		"-map", filepath.Join(stage, "README-NivaroOS.txt"), "/README-NivaroOS.txt",
		"-map", filepath.Join(stage, "NivaroOS-Guest-Tools-Setup.bat"), "/NivaroOS-Guest-Tools-Setup.bat",
		"-map", filepath.Join(stage, "linux"), "/linux",
	}
	if vdagent != "" {
		args = append(args, "-map", vdagent, "/spice-vdagent-x64.msi")
	}
	if out, err := exec.CommandContext(ctx, "xorriso", args...).CombinedOutput(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("xorriso: %v: %s", err, lastLines(string(out), 5))
	}
	if err := os.Rename(tmp, final); err != nil {
		return err
	}
	return os.WriteFile(final+".version", []byte(guestToolsVersion+"\n"), 0644)
}

// installPackage installs one package with whichever package manager the
// server has.
func installPackage(ctx context.Context, pkg string) error {
	managers := [][]string{
		{"apt-get", "install", "-y", pkg},
		{"dnf", "install", "-y", pkg},
		{"zypper", "--non-interactive", "install", pkg},
		{"apk", "add", pkg},
	}
	if pkg == "xorriso" {
		managers = append(managers, []string{"pacman", "-S", "--noconfirm", "libisoburn"})
	}
	for _, m := range managers {
		if _, err := exec.LookPath(m[0]); err != nil {
			continue
		}
		out, err := exec.CommandContext(ctx, m[0], m[1:]...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s: %v: %s", strings.Join(m, " "), err, lastLines(string(out), 3))
		}
		return nil
	}
	return errors.New("no supported package manager found")
}

// downloadFile fetches url to dst via a .part file; wantSize > 0 is checked.
func downloadFile(ctx context.Context, url, dst string, wantSize int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	part := dst + ".part"
	f, err := os.Create(part)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && wantSize > 0 && n != wantSize {
		err = fmt.Errorf("%s: got %d bytes, expected %d", url, n, wantSize)
	}
	if err != nil {
		_ = os.Remove(part)
		return err
	}
	return os.Rename(part, dst)
}

// ensureVerifiedDownload makes dst a copy of url with exactly wantSize
// bytes and the given sha256 - reusing a cached copy that already
// matches, otherwise downloading it again.
func ensureVerifiedDownload(ctx context.Context, url, dst string, wantSize int64, wantSHA256 string) error {
	if fileSHA256(dst) == wantSHA256 {
		return nil
	}
	if err := downloadFile(ctx, url, dst, wantSize); err != nil {
		return err
	}
	if got := fileSHA256(dst); got != wantSHA256 {
		_ = os.Remove(dst)
		return fmt.Errorf("%s: sha256 %s, expected %s", url, got, wantSHA256)
	}
	return nil
}

// fileSHA256 is path's hex sha256, or "" if it can't be read.
func fileSHA256(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

// ---- running the setup inside the guest (QEMU guest agent) ----

type agentReply struct {
	Return json.RawMessage `json:"return"`
}

func agentCommand(dom *libvirt.Domain, cmd string, args interface{}) (json.RawMessage, error) {
	body := map[string]interface{}{"execute": cmd}
	if args != nil {
		body["arguments"] = args
	}
	b, _ := json.Marshal(body)
	out, err := dom.QemuAgentCommand(string(b), libvirt.DOMAIN_QEMU_AGENT_COMMAND_DEFAULT, 0)
	if err != nil {
		return nil, err
	}
	var r agentReply
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		return nil, err
	}
	return r.Return, nil
}

// errNoAgent: the VM doesn't run the QEMU guest agent (yet) - the UI then
// shows the one manual step instead.
var errNoAgent = errors.New("the QEMU guest agent isn't running in this VM")

// guestExec runs path+args in the guest and waits for it (up to timeout),
// returning exit code and combined output.
func guestExec(dom *libvirt.Domain, path string, args []string, timeout time.Duration) (int, string, error) {
	ret, err := agentCommand(dom, "guest-exec", map[string]interface{}{"path": path, "arg": args, "capture-output": true})
	if err != nil {
		return 0, "", err
	}
	var started struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(ret, &started); err != nil {
		return 0, "", err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(time.Second)
		st, err := agentCommand(dom, "guest-exec-status", map[string]int{"pid": started.PID})
		if err != nil {
			return 0, "", err
		}
		var s struct {
			Exited   bool   `json:"exited"`
			ExitCode int    `json:"exitcode"`
			OutData  string `json:"out-data"`
			ErrData  string `json:"err-data"`
		}
		if err := json.Unmarshal(st, &s); err != nil {
			return 0, "", err
		}
		if s.Exited {
			o, _ := base64.StdEncoding.DecodeString(s.OutData)
			e, _ := base64.StdEncoding.DecodeString(s.ErrData)
			return s.ExitCode, strings.TrimSpace(string(o) + "\n" + string(e)), nil
		}
	}
	return 0, "", errors.New("the setup is still running inside the VM - check it there")
}

// runGuestSetup detects the guest OS through the agent and runs the
// matching script from the inserted Guest Tools CD.
func runGuestSetup(dom *libvirt.Domain) (string, int, string, error) {
	if _, err := agentCommand(dom, "guest-ping", nil); err != nil {
		return "", 0, "", errNoAgent
	}
	info, err := agentCommand(dom, "guest-get-osinfo", nil)
	if err != nil {
		return "", 0, "", err
	}
	var osInfo struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(info, &osInfo)
	if osInfo.ID == "mswindows" {
		// Find the CD by its volume label, then run the script unattended.
		ps := `$d=(Get-Volume | Where-Object FileSystemLabel -eq '` + guestToolsVolID + `' | Select-Object -First 1).DriveLetter; ` +
			`if(-not $d){Write-Output 'NivaroOS Guest Tools CD not found'; exit 2}; ` +
			`& cmd.exe /c ($d + ':\NivaroOS-Guest-Tools-Setup.bat') /quiet; exit $LASTEXITCODE`
		code, out, err := guestExec(dom, "powershell.exe", []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", ps}, 10*time.Minute)
		return "windows", code, out, err
	}
	// Linux: mount the CD by label (desktops may already have), run the script.
	sh := `set -e; M=$(findmnt -rn -S LABEL=` + guestToolsVolID + ` -o TARGET | head -n1); ` +
		`if [ -z "$M" ]; then M=/run/nivaroos-tools; mkdir -p $M; mount -o ro -L ` + guestToolsVolID + ` $M; fi; ` +
		`sh "$M/linux/nivaroos-guest-setup.sh"`
	code, out, err := guestExec(dom, "/bin/sh", []string{"-c", sh}, 10*time.Minute)
	return "linux", code, out, err
}

func RegisterGuestToolsRoutes(mux *http.ServeMux, store *LibvirtStore) {
	mux.HandleFunc("GET /guest-tools", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, guestToolsStatus())
	})
	mux.HandleFunc("POST /guest-tools/build", func(w http.ResponseWriter, r *http.Request) {
		startGuestToolsBuild()
		writeJSON(w, http.StatusAccepted, guestToolsStatus())
	})

	// Insert the (current) Guest Tools CD; 409 while it still has to be built.
	handleVM(mux, "POST /vms/{name}/guest-tools/insert", func(w http.ResponseWriter, r *http.Request, name string) {
		st := guestToolsStatus()
		if !st.Current {
			startGuestToolsBuild()
			writeJSON(w, http.StatusConflict, guestToolsStatus())
			return
		}
		if err := store.InsertCDROM(name, st.Path); err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, st)
	})

	// Run the setup inside the guest through the QEMU guest agent.
	handleVM(mux, "POST /vms/{name}/guest-tools/auto-setup", func(w http.ResponseWriter, r *http.Request, name string) {
		dom, err := store.lookup(name)
		if err != nil {
			writeStoreError(w, http.StatusNotFound, err)
			return
		}
		defer dom.Free()
		osName, code, out, err := runGuestSetup(dom)
		if errors.Is(err, errNoAgent) {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": err.Error(), "agent": false})
			return
		}
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"os": osName, "exit_code": code, "ok": code == 0, "output": lastLines(out, 40)})
	})
}
