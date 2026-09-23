package service

import (
	"path/filepath"

	"github.com/F-e-n-y-x/NivaroOS/services/core/service/aptjob"
)

// AptJobs runs every apt operation the UI starts (install, remove, upgrade,
// update) as a background job - one at a time, outside core's cgroup.
var AptJobs *aptjob.Runner

func InitAptJobs(dataDir string) {
	if dataDir == "" {
		dataDir = "/var/lib/nivaroos"
	}
	AptJobs = aptjob.New(aptjob.Options{Dir: filepath.Join(dataDir, "apt-jobs")})
}
