package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/realrashid/servlo/internal/cli"
	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/deploykey"
	"github.com/realrashid/servlo/internal/linker"
	"github.com/realrashid/servlo/internal/siteops"
)

// Adding a site by cloning a repository.
//
// Three requests rather than one, because the middle one is the whole point.
// Servlo mints a key, the operator pastes it into the repository's deploy keys,
// and then presses test: without that step a clone of a private repository
// fails with "Permission denied (publickey)" and there is nothing on screen
// explaining that a key needed pasting anywhere.

// DeployKeyRequest asks for the key a site will clone with.
type DeployKeyRequest struct {
	Domain string `json:"domain"`
}

// CloneRequest is the body of both the connection test and the clone itself.
// They take the same fields so the form can send what it has either way.
type CloneRequest struct {
	Domain     string `json:"domain"`
	Path       string `json:"path"`
	Repository string `json:"repository"`
	PHPVersion string `json:"php_version"`
	PublicDir  string `json:"public_dir"`
}

// handleDeployKey answers POST /api/sites/deploy-key with the site's public
// deploy key, generating it the first time.
//
// The private half never appears in the response, or in the path field of any
// struct that gets marshalled here. It exists on disk 0600 and is named to ssh
// by the process that runs it, and that is the only place it goes.
func handleDeployKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req DeployKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, map[string]any{"error": "invalid request body"})
		return
	}
	domain, err := siteops.NormalizeDomain(req.Domain)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	key, err := deploykey.Ensure(domain)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "public": key.Public})
}

// handleCloneTest answers POST /api/sites/clone-test by asking the host whether
// it knows this site's deploy key.
func handleCloneTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req CloneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, map[string]any{"error": "invalid request body"})
		return
	}
	domain, err := siteops.NormalizeDomain(req.Domain)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "reason": err.Error()})
		return
	}
	target, err := deploykey.NormalizeCloneURL(req.Repository)
	if err != nil {
		// Named as a URL problem rather than a connection one, because
		// reporting a connection failure that never happened is exactly the
		// generic error this flow exists to avoid.
		writeJSON(w, map[string]any{"ok": false, "reason": "that repository URL is not one servlo can clone: " + err.Error()})
		return
	}
	key, err := deploykey.Ensure(domain)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "reason": err.Error()})
		return
	}
	writeJSON(w, deploykey.TestConnection(r.Context(), key, target))
}

// handleSiteClone answers POST /api/sites/clone: clone the repository, then
// register what landed as a site.
func handleSiteClone(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req CloneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, SiteCreateResponse{Error: "invalid request body"})
		return
	}

	// Everything refusable is refused before anything is created or fetched.
	domain, err := siteops.NormalizeDomain(req.Domain)
	if err != nil {
		writeJSON(w, SiteCreateResponse{Error: err.Error()})
		return
	}
	if owner, err := config.IsDomainUsed(domain); err == nil && owner != nil {
		writeJSON(w, SiteCreateResponse{Error: fmt.Sprintf("%s is already served by the site %q", domain, owner.Name)})
		return
	}
	target, err := deploykey.NormalizeCloneURL(req.Repository)
	if err != nil {
		writeJSON(w, SiteCreateResponse{Error: err.Error()})
		return
	}
	if req.Path == "" {
		writeJSON(w, SiteCreateResponse{Error: "a site needs a directory to clone into"})
		return
	}
	// A clone into a directory holding a project would either fail confusingly
	// or land on top of somebody's site, so it is refused with the reason.
	if entries, err := os.ReadDir(req.Path); err == nil && len(entries) > 0 {
		writeJSON(w, SiteCreateResponse{Error: "the directory is not empty: clone into an empty directory, or add the existing project as a folder instead"})
		return
	}

	prepared, err := siteops.PrepareSiteDirectory(req.Path)
	if err != nil {
		writeJSON(w, SiteCreateResponse{Error: err.Error()})
		return
	}
	key, err := deploykey.Ensure(domain)
	if err != nil {
		writeJSON(w, SiteCreateResponse{Error: err.Error()})
		return
	}
	if err := deploykey.Clone(r.Context(), key, target, prepared.Path, nil); err != nil {
		// A clone that failed leaves a directory servlo made a moment ago and
		// nothing else, so take it back rather than leaving a stub the retry
		// would then refuse as non-empty.
		if prepared.Created {
			_ = os.RemoveAll(prepared.Path)
		}
		writeJSON(w, SiteCreateResponse{Error: err.Error()})
		return
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		writeJSON(w, SiteCreateResponse{Error: "reading the servlo config: " + err.Error()})
		return
	}
	policy := linker.PanelPolicy(domain)
	plan, err := linker.Resolve(prepared.Path, cfg, policy)
	if err != nil {
		writeJSON(w, SiteCreateResponse{Error: err.Error()})
		return
	}
	if req.PHPVersion != "" {
		plan.Site.PHPVersion = req.PHPVersion
	}
	if req.PublicDir != "" {
		plan.Site.PublicDir = req.PublicDir
	}
	res, err := linker.Apply(plan, policy, cli.LinkDeps(), nil)
	if err != nil {
		writeJSON(w, SiteCreateResponse{Error: err.Error()})
		return
	}

	writeJSON(w, SiteCreateResponse{
		OK:         true,
		Domain:     res.Site.PrimaryDomain(),
		Name:       res.Site.Name,
		Path:       res.Site.Path,
		Framework:  res.Site.Framework,
		PHPVersion: res.Site.PHPVersion,
		PublicDir:  res.Site.PublicDir,
		Created:    prepared.Created,
	})
}
