package service

import (
	"path/filepath"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/core/service/trash"
)

// Trash is the Files app's recycle bin (see service/trash).
var Trash *trash.Bin

// TrashRetention: items older than this are removed for good.
const TrashRetention = 30 * 24 * time.Hour

func InitTrash(dataDir string) {
	Trash = trash.New(trash.Options{IndexPath: filepath.Join(dataDir, "trash-roots.json")})
	go func() {
		for {
			Trash.Purge(TrashRetention)
			time.Sleep(6 * time.Hour)
		}
	}()
}
