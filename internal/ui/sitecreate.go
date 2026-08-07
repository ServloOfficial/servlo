package ui

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/realrashid/servlo/internal/cli"
	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/linker"
	"github.com/realrashid/servlo/internal/siteops"
)

// Adding a site from the panel.
//
// The inherited flow shelled out to the servlo binary with the browser's chosen
// directory as the working directory and let `servlo link` derive everything.
// That worked when a site's domain came from its directory name plus a TLD
// servlo resolved itself. It cannot work here: the domain is given, never
// derived, so it has to reach the linker as a value rather than as an argv the
// CLI parses back out.
//
// So the panel resolves and applies a plan in process, under a policy that says
// what a click may consent to. Same linker, same registration, same vhost as
// the CLI takes; only the policy and the reporting differ.

// SiteCreateRequest is the body of POST /api/sites/create.
type SiteCreateRequest struct {
	Domain string `json:"domain"`
	Path   string `json:"path"`
	// PHPVersion overrides the detected one. Empty takes what detection chose.
	PHPVersion string `json:"php_version"`
	// PublicDir overrides the detected document root, relative to Path.
	PublicDir string `json:"public_dir"`
}

// SiteCreateResponse reports what was registered, so the panel can select the
// new site without reloading the whole list to find it.
type SiteCreateResponse struct {
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
	Domain     string `json:"domain,omitempty"`
	Name       string `json:"name,omitempty"`
	Path       string `json:"path,omitempty"`
	Framework  string `json:"framework,omitempty"`
	PHPVersion string `json:"php_version,omitempty"`
	PublicDir  string `json:"public_dir,omitempty"`
	// Created says the site directory did not exist and was made, which the
	// panel says out loud: an empty directory serving a 403 is otherwise a
	// confusing first impression.
	Created bool `json:"created,omitempty"`
	// Warning carries something that did not stop the site being registered.
	Warning string `json:"warning,omitempty"`
}

// handleSiteInspect answers GET /api/sites/inspect?path=… with what the add-site
// form should prefill. It creates nothing, including when the path is not there
// yet, because the operator is allowed to name a directory that does not exist.
func handleSiteInspect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path parameter required", http.StatusBadRequest)
		return
	}
	report, err := siteops.InspectSiteDirectory(path)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, report)
}

// handleSiteCreate registers a new site from POST /api/sites/create.
func handleSiteCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req SiteCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, SiteCreateResponse{Error: "invalid request body"})
		return
	}

	// Everything that can be refused is refused before anything is created, so
	// a rejected request leaves no directory behind for the operator to wonder
	// about on the retry.
	domain, err := siteops.NormalizeDomain(req.Domain)
	if err != nil {
		writeJSON(w, SiteCreateResponse{Error: err.Error()})
		return
	}
	if owner, err := config.IsDomainUsed(domain); err == nil && owner != nil {
		writeJSON(w, SiteCreateResponse{Error: fmt.Sprintf("%s is already served by the site %q", domain, owner.Name)})
		return
	}
	if req.Path == "" {
		writeJSON(w, SiteCreateResponse{Error: "a site needs a directory to serve"})
		return
	}

	prepared, err := siteops.PrepareSiteDirectory(req.Path)
	if err != nil {
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
	// The form's choices win over detection, because the operator looked at
	// what detection proposed and changed it on purpose.
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

	resp := SiteCreateResponse{
		OK:         true,
		Domain:     res.Site.PrimaryDomain(),
		Name:       res.Site.Name,
		Path:       res.Site.Path,
		Framework:  res.Site.Framework,
		PHPVersion: res.Site.PHPVersion,
		PublicDir:  res.Site.PublicDir,
		Created:    prepared.Created,
	}
	// An empty directory is registered and served, and serving nothing is the
	// correct outcome of asking for it. Saying so beats a blank page.
	if prepared.Created || !prepared.HasContent {
		resp.Warning = "the site directory is empty, so it will serve nothing until you put a project in it"
	}
	writeJSON(w, resp)
}
