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

// vmSidecarBinary owns Host Desktop provisioning: the x11vnc wrapper
// script, its unit and the desktop provisioner are embedded in it (single
// source of truth, services/vm-sidecar/hostdesktop/), and its
// `install-host-desktop` subcommand is the same install the dashboard's
// Install button and installer/install.sh run. Host Desktop streams through
// vm-sidecar's /host/console, so it can't work without it anyway.
const vmSidecarBinary = "/usr/bin/nivaroos-vm-sidecar"

func findVMSidecar() (string, error) {
	if p, err := exec.LookPath("nivaroos-vm-sidecar"); err == nil {
		return p, nil
	}
	if st, err := os.Stat(vmSidecarBinary); err == nil && !st.IsDir() {
		return vmSidecarBinary, nil
	}
	return "", fmt.Errorf("nivaroos-vm-sidecar is not installed - Host Desktop needs the VM Manager component; re-run the NivaroOS installer with --with-vm --with-host-desktop")
}

var hostDesktopEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Install and enable Host Desktop streaming (x11vnc)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureRoot(); err != nil {
			return err
		}
		sidecar, err := findVMSidecar()
		if err != nil {
			return err
		}
		install := exec.Command(sidecar, "install-host-desktop")
		install.Stdout = os.Stdout
		install.Stderr = os.Stderr
		if err := install.Run(); err != nil {
			return fmt.Errorf("installing Host Desktop streaming failed (see output above): %w", err)
		}
		fmt.Println("Open the Host Desktop app in the web UI to view the stream.")
		return nil
	},
}

func init() {
	hostDesktopCmd.AddCommand(hostDesktopEnableCmd)
}
