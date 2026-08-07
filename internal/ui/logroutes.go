package ui

import (
	"net/http"
	"strings"
)

// siteWorkerLogKinds are the /api/{kind}/{site}/logs stream routes. The kind
// doubles as the unit-name infix, so a new worker route is one entry here plus
// its mux registration.
var siteWorkerLogKinds = []string{"queue", "horizon", "schedule", "reverb", "stripe"}

// unitForLogPath maps a log stream path back to the unit behind it. Every log
// route resolves through here, so no two of them can disagree about which unit
// a pane is showing.
func unitForLogPath(path string) (string, bool) {
	if path == "/api/watcher/logs" {
		return "servlo-watcher", true
	}
	if rest, ok := strings.CutPrefix(path, "/api/logs/"); ok {
		if !allowedContainer.MatchString(rest) {
			return "", false
		}
		return rest, true
	}
	for _, kind := range siteWorkerLogKinds {
		if rest, ok := strings.CutPrefix(path, "/api/"+kind+"/"); ok {
			parts := strings.Split(rest, "/")
			if len(parts) != 2 || parts[1] != "logs" || !allowedQueueUnit.MatchString(parts[0]) {
				return "", false
			}
			return "servlo-" + kind + "-" + parts[0], true
		}
	}
	// /api/worker/{site}/{worker}/logs — unit: servlo-{worker}-{site}
	if rest, ok := strings.CutPrefix(path, "/api/worker/"); ok {
		parts := strings.Split(rest, "/")
		if len(parts) != 3 || parts[2] != "logs" ||
			!allowedQueueUnit.MatchString(parts[0]) || !allowedQueueUnit.MatchString(parts[1]) {
			return "", false
		}
		return "servlo-" + parts[1] + "-" + parts[0], true
	}
	return "", false
}

// handleUnitLogStream serves every per-site worker log stream, resolving the
// request path to its unit through unitForLogPath.
func handleUnitLogStream(w http.ResponseWriter, r *http.Request) {
	unit, ok := unitForLogPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	streamUnitLogs(w, r, unit)
}
