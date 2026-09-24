// gen-notifytext writes services/backup/jobs/notify_en.json: the English
// texts the service renders notifications with, taken from
// ui/src/assets/lang/en_US.json (see jobs/notifytext.go). Run it with
// `go generate ./jobs` from services/backup; TestNotifyTextInSync fails
// until the checked-in file matches.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/jobs"
)

func main() {
	root, err := repoRoot()
	if err != nil {
		log.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "ui", "src", "assets", "lang", "en_US.json"))
	if err != nil {
		log.Fatal(err)
	}
	var enUS map[string]string
	if err := json.Unmarshal(raw, &enUS); err != nil {
		log.Fatalf("en_US.json: %v", err)
	}
	dst := filepath.Join(root, "services", "backup", "jobs", "notify_en.json")
	if err := os.WriteFile(dst, jobs.RenderNotifyText(enUS), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Println("wrote services/backup/jobs/notify_en.json")
}

// repoRoot walks up from the working directory to the folder holding
// both services/backup and ui/src.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if isDir(filepath.Join(dir, "services", "backup")) && isDir(filepath.Join(dir, "ui", "src")) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no NivaroOS checkout (services/backup + ui/src) above the working directory")
		}
		dir = parent
	}
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
