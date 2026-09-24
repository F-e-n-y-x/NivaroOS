package engine

// Every rclone backend is linked in, exactly like local-storage does, so
// any remote a user set up in NivaroOS (it may be of any type rclone
// knows) is a backup source or destination. TeraBox is NivaroOS's own
// backend and lives in local-storage; it only imports rclone, so linking
// it here adds no second copy of anything.
import (
	_ "github.com/F-e-n-y-x/NivaroOS/services/local-storage/backend/terabox"
	_ "github.com/rclone/rclone/backend/all"
)
