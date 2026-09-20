/*
Copyright © 2022 NivaroOS

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

// resolveGoBinary finds the go toolchain even when PATH can't be trusted to
// have it. ensureRoot() re-execs this command under `sudo`, and Debian's
// default sudoers "secure_path" replaces PATH with a fixed list that does
// not include /usr/local/go/bin - where installer/install.sh always puts
// the Go toolchain it downloads - regardless of what the original,
// unprivileged shell's own PATH contained or whether `-E`/--preserve-env
// was used (secure_path is specifically designed to override both). A bare
// exec.Command("go", ...) then fails with "executable file not found in
// $PATH" even though Go is very much installed.
func resolveGoBinary() (string, error) {
	if p, err := exec.LookPath("go"); err == nil {
		return p, nil
	}
	for _, candidate := range []string{"/usr/local/go/bin/go", "/usr/lib/go/bin/go", "/usr/bin/go"} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("go toolchain not found (checked PATH and /usr/local/go/bin) - run the NivaroOS installer first, or install Go manually")
}

const nivaroosSrcDir = "/opt/nivaroos/src"

const vmSidecarUnitContent = `[Unit]
After=network.target nivaroos-message-bus.service
Description=NivaroOS VM Sidecar

[Service]
ExecStart=/usr/bin/nivaroos-vm-sidecar
Restart=always

[Install]
WantedBy=multi-user.target
`

const vmSidecarUnitPath = "/usr/lib/systemd/system/nivaroos-vm-sidecar.service"

var vmEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Build and enable the VM Manager service",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureRoot(); err != nil {
			return err
		}

		if _, err := os.Stat(nivaroosSrcDir); err != nil {
			return fmt.Errorf("%s not found - run the NivaroOS installer first: %w", nivaroosSrcDir, err)
		}

		// nivaroos-vm-sidecar links against libvirt via cgo (pkg-config: libvirt-admin)
		// and needs a C compiler to do so. The base install (--without-vm) never
		// installs either, since it doesn't need to. This command is meant to be
		// run long after the initial install, so the apt cache may be stale -
		// update it first rather than risk a resolve failure for a since-removed
		// package version.
		update := exec.Command("apt-get", "update")
		update.Stdout = os.Stdout
		update.Stderr = os.Stderr
		if err := update.Run(); err != nil {
			return fmt.Errorf("apt-get update: %w", err)
		}

		deps := exec.Command("apt-get", "install", "-y", "libvirt-dev", "gcc")
		deps.Stdout = os.Stdout
		deps.Stderr = os.Stderr
		if err := deps.Run(); err != nil {
			return fmt.Errorf("installing libvirt-dev and gcc: %w", err)
		}

		goBin, err := resolveGoBinary()
		if err != nil {
			return err
		}

		build := exec.Command(goBin, "build", "-o", "/usr/bin/nivaroos-vm-sidecar", ".")
		build.Dir = nivaroosSrcDir + "/services/vm-sidecar"
		build.Stdout = os.Stdout
		build.Stderr = os.Stderr
		// GOWORK=off: nivaroosSrcDir has a go.work at its root covering every
		// service - workspace-mode dependency resolution across all of them
		// can pick different (sometimes incompatible) versions than this
		// module's own go.mod/go.sum would alone. installer/install.sh's
		// run_step already sets this for exactly this reason; this command
		// needs it for the same reason since it builds from the same tree.
		build.Env = append(os.Environ(), "GOWORK=off", "PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:"+os.Getenv("PATH"))
		if err := build.Run(); err != nil {
			return fmt.Errorf("building nivaroos-vm-sidecar: %w", err)
		}

		if err := os.WriteFile(vmSidecarUnitPath, []byte(vmSidecarUnitContent), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", vmSidecarUnitPath, err)
		}

		for _, args := range [][]string{
			{"daemon-reload"},
			{"enable", "--now", "nivaroos-vm-sidecar.service"},
		} {
			c := exec.Command("systemctl", args...)
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			if err := c.Run(); err != nil {
				return fmt.Errorf("systemctl %v: %w", args, err)
			}
		}

		fmt.Println("VM Manager is enabled. Open the VM Manager app in the web UI to finish setting up virtualization support if you haven't already.")
		return nil
	},
}

func init() {
	vmCmd.AddCommand(vmEnableCmd)
}
