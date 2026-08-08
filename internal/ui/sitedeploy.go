package ui

import (
	"encoding/json"
	"net/http"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/deploy"
	"github.com/realrashid/servlo/internal/siteops"
)

// runDeployFn is the deploy itself, indirected so the handler's own behaviour
// can be tested without a repository, a database or a container.
var runDeployFn = deploy.Run

// handleSiteDeploy answers POST /api/sites/{domain}/deploy by deploying the
// site and streaming every phase as it happens.
//
// Streamed rather than answered at the end, because a deploy is minutes long
// and a spinner that eventually says "failed" is the thing this replaces. The
// same SSE shape the command runner and the PHP build use, so the panel already
// knows how to render it.
func handleSiteDeploy(w http.ResponseWriter, r *http.Request, site *config.Site) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// The same lock the command runner and the doctor-fix runner take. A deploy
	// running composer while somebody presses a framework command in another
	// tab is two processes writing the same vendor directory.
	release, busyWith, ok := tryAcquireRun(siteRunLockKey(site), "deploy")
	if !ok {
		writeJSON(w, SiteActionResponse{Error: "this site is busy running " + busyWith})
		return
	}
	defer release()

	sw, done, ok := startPHPBuildStream(w)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	res, err := runDeployFn(deploy.Defaults(site, sw))
	if err != nil {
		// Into the stream rather than as a status code: the response has been
		// streaming for minutes and its headers went out long ago, so the only
		// place left to say what happened is the stream itself.
		done(map[string]any{
			"ok":    false,
			"error": err.Error(),
			// What it managed before failing. A deploy that pulled and then
			// failed its script left the site on new code that was never
			// prepared, and the operator has to know that from here.
			"from":     res.FromCommit,
			"to":       res.ToCommit,
			"snapshot": res.Snapshot,
		})
		return
	}

	done(map[string]any{
		"ok":          true,
		"from":        res.FromCommit,
		"to":          res.ToCommit,
		"snapshot":    res.Snapshot,
		"duration_ms": res.Duration.Milliseconds(),
	})
}

// SiteDeployExcludeResponse is the paths this site's deploys must not remove.
type SiteDeployExcludeResponse struct {
	// Paths is what the next deploy will actually protect, whether it came from
	// the site or from its framework.
	Paths []string `json:"paths"`
	// Custom is false while the site is still following its framework, so the
	// form can say where the list came from and offer to go back to it.
	Custom bool `json:"custom"`
	// Default is the framework's list, which is what "reset" restores and what
	// the form shows as the alternative to the site's own.
	Default []string `json:"default"`
}

// SiteDeployExcludeRequest saves a site's own list.
type SiteDeployExcludeRequest struct {
	Paths []string `json:"paths"`
	// Reset clears the site's list so it follows its framework again, which is
	// a different thing from saving an empty one: an empty list protects
	// nothing, and following the framework protects whatever it declares.
	Reset bool `json:"reset"`
}

// handleSiteDeployExclude serves GET and POST on
// /api/sites/{domain}/deploy-exclude.
func handleSiteDeployExclude(w http.ResponseWriter, r *http.Request, site *config.Site) {
	switch r.Method {
	case http.MethodGet:
		paths, err := siteops.DeployExcludes(site)
		if err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		var fallback []string
		if fw, ok := config.GetFrameworkForDir(site.Framework, site.Path); ok {
			fallback = fw.DeployExcludes()
		}
		writeJSON(w, SiteDeployExcludeResponse{
			Paths:   orEmpty(paths),
			Custom:  site.DeployExclude != nil,
			Default: orEmpty(fallback),
		})
	case http.MethodPost:
		var req SiteDeployExcludeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, SiteActionResponse{Error: "reading the list: " + err.Error()})
			return
		}
		list := &req.Paths
		if req.Reset {
			list = nil
		}
		if err := siteops.SetSiteDeployExclude(site, list); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// orEmpty sends an empty list rather than null, so the form iterates it without
// having to know that JSON has two ways to say nothing.
func orEmpty(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// SiteDeployScriptResponse is the site's deploy script and where it lives.
type SiteDeployScriptResponse struct {
	Path string `json:"path"`
	Body string `json:"body"`
	// Exists is false while the site is still on its framework's template, so
	// the editor can say the script is a starting point rather than a saved
	// one.
	Exists bool `json:"exists"`
	// Migrates reports whether this script triggers the pre-deploy database
	// backup, which is worth showing beside the editor: it is the difference
	// between a deploy that can be undone and one that cannot.
	Migrates bool `json:"migrates"`
}

// SiteDeployScriptRequest is a save.
type SiteDeployScriptRequest struct {
	Body string `json:"body"`
	// Backup keeps a copy of what is being replaced. Defaults on, because the
	// mistake this protects against is pasting over a working script.
	Backup bool `json:"backup"`
}

// handleSiteDeployScript serves GET and POST on
// /api/sites/{domain}/deploy-script.
func handleSiteDeployScript(w http.ResponseWriter, r *http.Request, site *config.Site) {
	switch r.Method {
	case http.MethodGet:
		content, err := siteops.ReadDeployScript(site)
		if err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		migrates, _ := siteops.DeployScriptMigrates(site)
		writeJSON(w, SiteDeployScriptResponse{
			Path:     content.Path,
			Body:     content.Body,
			Exists:   content.Exists,
			Migrates: migrates,
		})
	case http.MethodPost:
		var req SiteDeployScriptRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, SiteActionResponse{Error: "reading the script: " + err.Error()})
			return
		}
		res, err := siteops.SaveDeployScript(site, req.Body, req.Backup)
		if err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		if !res.OK {
			writeJSON(w, SiteActionResponse{Error: res.Error})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
