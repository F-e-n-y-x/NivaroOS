// gen-uicontract writes the UI mirrors of the backup Go contracts
// (ui/src/apps/backup/errorCodes.js and events.js). Run it with
// `go generate ./jobs` from services/backup; TestUIContractInSync fails
// until the checked-in files match.
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/jobs"
)

func main() {
	root, err := repoRoot()
	if err != nil {
		log.Fatal(err)
	}
	paths := make([]string, 0, len(jobs.UIContractFiles))
	for p := range jobs.UIContractFiles {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		dst := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(dst, jobs.UIContractFiles[rel](), 0o644); err != nil {
			log.Fatal(err)
		}
		fmt.Println("wrote", rel)
	}
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
