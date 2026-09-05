package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/ServloOfficial/servlo/internal/cli"
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/linker"
	"github.com/ServloOfficial/servlo/internal/siteops"
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
	chosen, err := checkOverrides(req.PHPVersion, req.PublicDir)
	if err != nil {
		writeJSON(w, SiteCreateResponse{Error: err.Error()})
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
	chosen.apply(plan)

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

// overrides are the form's PHP version and document root, checked.
//
// They are checked up front, before a directory is made or a repository is
// fetched, because a refusal after four seconds of network is a refusal that
// wasted four seconds and left something to clear up. Applying them has to wait
// for the plan, which is why validating and applying are two steps.
type overrides struct {
	phpVersion string
	publicDir  string
}

// checkOverrides validates what the form sent.
//
// The form's choices win over detection, because the operator looked at what
// detection proposed and changed it on purpose. That is exactly why they have
// to be checked: nothing downstream treats them as untrusted. The PHP version
// becomes the FPM upstream's name in the generated vhost, and a document root
// the vhost writer rejects is silently replaced with "public", which registers
// a site claiming a root it does not serve from.
func checkOverrides(phpVersion, publicDir string) (overrides, error) {
	var o overrides
	if phpVersion != "" {
		// Normalised rather than merely checked, so "php8.4" and "8.4.7" mean
		// what the operator obviously meant.
		normalised, err := config.NormalizePHPVersion(phpVersion)
		if err != nil {
			return o, fmt.Errorf("that is not a PHP version servlo can serve: %w", err)
		}
		o.phpVersion = normalised
	}
	if publicDir != "" {
		if err := config.ValidatePublicDir(publicDir); err != nil {
			return o, fmt.Errorf("that is not a usable document root: it must be a directory inside the site, and %w", err)
		}
		o.publicDir = publicDir
	}
	return o, nil
}

// apply puts the checked overrides onto the plan. An empty field is the form
// saying "keep what detection chose", so it leaves the plan alone.
func (o overrides) apply(plan *linker.Plan) {
	if o.phpVersion != "" {
		plan.Site.PHPVersion = o.phpVersion
	}
	if o.publicDir != "" {
		plan.Site.PublicDir = o.publicDir
	}
}

// rollback undoes what a half-finished clone or upload left on disk.
//
// Both of those flows require the directory to be empty before they start, and
// they check it, so everything in it afterwards was put there by servlo
// seconds ago. That is what makes emptying it safe: there is nothing of the
// operator's in there to lose.
//
// Without it, a failure after the files land, the linker refusing, the vhost
// failing to write, leaves a directory full of servlo's work and no site
// registered. The retry is then refused as "not empty" by servlo's own
// leftovers, and the operator has to clear up by hand a directory they never
// touched.
//
// A directory servlo created goes entirely; one the operator made is emptied
// and left standing, because taking it would be removing something they chose
// to have.
func rollback(prepared siteops.PrepareResult) {
	if prepared.Created {
		_ = os.RemoveAll(prepared.Path)
		return
	}
	entries, err := os.ReadDir(prepared.Path)
	if err != nil {
		return
	}
	for _, e := range entries {
		_ = os.RemoveAll(filepath.Join(prepared.Path, e.Name()))
	}
}
