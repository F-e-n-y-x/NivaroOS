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
	"strings"

	"github.com/spf13/cobra"
)

var vmDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Stop and disable the VM Manager service (does not delete VM data)",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := exec.Command("systemctl", "disable", "--now", "nivaroos-vm-sidecar.service")
		out, err := c.CombinedOutput()
		if err != nil && !strings.Contains(string(out), "not found") && !strings.Contains(string(out), "does not exist") {
			fmt.Fprint(os.Stderr, string(out))
			return fmt.Errorf("systemctl disable --now nivaroos-vm-sidecar.service: %w", err)
		}
		fmt.Println("VM Manager is disabled. VM disk images under /DATA/VMs were left untouched.")
		return nil
	},
}

func init() {
	vmCmd.AddCommand(vmDisableCmd)
}
