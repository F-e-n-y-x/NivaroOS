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

var hostDesktopDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Stop and disable Host Desktop streaming",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureRoot(); err != nil {
			return err
		}

		c := exec.Command("systemctl", "disable", "--now", "nivaroos-host-desktop.service")
		out, err := c.CombinedOutput()
		if err != nil && !strings.Contains(string(out), "not found") && !strings.Contains(string(out), "does not exist") {
			fmt.Fprint(os.Stderr, string(out))
			return fmt.Errorf("systemctl disable --now nivaroos-host-desktop.service: %w", err)
		}
		fmt.Println("Host Desktop streaming is disabled.")
		return nil
	},
}

func init() {
	hostDesktopCmd.AddCommand(hostDesktopDisableCmd)
}
