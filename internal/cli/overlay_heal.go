package cli

import "strings"

// isOverlayStorageError reports whether err is the podman graph-driver /
// overlay-storage failure that surfaces after an ungraceful host shutdown
// leaves the Podman Machine's container storage with a stale overlay mount,
// e.g. `getting graph driver info "<id>": readlink
// /var/lib/containers/storage/overlay: invalid argument`. Every container
// start fails with this until the VM remounts its storage. Matching requires
// the graph-driver phrase plus an overlay/readlink signal so unrelated
// failures (port conflicts, missing images) don't trip the heal.
func isOverlayStorageError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "graph driver") {
		return false
	}
	return strings.Contains(msg, "/containers/storage/overlay") ||
		(strings.Contains(msg, "readlink") && strings.Contains(msg, "invalid argument"))
}
