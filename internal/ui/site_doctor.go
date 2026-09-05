package ui

import (
	"net/http"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/sitedoctor"
)

// doctorRoute handles the doctor subroutes for a site. It requires
// dashboard-control authority because checks and fixes execute in containers.
// Returns true when it owns the request. The check logic itself lives in
// internal/sitedoctor so the TUI and CLI share it.
//
//	GET  /api/sites/{domain}/doctor                 → run checks
//	POST /api/sites/{domain}/doctor/fix/{key}/run   → run a package-manager fix
func doctorRoute(w http.ResponseWriter, r *http.Request, domain string, rest []string) bool {
	if len(rest) == 0 || rest[0] != "doctor" {
		return false
	}
	site, err := config.FindSiteByDomain(domain)
	if err != nil {
		writeJSON(w, map[string]any{"error": "site not found: " + domain})
		return true
	}
	switch {
	case len(rest) == 1 && r.Method == http.MethodGet:
		handleDoctorRun(w, r, site)
	case len(rest) == 4 && rest[1] == "fix" && rest[3] == "run" && r.Method == http.MethodPost:
		handleDoctorFixRun(w, r, site, rest[2])
	default:
		http.NotFound(w, r)
	}
	return true
}

func handleDoctorRun(w http.ResponseWriter, r *http.Request, site *config.Site) {
	writeJSON(w, sitedoctor.RunForPath(r.Context(), site.Path, site.Framework))
}

// handleDoctorFixRun runs an allowlisted package-manager fix (composer
// install/update, npm install, npm audit fix) and streams its output as SSE,
// reusing the command runner's stream and per-site run lock.
func handleDoctorFixRun(w http.ResponseWriter, r *http.Request, site *config.Site, key string) {
	shell, ok := sitedoctor.DoctorFixCommands[key]
	if !ok {
		writeJSON(w, map[string]any{"error": "unknown doctor fix: " + key})
		return
	}
	release, busyWith, ok := tryAcquireRun(siteRunLockKey(site), key)
	if !ok {
		w.WriteHeader(http.StatusConflict)
		writeJSON(w, map[string]any{"error": "another command is already running on this site: " + busyWith})
		return
	}
	defer release()
	streamShellRun(w, r.Context(), site.Path, shell, false)
}
