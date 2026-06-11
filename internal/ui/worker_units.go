package ui

import "github.com/geodro/lerd/internal/unitlog"

// isFrameworkWorkerUnit reports whether unit looks like a built-in framework
// worker (queue, schedule, horizon, reverb). Used by handleLogs to decide
// whether to register the SSE stream with logStreams so a worker-mode
// migration can cancel it before issuing podman rm calls.
func isFrameworkWorkerUnit(unit string) bool { return unitlog.IsFrameworkWorkerUnit(unit) }
