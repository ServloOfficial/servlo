//go:build darwin

package logsource

import "github.com/geodro/lerd/internal/unitlog"

// readJournal has no journald on macOS. Units that actually run as detached
// podman containers are read with `podman logs`; the launchd-supervised ones
// (dns, watcher, ui, exec-mode workers) tail their ~/Library/Logs/lerd file.
func readJournal(src Source, opts Opts) (Result, error) {
	if unitlog.IsContainerUnit(src.Locator) {
		podSrc := src
		podSrc.Kind = KindPodman
		return readPodman(podSrc, opts)
	}
	fileSrc := Source{
		Name:    src.Name,
		Kind:    KindFile,
		Locator: unitlog.LogPath(src.Locator),
		Scope:   src.Scope,
		Format:  "raw",
	}
	return readFile(fileSrc, opts)
}
