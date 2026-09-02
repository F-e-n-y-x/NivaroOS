package v1

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// The UI's Package Manager (search/install/uninstall arbitrary APT packages,
// manage repository sources) - distinct from the simpler system-update
// checker in pkg_updates.go, which only tracks upgradable packages for the
// Updates section.

var validAptPackageName = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]*$`)

const aptSourcesDir = "/etc/apt/sources.list.d"
const aptSourcesFile = "/etc/apt/sources.list"

func validatePackageNames(names []string) error {
	if len(names) == 0 {
		return fmt.Errorf("no packages specified")
	}
	for _, n := range names {
		if !validAptPackageName.MatchString(n) {
			return fmt.Errorf("invalid package name: %q", n)
		}
	}
	return nil
}

// runAptCommand runs an apt/dpkg command with a bounded timeout, argv-only
// (never a shell string) so package names can never be interpreted as shell
// syntax, and "--" before any user-supplied names so they can never be
// misread as flags either.
func runAptCommand(timeout time.Duration, name string, args ...string) (string, error) {
	ctxTimeout, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctxTimeout, name, args...)
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive", "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

type aptPackageInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Installed   bool   `json:"installed"`
}

// GetAptSearch searches the local APT cache (apt-cache search, no network
// access needed) and marks which hits are already installed.
func GetAptSearch(ctx echo.Context) error {
	q := strings.TrimSpace(ctx.QueryParam("q"))
	if q == "" {
		return badParams(ctx, "missing search query")
	}
	if len(q) > 100 {
		return badParams(ctx, "search query too long")
	}

	out, err := runAptCommand(20*time.Second, "apt-cache", "search", "--names-only", "--", q)
	if err != nil && out == "" {
		return serviceError(ctx, fmt.Errorf("apt-cache search failed: %w", err))
	}

	installed := installedPackageSet()

	results := make([]aptPackageInfo, 0)
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, " - ", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if name == "" {
			continue
		}
		results = append(results, aptPackageInfo{
			Name:        name,
			Description: strings.TrimSpace(parts[1]),
			Installed:   installed[name],
		})
		if len(results) >= 100 {
			break
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })
	return ok(ctx, results)
}

// installedPackageSet returns the set of currently-installed package names.
func installedPackageSet() map[string]bool {
	set := map[string]bool{}
	out, err := runAptCommand(15*time.Second, "dpkg-query", "-W", "-f=${Package}\t${Status}\n")
	if err != nil && out == "" {
		return set
	}
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		fields := strings.SplitN(scanner.Text(), "\t", 2)
		if len(fields) != 2 {
			continue
		}
		if strings.Contains(fields[1], "install ok installed") {
			set[fields[0]] = true
		}
	}
	return set
}

type installedPackage struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Size        int64  `json:"size"` // bytes
	Description string `json:"description"`
}

// GetAptInstalled lists installed packages, optionally filtered by a
// case-insensitive substring match on the package name.
func GetAptInstalled(ctx echo.Context) error {
	q := strings.ToLower(strings.TrimSpace(ctx.QueryParam("q")))

	out, err := runAptCommand(15*time.Second, "dpkg-query", "-W",
		"-f=${Package}\t${Version}\t${Installed-Size}\t${binary:Summary}\n")
	if err != nil && out == "" {
		return serviceError(ctx, fmt.Errorf("dpkg-query failed: %w", err))
	}

	list := make([]installedPackage, 0)
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		fields := strings.SplitN(scanner.Text(), "\t", 4)
		if len(fields) != 4 {
			continue
		}
		name := fields[0]
		if q != "" && !strings.Contains(strings.ToLower(name), q) {
			continue
		}
		sizeKiB, _ := strconv.ParseInt(fields[2], 10, 64)
		list = append(list, installedPackage{
			Name:        name,
			Version:     fields[1],
			Size:        sizeKiB * 1024,
			Description: fields[3],
		})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return ok(ctx, list)
}

type aptUpgradablePackage struct {
	Name             string `json:"name"`
	CurrentVersion   string `json:"current_version"`
	CandidateVersion string `json:"candidate_version"`
	Arch             string `json:"arch"`
}

// GetAptUpgradable lists packages with a newer version available, for the
// Package Manager's own Upgrades tab (see pkg_updates.go's GetSystemPackageUpdates
// for the equivalent used by the Settings > Updates section).
func GetAptUpgradable(ctx echo.Context) error {
	pkgs, err := getUpgradablePackages()
	if err != nil {
		return serviceError(ctx, fmt.Errorf("failed to list upgradable packages: %w", err))
	}
	list := make([]aptUpgradablePackage, 0, len(pkgs))
	for _, p := range pkgs {
		list = append(list, aptUpgradablePackage{
			Name:             p.Name,
			CurrentVersion:   p.CurrentVersion,
			CandidateVersion: p.NewVersion,
			Arch:             p.Arch,
		})
	}
	return ok(ctx, list)
}

type aptInstallReq struct {
	Packages  []string `json:"packages"`
	Reinstall bool     `json:"reinstall"`
}

// PostAptInstall installs (or reinstalls) one or more packages.
func PostAptInstall(ctx echo.Context) error {
	req := new(aptInstallReq)
	if err := ctx.Bind(req); err != nil {
		return badParams(ctx, "invalid request body")
	}
	if err := validatePackageNames(req.Packages); err != nil {
		return badParams(ctx, err.Error())
	}
	args := []string{"install", "-y"}
	if req.Reinstall {
		args = append(args, "--reinstall")
	}
	args = append(args, "--")
	args = append(args, req.Packages...)
	out, err := runAptCommand(5*time.Minute, "apt-get", args...)
	if err != nil {
		return serviceError(ctx, fmt.Errorf("apt-get install failed: %s", lastLines(out, 20)))
	}
	return ok(ctx, map[string]string{"output": lastLines(out, 50)})
}

type aptUninstallReq struct {
	Packages []string `json:"packages"`
	Purge    bool     `json:"purge"`
}

// PostAptUninstall removes (or purges) one or more packages.
func PostAptUninstall(ctx echo.Context) error {
	req := new(aptUninstallReq)
	if err := ctx.Bind(req); err != nil {
		return badParams(ctx, "invalid request body")
	}
	if err := validatePackageNames(req.Packages); err != nil {
		return badParams(ctx, err.Error())
	}
	verb := "remove"
	if req.Purge {
		verb = "purge"
	}
	args := append([]string{verb, "-y", "--"}, req.Packages...)
	out, err := runAptCommand(5*time.Minute, "apt-get", args...)
	if err != nil {
		return serviceError(ctx, fmt.Errorf("apt-get %s failed: %s", verb, lastLines(out, 20)))
	}
	return ok(ctx, map[string]string{"output": lastLines(out, 50)})
}

type aptUpgradeReq struct {
	Packages []string `json:"packages"`
}

// PostAptUpgrade upgrades either specific packages, or (if none given) every
// upgradable package.
func PostAptUpgrade(ctx echo.Context) error {
	req := new(aptUpgradeReq)
	if err := ctx.Bind(req); err != nil {
		return badParams(ctx, "invalid request body")
	}
	var out string
	var err error
	if len(req.Packages) == 0 {
		out, err = runAptCommand(10*time.Minute, "apt-get", "upgrade", "-y")
	} else {
		if verr := validatePackageNames(req.Packages); verr != nil {
			return badParams(ctx, verr.Error())
		}
		args := append([]string{"install", "-y", "--only-upgrade", "--"}, req.Packages...)
		out, err = runAptCommand(5*time.Minute, "apt-get", args...)
	}
	if err != nil {
		return serviceError(ctx, fmt.Errorf("apt-get upgrade failed: %s", lastLines(out, 20)))
	}
	return ok(ctx, map[string]string{"output": lastLines(out, 50)})
}

// PostAptUpdate refreshes the package repository indexes.
func PostAptUpdate(ctx echo.Context) error {
	out, err := runAptCommand(2*time.Minute, "apt-get", "update")
	if err != nil {
		return serviceError(ctx, fmt.Errorf("apt-get update failed: %s", lastLines(out, 20)))
	}
	return ok(ctx, map[string]string{"output": lastLines(out, 50)})
}

type aptSourceEntry struct {
	File       string   `json:"file"`
	Line       int      `json:"line"`
	Type       string   `json:"type"` // "deb" or "deb-src"
	URI        string   `json:"uri"`
	Suite      string   `json:"suite"`
	Components []string `json:"components"`
}

func parseSourceLines(path string) []aptSourceEntry {
	entries := make([]aptSourceEntry, 0)
	f, err := os.Open(path)
	if err != nil {
		return entries
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		typ := fields[0]
		if typ != "deb" && typ != "deb-src" {
			continue
		}
		// Skip a leading "[options]" block (e.g. "[arch=amd64 signed-by=...]").
		idx := 1
		if strings.HasPrefix(fields[1], "[") {
			for idx < len(fields) && !strings.HasSuffix(fields[idx], "]") {
				idx++
			}
			idx++
		}
		if idx >= len(fields) {
			continue
		}
		uri := fields[idx]
		suite := ""
		if idx+1 < len(fields) {
			suite = fields[idx+1]
		}
		components := []string{}
		if idx+2 < len(fields) {
			components = fields[idx+2:]
		}
		entries = append(entries, aptSourceEntry{
			File:       path,
			Line:       lineNum,
			Type:       typ,
			URI:        uri,
			Suite:      suite,
			Components: components,
		})
	}
	return entries
}

// GetAptSources lists every deb/deb-src line across /etc/apt/sources.list and
// /etc/apt/sources.list.d/*.list.
func GetAptSources(ctx echo.Context) error {
	entries := parseSourceLines(aptSourcesFile)
	matches, _ := filepath.Glob(filepath.Join(aptSourcesDir, "*.list"))
	sort.Strings(matches)
	for _, m := range matches {
		entries = append(entries, parseSourceLines(m)...)
	}
	return ok(ctx, entries)
}

// resolveSourceFile maps a user-supplied file name/path to a real, safe path
// this endpoint is allowed to write to - either the main sources.list, or a
// plain filename (no path separators) inside sources.list.d, so a request
// can never escape that directory.
func resolveSourceFile(file string) (string, error) {
	if file == "" || file == filepath.Base(aptSourcesFile) || file == aptSourcesFile {
		return aptSourcesFile, nil
	}
	base := filepath.Base(file)
	if base != file || base == "." || base == ".." {
		return "", fmt.Errorf("invalid source file name")
	}
	if !strings.HasSuffix(base, ".list") {
		base += ".list"
	}
	return filepath.Join(aptSourcesDir, base), nil
}

type aptAddSourceReq struct {
	Source string `json:"source"`
	File   string `json:"file"`
}

var validSourceLine = regexp.MustCompile(`^deb(-src)?\s+(\[[^\]]*\]\s+)?\S+\s+\S+(\s+\S+)*$`)

// PostAptSources appends a new "deb ..." / "deb-src ..." line to a file under
// /etc/apt/sources.list.d/ (creating it if needed).
func PostAptSources(ctx echo.Context) error {
	req := new(aptAddSourceReq)
	if err := ctx.Bind(req); err != nil {
		return badParams(ctx, "invalid request body")
	}
	line := strings.TrimSpace(req.Source)
	if !validSourceLine.MatchString(line) {
		return badParams(ctx, "source must look like: deb [options] <uri> <suite> [components...]")
	}
	path, err := resolveSourceFile(req.File)
	if err != nil {
		return badParams(ctx, err.Error())
	}
	if err := os.MkdirAll(aptSourcesDir, 0755); err != nil {
		return serviceError(ctx, err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return serviceError(ctx, err)
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		return serviceError(ctx, err)
	}
	return ok(ctx, nil)
}

type aptDeleteSourceReq struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

// DeleteAptSources removes a single line (by 1-based line number) from a
// sources file.
func DeleteAptSources(ctx echo.Context) error {
	req := new(aptDeleteSourceReq)
	if err := ctx.Bind(req); err != nil {
		return badParams(ctx, "invalid request body")
	}
	path, err := resolveSourceFile(req.File)
	if err != nil {
		return badParams(ctx, err.Error())
	}
	if req.Line < 1 {
		return badParams(ctx, "invalid line number")
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		return serviceError(ctx, err)
	}
	lines := strings.Split(string(contents), "\n")
	if req.Line > len(lines) {
		return badParams(ctx, "line number out of range")
	}
	lines = append(lines[:req.Line-1], lines[req.Line:]...)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		return serviceError(ctx, err)
	}
	return ok(ctx, nil)
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
