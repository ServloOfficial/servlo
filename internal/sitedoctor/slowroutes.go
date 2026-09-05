package sitedoctor

import (
	"fmt"
	"strings"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/reqstats"
)

// slowRoutesInDetail caps how many routes the finding names, so a site with many
// slow routes still reads as one short line rather than a wall of text.
const slowRoutesInDetail = 3

// checkSlowRoutes reports routes running well above the typical response time,
// read from the watcher's request-timing snapshot. Returns ok=false when the
// path belongs to no site, or there's no snapshot or no flagged route, so a
// healthy or quiet site adds nothing to the report. The remedy is investigation,
// not a command, so no Fix is set.
func checkSlowRoutes(path string) (Check, bool) {
	key, ok := storeKeyForPath(path)
	if !ok {
		return Check{}, false
	}
	stats, ok := reqstats.LoadSite(config.RequestStatsFile(), key)
	if !ok || len(stats.Slow) == 0 {
		return Check{}, false
	}

	parts := make([]string, 0, slowRoutesInDetail)
	for _, r := range stats.Slow {
		if len(parts) == slowRoutesInDetail {
			break
		}
		// A route flagged only in absolute terms has no baseline multiplier, so
		// name it by its p95 rather than an "0x" that reads as a bug.
		if r.Multiplier > 0 {
			parts = append(parts, fmt.Sprintf("%s (%gx, %gms)", r.Route, r.Multiplier, r.P95Millis))
		} else {
			parts = append(parts, fmt.Sprintf("%s (%gms)", r.Route, r.P95Millis))
		}
	}
	detail := fmt.Sprintf("%d route(s) run well above this site's typical %gms response: %s.",
		len(stats.Slow), stats.MedianMillis, strings.Join(parts, ", "))
	if len(stats.Slow) > slowRoutesInDetail {
		detail += fmt.Sprintf(" (+%d more)", len(stats.Slow)-slowRoutesInDetail)
	}
	detail += " See the Request timing panel in the site's Overview."

	return Check{
		Name:   "slow_routes",
		Status: StatusWarn,
		Detail: detail,
	}, true
}

// storeKeyForPath maps a project path to the key its request timing is stored
// under, the site name. A path belonging to no site resolves to ok=false.
func storeKeyForPath(path string) (string, bool) {
	if site, err := config.FindSiteByPath(path); err == nil && site != nil {
		return site.Name, true
	}
	return "", false
}
