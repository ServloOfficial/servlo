package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/linker"
	"github.com/realrashid/servlo/internal/siteops"
)

// panelDirs isolates the registry and data dir the way registerSite does, for
// tests that add a site rather than starting from one.
func panelDirs(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

func postJSON(t *testing.T, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(encoded)))
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	handleSiteCreate(rec, req)
	return rec
}

func decodeCreate(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding %q: %v", rec.Body.String(), err)
	}
	return out
}

// The form's first move is asking about a path, and it has to work for a path
// that is not there yet or the operator cannot name a new directory.
func TestHandleSiteInspect_DescribesAPathBeforeItExists(t *testing.T) {
	panelDirs(t)
	target := filepath.Join(t.TempDir(), "example.com")

	req := httptest.NewRequest(http.MethodGet, "/api/sites/inspect?path="+target, nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	handleSiteInspect(rec, req)

	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding %q: %v", rec.Body.String(), err)
	}
	if out["exists"] != false {
		t.Errorf("exists = %v, want false", out["exists"])
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("inspecting created the directory")
	}
}

func TestHandleSiteInspect_RequiresAPath(t *testing.T) {
	panelDirs(t)
	req := httptest.NewRequest(http.MethodGet, "/api/sites/inspect", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	handleSiteInspect(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// Servlo has no TLD to complete a bare name with, and completing one is exactly
// what this replaces, so a name that is not fully qualified is refused rather
// than turned into something that resolves nowhere.
func TestHandleSiteCreate_RefusesADomainThatIsNotFullyQualified(t *testing.T) {
	panelDirs(t)
	dir := t.TempDir()

	rec := postJSON(t, "/api/sites/create", map[string]any{"domain": "myapp", "path": dir})

	out := decodeCreate(t, rec)
	if out["error"] == nil {
		t.Fatalf("a bare label was accepted: %v", out)
	}
	if !strings.Contains(out["error"].(string), "qualified") {
		t.Errorf("error = %q, does not explain what is wrong", out["error"])
	}
}

func TestHandleSiteCreate_RefusesADomainAnotherSiteOwns(t *testing.T) {
	panelDirs(t)
	if err := config.AddSite(config.Site{Name: "taken", Path: t.TempDir(), Domains: []string{"example.com"}}); err != nil {
		t.Fatal(err)
	}

	rec := postJSON(t, "/api/sites/create", map[string]any{"domain": "example.com", "path": t.TempDir()})

	out := decodeCreate(t, rec)
	if out["error"] == nil {
		t.Fatalf("a domain already in use was accepted: %v", out)
	}
}

func TestHandleSiteCreate_RefusesARelativePath(t *testing.T) {
	panelDirs(t)

	rec := postJSON(t, "/api/sites/create", map[string]any{"domain": "example.com", "path": "sites/example.com"})

	out := decodeCreate(t, rec)
	if out["error"] == nil {
		t.Fatalf("a relative path was accepted: %v", out)
	}
}

// The directory the form named did not exist, and creating it is half of what
// the story asks for. A refusal that leaves it created would be worse than
// either outcome, so the failure path is checked too.
func TestHandleSiteCreate_DoesNotCreateTheDirectoryForARefusedDomain(t *testing.T) {
	panelDirs(t)
	target := filepath.Join(t.TempDir(), "example.com")

	postJSON(t, "/api/sites/create", map[string]any{"domain": "myapp", "path": target})

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("the directory was created for a request that was refused")
	}
}

func TestHandleSiteCreate_RequiresPost(t *testing.T) {
	panelDirs(t)
	req := httptest.NewRequest(http.MethodGet, "/api/sites/create", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	handleSiteCreate(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

// The form's overrides win over detection, which is the point of having them,
// so a value the rest of the stack cannot use has to be refused here rather
// than stored and quietly ignored.
//
// The PHP version reaches the generated vhost as the FPM upstream's name, and
// nothing between the form and that template was checking it. A version with a
// newline and a brace in it is nginx configuration written through an add-site
// form.
func TestHandleSiteCreate_RefusesAPHPVersionItCannotServe(t *testing.T) {
	for _, bad := range []string{"8.9", "nonsense", "x;}\nserver { listen 80; }", "../../etc", "8.4; rm -rf /"} {
		panelDirs(t)
		rec := postJSON(t, "/api/sites/create", map[string]any{
			"domain": "example.com", "path": t.TempDir(), "php_version": bad,
		})
		out := decodeCreate(t, rec)
		msg, _ := out["error"].(string)
		// Named specifically. Any other error means the request got further
		// than it should have and failed for an unrelated reason.
		if !strings.Contains(msg, "PHP version") {
			t.Errorf("php_version %q: error = %q, want it refused as an unusable PHP version", bad, msg)
		}
	}
}

// A document root that escapes the project is dropped by the vhost writer and
// replaced with "public", so the site would be registered claiming a root it
// does not serve from. Saying no beats silently serving something else.
func TestHandleSiteCreate_RefusesADocumentRootThatEscapesTheSite(t *testing.T) {
	for _, bad := range []string{"../../etc", "/etc", "~/secrets", "a/../../b"} {
		panelDirs(t)
		rec := postJSON(t, "/api/sites/create", map[string]any{
			"domain": "example.com", "path": t.TempDir(), "public_dir": bad,
		})
		msg, _ := decodeCreate(t, rec)["error"].(string)
		if !strings.Contains(msg, "document root") {
			t.Errorf("public_dir %q: error = %q, want it refused as a document root", bad, msg)
		}
	}
}

// Both overrides are refused on the clone path too: it is the same form.
func TestHandleSiteClone_RefusesTheSameBadOverrides(t *testing.T) {
	panelDirs(t)
	out := postClone(t, handleSiteClone, "/api/sites/clone", map[string]any{
		"domain": "example.com", "path": t.TempDir(),
		"repository": "git@github.com:realrashid/servlo.git", "php_version": "8.9",
	})
	msg, _ := out["error"].(string)
	if !strings.Contains(msg, "PHP version") {
		t.Errorf("error = %q, want the clone path to refuse the version too", msg)
	}
}

// The accepting half is tested against applyOverrides rather than the handler.
//
// Driving the handler all the way through would register the site for real,
// and on a machine that has podman that means building a PHP image: the first
// version of these two tests took the whole package past Go's ten-minute
// timeout on CI. The refusals can go through the handler because they return
// before any of that; the acceptances cannot.
func TestCheckOverrides_TakesWhatTheOperatorChose(t *testing.T) {
	plan := &linker.Plan{}

	chosen, err := checkOverrides("8.4", "public")
	if err != nil {
		t.Fatalf("checkOverrides: %v", err)
	}
	chosen.apply(plan)
	if plan.Site.PHPVersion != "8.4" {
		t.Errorf("PHPVersion = %q, want 8.4", plan.Site.PHPVersion)
	}
	if plan.Site.PublicDir != "public" {
		t.Errorf("PublicDir = %q, want public", plan.Site.PublicDir)
	}
}

// Normalised rather than merely checked, so the spellings an operator actually
// types mean what they obviously mean.
func TestCheckOverrides_NormalisesThePHPVersion(t *testing.T) {
	for _, in := range []string{"php8.4", "PHP 8.4", "8.4.7", "84"} {
		plan := &linker.Plan{}
		chosen, err := checkOverrides(in, "")
		if err != nil {
			t.Errorf("checkOverrides(%q): %v", in, err)
			continue
		}
		chosen.apply(plan)
		if plan.Site.PHPVersion != "8.4" {
			t.Errorf("checkOverrides(%q) gave %q, want 8.4", in, plan.Site.PHPVersion)
		}
	}
}

// An empty override is the form saying "keep what detection chose", so it must
// not overwrite the plan with an empty string.
func TestCheckOverrides_LeavesDetectionAloneWhenTheFormSaidNothing(t *testing.T) {
	plan := &linker.Plan{}
	plan.Site.PHPVersion = "8.3"
	plan.Site.PublicDir = "web"

	chosen, err := checkOverrides("", "")
	if err != nil {
		t.Fatalf("checkOverrides: %v", err)
	}
	chosen.apply(plan)
	if plan.Site.PHPVersion != "8.3" || plan.Site.PublicDir != "web" {
		t.Errorf("an empty override overwrote detection: %+v", plan.Site)
	}
}

// rollback is what stops a failure after the files land from leaving servlo's
// own work in the way of the retry. Both branches, directly, because reaching
// them through a handler means registering a site for real.
func TestRollback_RemovesADirectoryServloCreated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "example.com")
	if err := os.MkdirAll(filepath.Join(dir, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}

	rollback(siteops.PrepareResult{Path: dir, Created: true})

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("a directory servlo created survived the rollback")
	}
}

// A directory the operator made is emptied and left standing: taking it would
// be removing something they chose to have.
func TestRollback_EmptiesADirectoryTheOperatorMade(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "vendor", "acme"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.php"), []byte("<?php"), 0o644); err != nil {
		t.Fatal(err)
	}

	rollback(siteops.PrepareResult{Path: dir})

	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("the operator's directory was removed: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("the rollback left %d entries behind", len(entries))
	}
}
