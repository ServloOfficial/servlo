package ui

import (
	"encoding/json"
	"net/http"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/sitecron"
	"github.com/realrashid/servlo/internal/siteops"
)

// SiteCronResponse is a site's schedule: what it runs, what happened last time,
// and whether its framework has a built-in scheduler worth replacing.
type SiteCronResponse struct {
	Entries []sitecron.Status `json:"entries"`
	// PseudoCron is the framework's own page-load scheduler. Available is false
	// for a framework that has none, and the panel shows nothing rather than an
	// empty card.
	PseudoCron siteops.PseudoCron `json:"pseudo_cron"`
	// Supported is false for a site with no container to run a command in, so
	// the panel can say why the form is not there instead of offering one that
	// would fail on every tick.
	Supported bool `json:"supported"`
	// Unsupported says why, in the operator's terms.
	Unsupported string `json:"unsupported,omitempty"`
}

// SiteCronRequest saves one entry. An empty ID adds; an ID that is already
// there replaces, keeping its position in the list.
type SiteCronRequest struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Command       string `json:"command"`
	Schedule      string `json:"schedule"`
	CaptureOutput bool   `json:"capture_output"`
	Disabled      bool   `json:"disabled"`
}

// SiteCronSaveResponse carries the saved entry back, because the identifier and
// the translated schedule are both derived and the form has to show them.
type SiteCronSaveResponse struct {
	OK    bool             `json:"ok"`
	Error string           `json:"error,omitempty"`
	Entry *sitecron.Status `json:"entry,omitempty"`
}

// cronRoute dispatches the cron subroutes:
//
//	GET    /api/sites/{domain}/cron          → the schedule and its run state
//	POST   /api/sites/{domain}/cron          → add or replace one entry
//	DELETE /api/sites/{domain}/cron/{id}     → remove one entry and its units
//
// Returns true when the request was one of them, so the caller falls through to
// the generic site action handler otherwise.
func cronRoute(w http.ResponseWriter, r *http.Request, domain string, rest []string) bool {
	if len(rest) == 0 || rest[0] != "cron" {
		return false
	}
	site, err := config.FindSiteByDomain(domain)
	if err != nil {
		writeJSON(w, SiteActionResponse{Error: "site not found: " + domain})
		return true
	}
	switch {
	case len(rest) == 1 && r.Method == http.MethodGet:
		handleSiteCronList(w, site)
	case len(rest) == 1 && r.Method == http.MethodPost:
		handleSiteCronSave(w, r, site)
	case len(rest) == 2 && r.Method == http.MethodDelete:
		handleSiteCronDelete(w, site, rest[1])
	default:
		http.NotFound(w, r)
	}
	return true
}

func handleSiteCronList(w http.ResponseWriter, site *config.Site) {
	supported, why := cronSupported(site)
	writeJSON(w, SiteCronResponse{
		Entries:     sitecron.List(*site),
		PseudoCron:  siteops.PseudoCronFor(site),
		Supported:   supported,
		Unsupported: why,
	})
}

func handleSiteCronSave(w http.ResponseWriter, r *http.Request, site *config.Site) {
	if supported, why := cronSupported(site); !supported {
		writeJSON(w, SiteCronSaveResponse{Error: why})
		return
	}
	var req SiteCronRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, SiteCronSaveResponse{Error: "reading the entry: " + err.Error()})
		return
	}
	saved, err := siteops.SaveCron(site, config.CronEntry{
		ID:            req.ID,
		Name:          req.Name,
		Command:       req.Command,
		Schedule:      req.Schedule,
		CaptureOutput: req.CaptureOutput,
		Disabled:      req.Disabled,
	})
	if err != nil {
		writeJSON(w, SiteCronSaveResponse{Error: err.Error()})
		return
	}
	status := sitecron.StatusOf(*site, saved)
	writeJSON(w, SiteCronSaveResponse{OK: true, Entry: &status})
}

func handleSiteCronDelete(w http.ResponseWriter, site *config.Site, id string) {
	if err := siteops.DeleteCron(site, id); err != nil {
		writeJSON(w, SiteActionResponse{Error: err.Error()})
		return
	}
	writeJSON(w, SiteActionResponse{OK: true})
}

// SitePseudoCronRequest is the switch: replace the framework's page-load
// scheduler with a timer, or put it back.
type SitePseudoCronRequest struct {
	Replace bool `json:"replace"`
}

// handleSitePseudoCron serves POST on /api/sites/{domain}/pseudo-cron. There is
// no GET: the state is part of the cron listing, and a second endpoint saying
// the same thing is a second endpoint that can disagree.
func handleSitePseudoCron(w http.ResponseWriter, r *http.Request, site *config.Site) {
	var req SitePseudoCronRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, SiteActionResponse{Error: "reading the request: " + err.Error()})
		return
	}
	if supported, why := cronSupported(site); !supported && req.Replace {
		writeJSON(w, SiteActionResponse{Error: why})
		return
	}
	if err := siteops.SetPseudoCron(site, req.Replace); err != nil {
		writeJSON(w, SiteActionResponse{Error: err.Error()})
		return
	}
	writeJSON(w, SiteActionResponse{OK: true})
}

// cronSupported reports whether this site can run a scheduled command at all.
// A host-proxy site has no container to run one in, and saying so is better
// than a form whose every entry fails once a minute.
func cronSupported(site *config.Site) (bool, string) {
	if site.IsHostProxy() {
		return false, site.Name + " runs on the host rather than in a container, so servlo has nowhere to run a scheduled command for it"
	}
	return true, ""
}
