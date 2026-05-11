package cli

import "github.com/geodro/lerd/internal/config"

// WorkerSupportedOnPlatform is the exported entry point to the platform
// support gate. External packages (the watcher's exec_workers loop, future
// callers) consult it before enumerating a worker as "expected to be
// running" — without the gate they would re-issue start attempts every
// tick for workers that the platform-specific writeWorkerUnitFile
// silently skips.
//
// The actual policy lives in the build-tagged worker_supported_<goos>.go
// files via the workerSupportedOnPlatform package var, which tests can
// substitute. This wrapper is here so the var can stay unexported.
func WorkerSupportedOnPlatform(w config.FrameworkWorker) (bool, string) {
	return workerSupportedOnPlatform(w)
}
