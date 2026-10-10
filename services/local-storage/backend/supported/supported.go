// Package supported links in the rclone backends NivaroOS uses, instead of
// rclone's backend/all (about 70 backends, ~30 MB per binary and their
// init() cost). local-storage and backup both import it so they agree.
//
// The list: every type local-storage's providerCatalog offers
// (route/v1/cloud_accounts.go), plus local and crypt for the backup engine.
// A type added to the catalog must be added here too
// (TestCatalogBackendsLinked checks it). A remote of any other type in an
// existing rclone.conf is not mounted or backed up; both services say so
// in the log by name and type instead of failing.
package supported

import (
	_ "github.com/F-e-n-y-x/NivaroOS/services/local-storage/backend/terabox"
	_ "github.com/rclone/rclone/backend/b2"
	_ "github.com/rclone/rclone/backend/crypt"
	_ "github.com/rclone/rclone/backend/drive"
	_ "github.com/rclone/rclone/backend/dropbox"
	_ "github.com/rclone/rclone/backend/iclouddrive"
	_ "github.com/rclone/rclone/backend/local"
	_ "github.com/rclone/rclone/backend/onedrive"
	_ "github.com/rclone/rclone/backend/s3"
	_ "github.com/rclone/rclone/backend/sftp"
	_ "github.com/rclone/rclone/backend/smb"
	_ "github.com/rclone/rclone/backend/webdav"
)
