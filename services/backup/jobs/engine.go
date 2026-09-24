package jobs

import "github.com/F-e-n-y-x/NivaroOS/services/backup/engine"

// EngineClient is how the job side drives the engine: exactly the frozen
// engine.API (spec §11.1), in process. Production passes the engine
// package's implementation; tests use enginetest.FakeEngine
// (services/backup/engine/enginetest).
type EngineClient = engine.API
