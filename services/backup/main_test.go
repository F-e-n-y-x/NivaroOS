package main

import (
	"context"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
	"github.com/F-e-n-y-x/NivaroOS/services/backup/jobs"
)

func TestLoopbackOnly(t *testing.T) {
	for addr, ok := range map[string]bool{
		jobs.ListenAddr:  true,
		"[::1]:28643":    true,
		"localhost:1":    true,
		":28643":         false, // every interface
		"0.0.0.0:28643":  false,
		"192.168.1.2:80": false,
		"nonsense":       false,
	} {
		if err := loopbackOnly(addr); (err == nil) != ok {
			t.Errorf("loopbackOnly(%q) = %v", addr, err)
		}
	}
}

func TestUnavailableEngine(t *testing.T) {
	var e engine.API = unavailableEngine{reason: "spool folder missing"}
	if _, err := e.Health(context.Background()); engine.CodeOf(err) != engine.CodeEngineUnavailable {
		t.Errorf("Health: %v", err)
	}
	if _, err := e.StartJob(context.Background(), engine.JobRequest{}); engine.CodeOf(err) != engine.CodeEngineUnavailable {
		t.Errorf("StartJob: %v", err)
	}
	// Retryable, so runs wait for the engine instead of failing.
	if !jobs.Retryable(engine.CodeEngineUnavailable, jobs.UnmetSkip) {
		t.Error("engine_unavailable is not retryable")
	}
}
