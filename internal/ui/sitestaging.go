package ui

import (
	"encoding/json"
	"net/http"

	"github.com/ServloOfficial/servlo/internal/authz"
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/staging"
)

// The staging surface on a site.
//
//	GET  /api/sites/{domain}/staging  → whether it is one, and what it copies
//	POST /api/sites/{domain}/staging  → refresh, reset the password, or unstage
//
// Creating one is not here. A staging site is a new site with a domain of its
// own, so it belongs to whatever creates sites rather than to a card on an
// existing one, and putting it here would have the panel create a second site
// from the settings of the first.

// SiteStagingResponse is what the card renders.
type SiteStagingResponse struct {
	// Staging is true when this site is a copy of another.
	Staging bool   `json:"staging"`
	Origin  string `json:"origin,omitempty"`
	// OriginDomain is the live site's address, which is what an operator
	// recognises. The name above is servlo's internal one and is only what is
	// left to show when the live site has been removed.
	OriginDomain string `json:"origin_domain,omitempty"`
	// OriginExists is false when the live site has been removed, which turns
	// the refresh button off rather than letting it fail.
	OriginExists bool   `json:"origin_exists"`
	User         string `json:"user,omitempty"`
	RefreshedAt  string `json:"refreshed_at,omitempty"`
	// Copies names the staging sites that copy from this one, for a live site.
	// It is the other half of the same relationship and the reason anybody
	// looking at a live site cares about staging at all.
	Copies []string `json:"copies"`
	Error  string   `json:"error,omitempty"`
}

// SiteStagingRequest is one action on a staging site.
type SiteStagingRequest struct {
	Action string `json:"action"`
	// Bring narrows a refresh: "files", "database", or empty for both.
	Bring string `json:"bring,omitempty"`
}

// SiteStagingActionResponse is what came of it.
type SiteStagingActionResponse struct {
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	Files    int    `json:"files,omitempty"`
	Bytes    int64  `json:"bytes,omitempty"`
	Database string `json:"database,omitempty"`
	// Password is returned exactly once, by a reset, and stored nowhere.
	Password string `json:"password,omitempty"`
	User     string `json:"user,omitempty"`
	Note     string `json:"note,omitempty"`
}

// stagingRoute dispatches /api/sites/{domain}/staging.
func stagingRoute(w http.ResponseWriter, r *http.Request, domain string, rest []string) bool {
	if len(rest) != 1 || rest[0] != "staging" {
		return false
	}
	site, err := config.FindSiteByDomain(domain)
	if err != nil {
		writeJSON(w, SiteStagingResponse{Copies: []string{}, Error: "site not found: " + domain})
		return true
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, siteStagingState(site))
	case http.MethodPost:
		handleSiteStagingAction(w, r, site)
	default:
		http.NotFound(w, r)
	}
	return true
}

func siteStagingState(site *config.Site) SiteStagingResponse {
	out := SiteStagingResponse{Copies: []string{}}
	if site.IsStaging() {
		out.Staging = true
		out.Origin = site.Staging.Origin
		out.User = site.Staging.User
		out.RefreshedAt = site.Staging.RefreshedAt
		if origin, err := config.FindSiteByRef(site.Staging.Origin); err == nil {
			out.OriginExists = true
			out.OriginDomain = origin.PrimaryDomain()
		}
		return out
	}
	reg, err := config.LoadSites()
	if err != nil {
		out.Error = err.Error()
		return out
	}
	for i := range reg.Sites {
		other := &reg.Sites[i]
		if other.IsStaging() && other.Staging.Origin == site.Name {
			out.Copies = append(out.Copies, other.PrimaryDomain())
		}
	}
	return out
}

func handleSiteStagingAction(w http.ResponseWriter, r *http.Request, site *config.Site) {
	var req SiteStagingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, SiteStagingActionResponse{Error: "reading the request: " + err.Error()})
		return
	}
	if !site.IsStaging() {
		writeJSON(w, SiteStagingActionResponse{Error: site.Name + " is not a staging site"})
		return
	}
	// Admin, even though the path is site-scoped and a Developer assigned to
	// this site reaches it. A refresh copies the live site's database onto this
	// one, so a Developer given only the staging site could otherwise pull
	// production data onto a site they control. Whether they should see that
	// data is not this site's assignment to answer.
	// Absent counts as not allowed, the same way the scope middleware treats a
	// path it does not recognise: unrecognised meaning allowed is how a route
	// added later is open until somebody remembers to classify it.
	if scope, ok := authz.ScopeFrom(r.Context()); !ok || !scope.MayAdminister() {
		writeJSON(w, SiteStagingActionResponse{
			Error: "refreshing copies the live site's data across, so it needs an admin account",
		})
		return
	}

	switch req.Action {
	case "refresh":
		bring := staging.Everything()
		switch req.Bring {
		case "files":
			bring = staging.Bring{Files: true}
		case "database":
			bring = staging.Bring{Database: true}
		}
		res, err := staging.Refresh(site, bring)
		if err != nil {
			writeJSON(w, SiteStagingActionResponse{Error: err.Error()})
			return
		}
		authz.SetAuditDetail(r, "refreshed "+site.Name+" from "+site.Staging.Origin)
		writeJSON(w, SiteStagingActionResponse{
			OK: true, Files: res.Files, Bytes: res.Bytes, Database: res.Database, Note: res.Note,
		})
	case "password":
		creds, hash, err := staging.NewCredentials(site.Staging.User)
		if err != nil {
			writeJSON(w, SiteStagingActionResponse{Error: err.Error()})
			return
		}
		if err := staging.WriteHtpasswd(site.PrimaryDomain(), creds.User, hash); err != nil {
			writeJSON(w, SiteStagingActionResponse{Error: err.Error()})
			return
		}
		site.Staging.User, site.Staging.Hash = creds.User, hash
		if err := config.AddSite(*site); err != nil {
			writeJSON(w, SiteStagingActionResponse{Error: err.Error()})
			return
		}
		// The password itself is never in the audit log. The fact of a reset is
		// what an operator needs to see there.
		authz.SetAuditDetail(r, "reset the staging password for "+site.Name)
		writeJSON(w, SiteStagingActionResponse{OK: true, User: creds.User, Password: creds.Password})
	default:
		writeJSON(w, SiteStagingActionResponse{Error: "unknown action " + req.Action})
	}
}
