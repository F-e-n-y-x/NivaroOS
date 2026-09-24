package main

import (
	"context"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// unavailableEngine stands in when the real engine couldn't start (and
// for the release-scheduled-tasks subcommand, which needs none): every
// call fails with engine_unavailable, which the job side treats as
// "wait", never as a failure.
type unavailableEngine struct{ reason string }

var _ engine.API = unavailableEngine{}

func (u unavailableEngine) err() error {
	return &engine.Error{Code: engine.CodeEngineUnavailable, Detail: u.reason}
}

func (u unavailableEngine) Health(context.Context) (engine.Health, error) {
	return engine.Health{}, u.err()
}

func (u unavailableEngine) Locations(context.Context) ([]engine.Location, error) {
	return nil, u.err()
}

func (u unavailableEngine) Volumes(context.Context) ([]engine.Volume, error) { return nil, u.err() }

func (u unavailableEngine) Resolve(context.Context, engine.ResolveRequest) (engine.Resolved, error) {
	return engine.Resolved{}, u.err()
}

func (u unavailableEngine) ResolvePath(context.Context, engine.ResolvePathRequest) (engine.ResolvePathResult, error) {
	return engine.ResolvePathResult{}, u.err()
}

func (u unavailableEngine) Browse(context.Context, engine.BrowseRequest) (engine.BrowseResult, error) {
	return engine.BrowseResult{}, u.err()
}

func (u unavailableEngine) Precheck(context.Context, engine.PrecheckRequest) (engine.PrecheckResult, error) {
	return engine.PrecheckResult{}, u.err()
}

func (u unavailableEngine) ListVersions(context.Context, engine.VersionsRequest) ([]engine.Version, error) {
	return nil, u.err()
}

func (u unavailableEngine) OpenDownload(context.Context, engine.DownloadRequest) (*engine.Download, error) {
	return nil, u.err()
}

func (u unavailableEngine) StartJob(context.Context, engine.JobRequest) (engine.JobID, error) {
	return 0, u.err()
}

func (u unavailableEngine) JobStatus(context.Context, engine.JobID) (engine.JobStatus, error) {
	return engine.JobStatus{}, u.err()
}

func (u unavailableEngine) StopJob(context.Context, engine.JobID) error { return u.err() }

func (u unavailableEngine) JobLog(context.Context, engine.JobID) (<-chan engine.LogLine, error) {
	return nil, u.err()
}

func (u unavailableEngine) Events(context.Context) (<-chan engine.Event, error) {
	return nil, u.err()
}
