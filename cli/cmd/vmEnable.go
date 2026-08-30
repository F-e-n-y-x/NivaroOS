/*
Copyright © 2022 Recasa

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

const recasaSrcDir = "/opt/recasa/src"

const vmSidecarUnitContent = `[Unit]
After=network.target recasa-message-bus.service
Description=Recasa VM Sidecar

[Service]
ExecStart=/usr/bin/recasa-vm-sidecar
Restart=always

[Install]
WantedBy=multi-user.target
`

const vmSidecarUnitPath = "/usr/lib/systemd/system/recasa-vm-sidecar.service"

var vmEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Build and enable the VM Manager service",
	RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := os.Stat(recasaSrcDir); err != nil {
			return fmt.Errorf("%s not found - run the Recasa installer first: %w", recasaSrcDir, err)
		}

		build := exec.Command("go", "build", "-o", "/usr/bin/recasa-vm-sidecar", ".")
		build.Dir = recasaSrcDir + "/services/vm-sidecar"
		build.Stdout = os.Stdout
		build.Stderr = os.Stderr
		if err := build.Run(); err != nil {
			return fmt.Errorf("building recasa-vm-sidecar: %w", err)
		}

		if err := os.WriteFile(vmSidecarUnitPath, []byte(vmSidecarUnitContent), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", vmSidecarUnitPath, err)
		}

		for _, args := range [][]string{
			{"daemon-reload"},
			{"enable", "--now", "recasa-vm-sidecar.service"},
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
