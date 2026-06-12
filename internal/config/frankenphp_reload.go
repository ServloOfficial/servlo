package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/geodro/lerd/internal/wsl"
)

// WatcherNeedsPolling reports whether a file-change watcher has to poll because
// host filesystem events don't reach the process watching them: always on
// macOS (the runtime runs inside the podman VM) and on WSL2 only for projects
// under a /mnt (9p) mount, where inotify isn't delivered. This is the canonical
// home for the predicate; cli.watcherNeedsPolling delegates here so the Horizon
// and Octane reload paths agree.
func WatcherNeedsPolling(sitePath string) bool {
	if runtime.GOOS == "darwin" {
		return true
	}
	return wsl.IsWSL() && strings.HasPrefix(sitePath, "/mnt/")
}

// ProjectHasChokidar reports whether the chokidar npm package, required by the
// Octane and Horizon file watchers, is installed in the project. Canonical home;
// cli.projectHasChokidar delegates here.
func ProjectHasChokidar(sitePath string) bool {
	info, err := os.Stat(filepath.Join(sitePath, "node_modules", "chokidar"))
	return err == nil && info.IsDir()
}

// ResolveFrankenPHPWorkerEntrypoint returns the entrypoint to launch inside the
// FrankenPHP container, substituting the watch-enabled variant
// (WorkerReloadEntrypoint) for WorkerEntrypoint when the site is in worker mode,
// the framework declares a watch variant, and the project has opted the "octane"
// worker into reload. On hosts where the container can't observe host filesystem
// events it appends Octane's --poll flag, mirroring how Horizon's
// resolveWorkerCommand handles polling.
//
// When reload is requested but chokidar is absent it silently falls back to the
// standard entrypoint; the enable paths (cli.ApplyOctaneReload and the UI) refuse
// up front when chokidar is missing, so the displayed state never diverges from
// what actually runs.
func (fw *Framework) ResolveFrankenPHPWorkerEntrypoint(sitePath string, worker bool) []string {
	base := fw.FrankenPHPEntrypoint(worker)
	if !worker || fw == nil || fw.FrankenPHP == nil {
		return base
	}
	if len(fw.FrankenPHP.WorkerReloadEntrypoint) == 0 {
		return base
	}
	if !ProjectReloadsWorker(sitePath, "octane") || !ProjectHasChokidar(sitePath) {
		return base
	}
	ep := append([]string(nil), fw.FrankenPHP.WorkerReloadEntrypoint...)
	if WatcherNeedsPolling(sitePath) {
		ep = appendPollFlag(ep)
	}
	return ep
}

// FrankenPHPQuadletSpec resolves the entrypoint and env a site's FrankenPHP
// container should run with, applying the reload-aware worker entrypoint
// (octane:start --watch) when the project opted in. Both the apply path
// (siteops.FinishFrankenPHPLink) and the global install refresh resolve through
// here, so a site's quadlet can't diverge between the two writers.
func (s *Site) FrankenPHPQuadletSpec() (entrypoint []string, env map[string]string) {
	fw, _ := GetFrameworkForDir(s.Framework, s.Path)
	return fw.ResolveFrankenPHPWorkerEntrypoint(s.Path, s.RuntimeWorker), fw.FrankenPHPEnv(s.RuntimeWorker)
}

// appendPollFlag adds Octane's --poll to a resolved watch entrypoint. For the
// `sh -c "<script>"` form used to install pcntl before exec'ing octane, the flag
// must go inside the script (the trailing element); for a bare argv form it is a
// normal trailing argument.
func appendPollFlag(ep []string) []string {
	if len(ep) >= 2 && ep[0] == "sh" && ep[1] == "-c" {
		ep[len(ep)-1] += " --poll"
		return ep
	}
	return append(ep, "--poll")
}
