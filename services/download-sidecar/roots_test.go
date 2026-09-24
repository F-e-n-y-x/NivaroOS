package main

import "os"

// Tests download into t.TempDir(); make the temp root a storage root.
func init() {
	pathPolicy.extra = append(pathPolicy.extra, os.TempDir())
}
