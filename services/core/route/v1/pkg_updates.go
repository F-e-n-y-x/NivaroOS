package v1

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	modelCommon "github.com/F-e-n-y-x/NivaroOS/services/common/model"
	"github.com/F-e-n-y-x/NivaroOS/services/core/service"
	"github.com/F-e-n-y-x/NivaroOS/services/core/service/aptjob"
	"github.com/labstack/echo/v4"
)

type PkgUpdateInfo struct {
	Name           string `json:"name"`
	Suite          string `json:"suite"`
	NewVersion     string `json:"new_version"`
	CurrentVersion string `json:"current_version"`
	Arch           string `json:"arch"`
	IsSecurity     bool   `json:"is_security"`
}

type PkgCheckResult struct {
	Count         int             `json:"count"`
	SecurityCount int             `json:"security_count"`
	Packages      []PkgUpdateInfo `json:"packages"`
	LastChecked   string          `json:"last_checked"`
}

var (
	aptLineRegex   = regexp.MustCompile(`^([^/\s]+)/([^\s]+)\s+([^\s]+)\s+([^\s]+)\s+\[upgradable from:\s+([^\]]+)\]`)
	pkgMu          sync.Mutex
	pkgLastChecked string
)

func parseUpgradableList(raw string) []PkgUpdateInfo {
	list := make([]PkgUpdateInfo, 0)
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		matches := aptLineRegex.FindStringSubmatch(line)
		if len(matches) == 6 {
			suite := matches[2]
			isSec := strings.Contains(strings.ToLower(suite), "security") || strings.Contains(strings.ToLower(matches[1]), "security")
			list = append(list, PkgUpdateInfo{
				Name:           matches[1],
				Suite:          suite,
				NewVersion:     matches[3],
				Arch:           matches[4],
				CurrentVersion: matches[5],
				IsSecurity:     isSec,
			})
		}
	}
	return list
}

func getUpgradablePackages() ([]PkgUpdateInfo, error) {
	cmd := exec.Command("apt", "list", "--upgradable")
	cmd.Env = append(os.Environ(), "LC_ALL=C", "DEBIAN_FRONTEND=noninteractive")
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return nil, err
	}
	return parseUpgradableList(string(out)), nil
}

// GetSystemPackageUpdates returns current list of upgradable apt packages
func GetSystemPackageUpdates(ctx echo.Context) error {
	pkgs, err := getUpgradablePackages()
	if err != nil {
		return serviceError(ctx, fmt.Errorf("failed to list upgradable packages: %w", err))
	}
	secCount := 0
	for _, p := range pkgs {
		if p.IsSecurity {
			secCount++
		}
	}
	pkgMu.Lock()
	if pkgLastChecked == "" {
		pkgLastChecked = time.Now().Format(time.RFC3339)
	}
	checked := pkgLastChecked
	pkgMu.Unlock()
	return ok(ctx, PkgCheckResult{
		Count:         len(pkgs),
		SecurityCount: secCount,
		Packages:      pkgs,
		LastChecked:   checked,
	})
}

// PostRefreshPackageUpdates runs `apt-get update` then returns the fresh
// list. A failed update (offline, broken repo) is reported, not turned into
// "everything is up to date". Bounded so the request answers before the
// UI's 60 s limit; an interrupted `apt-get update` is harmless.
func PostRefreshPackageUpdates(ctx echo.Context) error {
	if j := service.AptJobs.Status(); j.State == aptjob.StateRunning {
		return ctx.JSON(http.StatusConflict, modelCommon.Result{Success: http.StatusConflict, Message: aptjob.ErrBusy.Error()})
	}
	c, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	cmd := exec.CommandContext(c, "apt-get", "update")
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive", "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	if c.Err() == context.DeadlineExceeded {
		return serviceError(ctx, fmt.Errorf("checking the repositories took too long - is the server online?"))
	}
	if err != nil {
		return serviceError(ctx, fmt.Errorf("apt-get update failed: %s", lastLines(string(out), 8)))
	}

	pkgMu.Lock()
	pkgLastChecked = time.Now().Format(time.RFC3339)
	checked := pkgLastChecked
	pkgMu.Unlock()

	pkgs, err := getUpgradablePackages()
	if err != nil {
		return serviceError(ctx, fmt.Errorf("failed to list upgradable packages: %w", err))
	}
	secCount := 0
	for _, p := range pkgs {
		if p.IsSecurity {
			secCount++
		}
	}
	return ok(ctx, PkgCheckResult{
		Count:         len(pkgs),
		SecurityCount: secCount,
		Packages:      pkgs,
		LastChecked:   checked,
	})
}

// PostSystemPackageUpgrade starts the dist-upgrade job (the same job
// Package Manager's "Upgrade all" runs).
func PostSystemPackageUpgrade(ctx echo.Context) error {
	j, err := service.AptJobs.UpgradeAll()
	if errors.Is(err, aptjob.ErrBusy) {
		return badParams(ctx, "a package operation is already running")
	}
	if err != nil {
		return serviceError(ctx, err)
	}
	return ok(ctx, map[string]interface{}{"status": "started", "started_at": j.StartedAt.Format(time.RFC3339), "job": j})
}

// GetSystemPackageUpgradeStatus reports the latest package job in the shape
// the updater window reads.
func GetSystemPackageUpgradeStatus(ctx echo.Context) error {
	j := service.AptJobs.Status()
	res := map[string]interface{}{
		"running":     j.State == aptjob.StateRunning,
		"exit_code":   j.ExitCode,
		"logs":        j.Logs,
		"started_at":  "",
		"finished_at": "",
		"job":         j,
	}
	if j.ID != "" {
		res["started_at"] = j.StartedAt.Format(time.RFC3339)
	}
	if j.FinishedAt != nil {
		res["finished_at"] = j.FinishedAt.Format(time.RFC3339)
	}
	return ok(ctx, res)
}
