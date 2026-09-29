//go:build !linux || !cgo

package main

import "errors"

// Built without cgo: NVIDIA fans are listed read-only (nvidia-smi still
// shows them in the GPU widget).
func openNVML() (gpuAPI, error) {
	return nil, errors.New("this build of the fan service has no NVIDIA support (built without cgo)")
}
