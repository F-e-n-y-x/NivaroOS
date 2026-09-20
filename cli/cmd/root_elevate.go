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
)

// ensureRoot makes a command that needs to install packages, write systemd
// units, or otherwise touch the system safe to run from a normal user's own
// terminal, not just as root already. Without this, a command like
// `nivaroos vm enable` just fails partway through with a permission-denied
// error from whichever privileged step it first hit, instead of either
// working or explaining why not. Mirrors installer/install.sh's own
// check_root() - that script already re-execs itself under sudo when not
// root, so a different UX here (e.g. just erroring) would be inconsistent
// with how this project already handles the exact same situation.
func ensureRoot() error {
	if os.Geteuid() == 0 {
		return nil
	}

	sudoPath, err := exec.LookPath("sudo")
	if err != nil {
		return fmt.Errorf("this command needs root privileges - re-run it with sudo, or as root")
	}

	// os.Args[0] can be a relative path or a bare name resolved via PATH at
	// invocation time - re-resolve it to an absolute path first so the
	// re-exec under sudo doesn't depend on sudo's own PATH or working
	// directory matching the caller's.
	selfPath, err := os.Executable()
	if err != nil {
		selfPath = os.Args[0]
	}

	args := append([]string{selfPath}, os.Args[1:]...)
	c := exec.Command(sudoPath, args...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return fmt.Errorf("re-running under sudo: %w", err)
	}
	os.Exit(0)
	return nil
}
