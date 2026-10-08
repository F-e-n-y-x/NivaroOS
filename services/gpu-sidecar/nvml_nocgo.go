//go:build !linux || !cgo

package main

import "errors"

// Built without cgo: nvidia-smi only.
func queryNVML() ([]gpuStats, error) { return nil, errors.New("built without cgo: no NVML") }
