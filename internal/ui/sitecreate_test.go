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
