package v1

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

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
	pkgIsRunning   bool
	pkgLogs        []string
	pkgExitCode    int
	pkgStartedAt   string
	pkgFinishedAt  string
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
	if pkgLastChecked == "" {
		pkgLastChecked = time.Now().Format(time.RFC3339)
	}
	return ok(ctx, PkgCheckResult{
		Count:         len(pkgs),
		SecurityCount: secCount,
		Packages:      pkgs,
		LastChecked:   pkgLastChecked,
	})
}

// PostRefreshPackageUpdates runs `apt-get update` then returns fresh package list
func PostRefreshPackageUpdates(ctx echo.Context) error {
	cmd := exec.Command("apt-get", "update")
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive", "LC_ALL=C")
	_ = cmd.Run() // run even if some third-party repo warns

	pkgLastChecked = time.Now().Format(time.RFC3339)

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
		LastChecked:   pkgLastChecked,
	})
}

// PostSystemPackageUpgrade executes apt-get dist-upgrade in the background and tracks logs
func PostSystemPackageUpgrade(ctx echo.Context) error {
	pkgMu.Lock()
	if pkgIsRunning {
		pkgMu.Unlock()
		return badParams(ctx, "package upgrade is already running")
	}
	pkgIsRunning = true
	pkgLogs = []string{fmt.Sprintf("[%s] Starting Debian / Linux system package upgrade...", time.Now().Format("15:04:05"))}
	pkgExitCode = 0
	pkgStartedAt = time.Now().Format(time.RFC3339)
	pkgFinishedAt = ""
	pkgMu.Unlock()

	go func() {
		logFile, err := os.OpenFile("/var/log/recasa-apt-upgrade.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			logFile, _ = os.OpenFile("/tmp/recasa-apt-upgrade.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		}
		if logFile != nil {
			defer logFile.Close()
		}

		cmd := exec.Command("apt-get", "dist-upgrade", "-y", "-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold")
		cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive", "LC_ALL=C", "NEEDRESTART_MODE=a")

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			pkgMu.Lock()
			pkgIsRunning = false
			pkgExitCode = 1
			pkgLogs = append(pkgLogs, fmt.Sprintf("Error creating stdout pipe: %v", err))
			pkgFinishedAt = time.Now().Format(time.RFC3339)
			pkgMu.Unlock()
			return
		}
		cmd.Stderr = cmd.Stdout

		if err := cmd.Start(); err != nil {
			pkgMu.Lock()
			pkgIsRunning = false
			pkgExitCode = 1
			pkgLogs = append(pkgLogs, fmt.Sprintf("Failed to start apt-get: %v", err))
			pkgFinishedAt = time.Now().Format(time.RFC3339)
			pkgMu.Unlock()
			return
		}

		reader := bufio.NewReader(stdout)
		for {
			line, err := reader.ReadString('\n')
			if len(line) > 0 {
				cleanLine := strings.TrimRight(line, "\r\n")
				pkgMu.Lock()
				pkgLogs = append(pkgLogs, cleanLine)
				if len(pkgLogs) > 3000 {
					pkgLogs = pkgLogs[len(pkgLogs)-3000:]
				}
				pkgMu.Unlock()
				if logFile != nil {
					logFile.WriteString(line)
				}
			}
			if err != nil {
				if err != io.EOF {
					pkgMu.Lock()
					pkgLogs = append(pkgLogs, fmt.Sprintf("Read error: %v", err))
					pkgMu.Unlock()
				}
				break
			}
		}

		cmdErr := cmd.Wait()
		pkgMu.Lock()
		pkgIsRunning = false
		if cmdErr != nil {
			pkgExitCode = 1
			pkgLogs = append(pkgLogs, fmt.Sprintf("[%s] System upgrade finished with error: %v", time.Now().Format("15:04:05"), cmdErr))
		} else {
			pkgExitCode = 0
			pkgLogs = append(pkgLogs, fmt.Sprintf("[%s] System upgrade completed successfully!", time.Now().Format("15:04:05")))
		}
		pkgFinishedAt = time.Now().Format(time.RFC3339)
		pkgMu.Unlock()
	}()

	return ok(ctx, map[string]interface{}{
		"status":     "started",
		"started_at": pkgStartedAt,
	})
}

// GetSystemPackageUpgradeStatus returns the current status and latest logs of package upgrade
func GetSystemPackageUpgradeStatus(ctx echo.Context) error {
	pkgMu.Lock()
	defer pkgMu.Unlock()

	return ok(ctx, map[string]interface{}{
		"running":     pkgIsRunning,
		"exit_code":   pkgExitCode,
		"logs":        pkgLogs,
		"started_at":  pkgStartedAt,
		"finished_at": pkgFinishedAt,
	})
}
