package ui

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ServloOfficial/servlo/internal/authz"
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/deploy"
	"github.com/ServloOfficial/servlo/internal/siteops"
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
	recordDeploy(r, site, res, err, false)
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

// recordDeployFn is the history write, indirected so a handler test does not
// need a data directory.
var recordDeployFn = deploy.Record

// recordDeploy writes what a deploy did, whether or not it worked.
//
// Failures are recorded too. A history that only remembers the deploys that
// succeeded cannot answer "what happened at 3am", which is the question it gets
// asked. The write is best-effort: losing the record is not worth failing a
// deploy that already ran.
func recordDeploy(r *http.Request, site *config.Site, res deploy.Result, err error, redeploy bool) {
	// Read while the commit is still checked out. By the time anyone reads the
	// history the tree may have moved on, and a subject looked up then would be
	// the wrong one or missing.
	author, subject := deploy.CommitMeta(site.Path, res.ToCommit)
	e := deploy.Entry{
		At:         time.Now().UTC(),
		From:       res.FromCommit,
		To:         res.ToCommit,
		OK:         err == nil,
		Snapshot:   res.Snapshot,
		Kept:       len(res.Kept),
		DurationMS: res.Duration.Milliseconds(),
		Redeploy:   redeploy,
		Author:     author,
		Subject:    subject,
		Actor:      actorOf(r),
	}
	if err != nil {
		e.Error = err.Error()
	}
	_ = recordDeployFn(site, e)
}

// redeployFn is the redeploy itself, indirected the same way the deploy is.
var redeployFn = deploy.Redeploy

// SiteRedeployResponse says whether there is a commit to go back to, and which.
type SiteRedeployResponse struct {
	// Available is false when this site has no recorded deploy to go back from,
	// which is every site before its first one. The button is offered only when
	// there is somewhere to go.
	Available bool   `json:"available"`
	Commit    string `json:"commit,omitempty"`
}

// handleSiteRedeploy serves GET and POST on /api/sites/{domain}/redeploy.
//
// GET answers what a redeploy would do; POST does it. Split so the panel can
// show the target commit before the operator commits to anything, rather than
// offering a button whose effect is only discoverable by pressing it.
func handleSiteRedeploy(w http.ResponseWriter, r *http.Request, site *config.Site) {
	switch r.Method {
	case http.MethodGet:
		commit, ok := deploy.PreviousCommit(site)
		writeJSON(w, SiteRedeployResponse{Available: ok, Commit: commit})
	case http.MethodPost:
		commit, ok := deploy.PreviousCommit(site)
		if !ok {
			writeJSON(w, SiteActionResponse{Error: "this site has no recorded deploy to go back from"})
			return
		}

		release, busyWith, ok := tryAcquireRun(siteRunLockKey(site), "redeploy")
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

		res, err := redeployFn(deploy.Defaults(site, sw), commit)
		recordDeploy(r, site, res, err, true)
		if err != nil {
			done(map[string]any{
				"ok": false, "error": err.Error(),
				"from": res.FromCommit, "to": res.ToCommit,
			})
			return
		}
		done(map[string]any{
			"ok": true, "from": res.FromCommit, "to": res.ToCommit,
			"duration_ms": res.Duration.Milliseconds(),
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// actorOf is the signed-in user who asked for this, empty when the request did
// not come from a session. A webhook deploy has no actor and should say so
// rather than borrow somebody's name.
func actorOf(r *http.Request) string {
	if session, ok := authz.SessionFrom(r.Context()); ok {
		return session.User
	}
	return ""
}

// SiteDeployHistoryResponse is a site's recent deploys, newest first.
type SiteDeployHistoryResponse struct {
	Entries []deploy.Entry `json:"entries"`
}

// handleSiteDeployHistory serves GET on /api/sites/{domain}/deploy-history.
func handleSiteDeployHistory(w http.ResponseWriter, r *http.Request, site *config.Site) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	entries, err := deploy.History(site)
	if err != nil {
		writeJSON(w, SiteActionResponse{Error: err.Error()})
		return
	}
	if entries == nil {
		entries = []deploy.Entry{}
	}
	writeJSON(w, SiteDeployHistoryResponse{Entries: entries})
}
