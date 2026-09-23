package v1

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	modelCommon "github.com/F-e-n-y-x/NivaroOS/services/common/model"
	"github.com/F-e-n-y-x/NivaroOS/services/core/service"
	"github.com/F-e-n-y-x/NivaroOS/services/core/service/aptjob"
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
	// ID identifies the package for apt: the name, or name:arch for a
	// foreign architecture (multi-arch systems list the same name twice).
	ID   string `json:"id"`
	Arch string `json:"arch"`
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
		"-f=${Package}\t${Version}\t${Installed-Size}\t${binary:Package}\t${Architecture}\t${binary:Summary}\n")
	if err != nil && out == "" {
		return serviceError(ctx, fmt.Errorf("dpkg-query failed: %w", err))
	}

	list := make([]installedPackage, 0)
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		fields := strings.SplitN(scanner.Text(), "\t", 6)
		if len(fields) != 6 {
			continue
		}
		name := fields[0]
		if q != "" && !strings.Contains(strings.ToLower(name), q) {
			continue
		}
		sizeKiB, _ := strconv.ParseInt(fields[2], 10, 64)
		list = append(list, installedPackage{
			ID:          fields[3],
			Arch:        fields[4],
			Name:        name,
			Version:     fields[1],
			Size:        sizeKiB * 1024,
			Description: fields[5],
		})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return ok(ctx, list)
}

type aptUpgradablePackage struct {
	ID               string `json:"id"` // name, or name:arch for a foreign architecture
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
	native := nativeArch()
	list := make([]aptUpgradablePackage, 0, len(pkgs))
	for _, p := range pkgs {
		id := p.Name
		if p.Arch != "" && p.Arch != "all" && p.Arch != native {
			id = p.Name + ":" + p.Arch
		}
		list = append(list, aptUpgradablePackage{
			ID:               id,
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

// aptJobStarted answers a job start: the job (poll GET /sys/apt/job), or
// 409 when another package operation is still running.
func aptJobStarted(ctx echo.Context, j aptjob.Job, err error) error {
	if errors.Is(err, aptjob.ErrBusy) {
		return ctx.JSON(http.StatusConflict, modelCommon.Result{Success: http.StatusConflict, Message: err.Error(), Data: j})
	}
	if err != nil {
		return badParams(ctx, err.Error())
	}
	return ok(ctx, j)
}

// PostAptInstall installs (or reinstalls) packages as a background job.
func PostAptInstall(ctx echo.Context) error {
	req := new(aptInstallReq)
	if err := ctx.Bind(req); err != nil {
		return badParams(ctx, "invalid request body")
	}
	j, err := service.AptJobs.Install(req.Packages, req.Reinstall)
	return aptJobStarted(ctx, j, err)
}

type aptUninstallReq struct {
	Packages []string `json:"packages"`
	Purge    bool     `json:"purge"`
}

// PostAptUninstall removes (or purges) packages as a background job.
func PostAptUninstall(ctx echo.Context) error {
	req := new(aptUninstallReq)
	if err := ctx.Bind(req); err != nil {
		return badParams(ctx, "invalid request body")
	}
	j, err := service.AptJobs.Remove(req.Packages, req.Purge)
	return aptJobStarted(ctx, j, err)
}

type aptUpgradeReq struct {
	Packages []string `json:"packages"`
}

// PostAptUpgrade upgrades the given packages, or everything (dist-upgrade -
// the same job the Updates section runs) when none are given.
func PostAptUpgrade(ctx echo.Context) error {
	req := new(aptUpgradeReq)
	if err := ctx.Bind(req); err != nil {
		return badParams(ctx, "invalid request body")
	}
	if len(req.Packages) == 0 {
		j, err := service.AptJobs.UpgradeAll()
		return aptJobStarted(ctx, j, err)
	}
	j, err := service.AptJobs.Upgrade(req.Packages)
	return aptJobStarted(ctx, j, err)
}

// PostAptUpdate refreshes the repository indexes as a background job.
func PostAptUpdate(ctx echo.Context) error {
	j, err := service.AptJobs.Update()
	return aptJobStarted(ctx, j, err)
}

// GetAptJob is the latest package operation with its log tail.
func GetAptJob(ctx echo.Context) error {
	return ok(ctx, service.AptJobs.Status())
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

// nativeArch is dpkg's own architecture (e.g. amd64).
func nativeArch() string {
	out, err := exec.Command("dpkg", "--print-architecture").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
