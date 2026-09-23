// NivaroOS-Setup.exe: the Guest Tools disc's AutoRun target. Windows'
// AutoRun can only launch an .exe (pointing it at the .bat made
// double-clicking the CD fail with "no app associated"), so this tiny
// launcher asks for administrator rights and runs
// NivaroOS-Guest-Tools-Setup.bat from the same disc.
//
// Built for Windows by `go generate` in the vm-sidecar package; the .exe
// is embedded into the sidecar and put on the disc.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	exe, err := os.Executable()
	if err != nil {
		os.Exit(1)
	}
	bat := filepath.Join(filepath.Dir(exe), "NivaroOS-Guest-Tools-Setup.bat")
	// Single quotes are PowerShell's literal strings; double any in the path.
	quoted := "'" + strings.ReplaceAll(bat, "'", "''") + "'"
	ps := "Start-Process -FilePath 'cmd.exe' -ArgumentList '/c', ('\"' + " + quoted + " + '\"') -Verb RunAs"
	_ = exec.Command("powershell.exe", "-NoProfile", "-WindowStyle", "Hidden", "-Command", ps).Run()
}
