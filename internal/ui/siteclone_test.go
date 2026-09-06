package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func postClone(t *testing.T, handler http.HandlerFunc, path string, body any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(encoded)))
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	handler(rec, req)

	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding %q: %v", rec.Body.String(), err)
	}
	return out
}

// The first step is showing the operator a key to paste, and it has to hand
// back the public half without ever putting the private half on the wire.
func TestHandleDeployKey_ReturnsThePublicHalfOnly(t *testing.T) {
	panelDirs(t)

	out := postClone(t, handleDeployKey, "/api/sites/deploy-key", map[string]any{"domain": "example.com"})

	pub, _ := out["public"].(string)
	if !strings.HasPrefix(pub, "ssh-ed25519 ") {
		t.Fatalf("public = %q, not a public key line", pub)
	}
	// Whatever else the payload carries, it must not carry the private key or
	// even the path to it.
	raw, _ := json.Marshal(out)
	for _, forbidden := range []string{"PRIVATE KEY", "private", "deploy-keys/"} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("the response leaks the private key (%q): %s", forbidden, raw)
		}
	}
}

// Pressing the button twice must not invalidate the key the operator already
// pasted into GitHub.
func TestHandleDeployKey_IsIdempotent(t *testing.T) {
	panelDirs(t)

	first := postClone(t, handleDeployKey, "/api/sites/deploy-key", map[string]any{"domain": "example.com"})
	second := postClone(t, handleDeployKey, "/api/sites/deploy-key", map[string]any{"domain": "example.com"})

	if first["public"] != second["public"] {
		t.Error("a second request minted a different key")
	}
}

func TestHandleDeployKey_RefusesADomainThatIsNotOne(t *testing.T) {
	panelDirs(t)

	out := postClone(t, handleDeployKey, "/api/sites/deploy-key", map[string]any{"domain": "../../etc/passwd"})

	if out["error"] == nil {
		t.Fatalf("a path was accepted as a domain: %v", out)
	}
}

// The clone URL is refused before anything is created, the same way the domain
// is in the folder flow, so a typo leaves no half-made site behind.
func TestHandleSiteClone_RefusesABadRepositoryURL(t *testing.T) {
	panelDirs(t)
	target := filepath.Join(t.TempDir(), "example.com")

	out := postClone(t, handleSiteClone, "/api/sites/clone", map[string]any{
		"domain": "example.com", "path": target, "repository": "not a url",
	})

	if out["error"] == nil {
		t.Fatalf("a bad repository URL was accepted: %v", out)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("the directory was created for a request that was refused")
	}
}

// Cloning into a directory that already holds a project would either fail
// confusingly or overwrite somebody's site, so it is refused with the reason.
func TestHandleSiteClone_RefusesANonEmptyDirectory(t *testing.T) {
	panelDirs(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.php"), []byte("<?php\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := postClone(t, handleSiteClone, "/api/sites/clone", map[string]any{
		"domain": "example.com", "path": dir, "repository": "git@github.com:ServloOfficial/servlo.git",
	})

	msg, _ := out["error"].(string)
	if msg == "" {
		t.Fatalf("cloning over an existing project was accepted: %v", out)
	}
	// Servlo's own words, not git's. Letting the clone run and reporting what
	// git said would also mention "empty", and would mean servlo tried.
	if !strings.Contains(msg, "add the existing project as a folder") {
		t.Errorf("error = %q, want servlo's own guidance rather than git's failure", msg)
	}
	if strings.Contains(msg, "fatal:") {
		t.Errorf("error = %q: git ran, so the refusal came too late", msg)
	}
	if _, err := os.Stat(filepath.Join(dir, "index.php")); err != nil {
		t.Error("the existing project was disturbed")
	}
}

func TestHandleSiteClone_RefusesADomainThatIsNotFullyQualified(t *testing.T) {
	panelDirs(t)

	out := postClone(t, handleSiteClone, "/api/sites/clone", map[string]any{
		"domain": "myapp", "path": t.TempDir(), "repository": "git@github.com:ServloOfficial/servlo.git",
	})

	msg, _ := out["error"].(string)
	if !strings.Contains(msg, "qualified") {
		t.Errorf("error = %q, want the fully-qualified refusal", msg)
	}
}

// The test button is what turns "it did not work" into something actionable,
// so a malformed URL has to say what is wrong with the URL rather than
// reporting a connection failure that never happened.
func TestHandleCloneTest_RefusesABadURLWithoutConnecting(t *testing.T) {
	panelDirs(t)

	out := postClone(t, handleCloneTest, "/api/sites/clone-test", map[string]any{
		"domain": "example.com", "repository": "file:///etc/passwd",
	})

	if out["ok"] == true {
		t.Fatalf("a bad URL tested as OK: %v", out)
	}
	reason, _ := out["reason"].(string)
	if reason == "" {
		reason, _ = out["error"].(string)
	}
	if !strings.Contains(strings.ToLower(reason), "repository url") {
		t.Errorf("reason = %q, does not name the URL as the problem", reason)
	}
}

func TestCloneHandlers_RequirePost(t *testing.T) {
	panelDirs(t)
	for name, handler := range map[string]http.HandlerFunc{
		"deploy-key": handleDeployKey,
		"clone":      handleSiteClone,
		"clone-test": handleCloneTest,
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/sites/"+name, nil)
		req.RemoteAddr = "127.0.0.1:1234"
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", name, rec.Code)
		}
	}
}

// The clone path needs the same rollback as the upload path, for the same
// reason: a failure after the files land leaves them with no site registered,
// and the retry is refused as "not empty" by servlo's own leftovers.
func TestHandleSiteClone_TakesBackADirectoryItCreatedForAFailedClone(t *testing.T) {
	panelDirs(t)
	target := filepath.Join(t.TempDir(), "example.com")

	// A host that cannot resolve, so the clone fails the same way wherever this
	// runs. Naming a real repository would make the test depend on whether the
	// machine running it can reach GitHub and on what that key is allowed to
	// see, which are two things a rollback test is not about.
	out := postClone(t, handleSiteClone, "/api/sites/clone", map[string]any{
		"domain": "example.com", "path": target,
		"repository": "git@nothing.invalid:owner/repo.git",
	})

	if out["error"] == nil {
		t.Skip("the clone succeeded, so there is no failure to roll back")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("the directory servlo created survived a failed clone")
	}
}
