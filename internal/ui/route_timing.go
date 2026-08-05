package ui

import (
	"net/http"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/reqstats"
)

// resolveSiteName maps a site identifier that may be a domain (astrolov.test) to
// the internal site name (astrolov) the dumps ring and reqstats key on, so a
// caller can pass either. Shares one resolver with the dispatch boundary.
func resolveSiteName(s string) string {
	return config.ResolveSiteRef(s)
}

// handleRouteTiming serves the per-site request-timing snapshot (median + slow
// routes) keyed by site name, matching how analyze_queries is addressed.
//
//	GET /api/queries/route-timing?site=<name>[&branch=<sanitized>]
func handleRouteTiming(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	key := resolveSiteName(q.Get("site"))
	stats, ok := reqstats.LoadSite(config.RequestStatsFile(), key)
	if !ok {
		stats = reqstats.SiteStats{Site: key, Slow: []reqstats.RouteStat{}}
	}
	writeJSON(w, stats)
}
