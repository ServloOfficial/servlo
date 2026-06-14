package ui

import (
	"net/http"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/sitedoctor"
)

// doctorRoute handles GET /api/sites/{domain}/doctor, returning the app-level
// health checks for a Laravel site. Loopback-only: the migrations check execs
// `php artisan migrate:status` in the site's container, the same trust level as
// the command runner. Returns true when it owns the request. The check logic
// itself lives in internal/sitedoctor so the TUI shares it.
func doctorRoute(w http.ResponseWriter, r *http.Request, domain string, rest []string) bool {
	if len(rest) != 1 || rest[0] != "doctor" {
		return false
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return true
	}
	if !isLoopbackRequest(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return true
	}
	site, err := config.FindSiteByDomain(domain)
	if err != nil {
		writeJSON(w, map[string]any{"error": "site not found: " + domain})
		return true
	}
	// The checks are Laravel-flavoured (APP_KEY, migrations, storage:link), so
	// don't run them against other frameworks — the UI only shows the tab for
	// Laravel sites, but guard the endpoint too.
	if site.Framework != "laravel" {
		writeJSON(w, sitedoctor.Response{Checks: []sitedoctor.Check{}})
		return true
	}
	branch := r.URL.Query().Get("branch")
	path := site.Path
	if branch != "" {
		// An unresolved branch must not fall back to the parent checkout, or the
		// doctor would diagnose the main site's .env and database while the UI
		// thinks it's looking at the worktree. Refuse, as the command runner does.
		wt := resolveSitePath(site, branch)
		if wt == "" {
			writeJSON(w, map[string]any{"error": "unknown worktree branch: " + branch})
			return true
		}
		path = wt
	}
	// Freshly added worktrees don't carry .env (it's gitignored), so materialise
	// it first — otherwise every file check reads a missing .env and reports a
	// healthy worktree as broken. No-op for the parent and idempotent.
	ensureWorktreeEnvIfBranch(site, branch)
	writeJSON(w, sitedoctor.Run(r.Context(), path))
	return true
}
