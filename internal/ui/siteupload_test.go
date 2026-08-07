package ui

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// uploadRequest builds the multipart form the panel sends.
func uploadRequest(t *testing.T, fields map[string]string, archive []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if archive != nil {
		part, err := w.CreateFormFile("archive", "site.zip")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(archive); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/sites/upload", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.RemoteAddr = "127.0.0.1:1234"
	return req
}

func postUpload(t *testing.T, fields map[string]string, archive []byte) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	handleSiteUpload(rec, uploadRequest(t, fields, archive))

	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding %q: %v", rec.Body.String(), err)
	}
	return out
}

func testArchive(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range entries {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// The same refusals as the other two sources, because it is the same form.
func TestHandleSiteUpload_RefusesADomainThatIsNotFullyQualified(t *testing.T) {
	panelDirs(t)

	out := postUpload(t, map[string]string{"domain": "myapp", "path": t.TempDir()},
		testArchive(t, map[string]string{"index.php": "<?php"}))

	msg, _ := out["error"].(string)
	if !strings.Contains(msg, "qualified") {
		t.Errorf("error = %q, want the fully-qualified refusal", msg)
	}
}

func TestHandleSiteUpload_RefusesAPHPVersionItCannotServe(t *testing.T) {
	panelDirs(t)

	out := postUpload(t, map[string]string{
		"domain": "example.com", "path": t.TempDir(), "php_version": "8.9",
	}, testArchive(t, map[string]string{"index.php": "<?php"}))

	msg, _ := out["error"].(string)
	if !strings.Contains(msg, "PHP version") {
		t.Errorf("error = %q, want the version refused", msg)
	}
}

func TestHandleSiteUpload_RequiresAnArchive(t *testing.T) {
	panelDirs(t)

	out := postUpload(t, map[string]string{"domain": "example.com", "path": t.TempDir()}, nil)

	if out["error"] == nil {
		t.Fatalf("a request with no archive was accepted: %v", out)
	}
}

// An archive that escapes is refused, and the refusal has to happen before the
// site is registered rather than leaving a site pointed at a half-unpacked
// directory.
func TestHandleSiteUpload_RefusesAnArchiveThatEscapes(t *testing.T) {
	panelDirs(t)
	dir := t.TempDir()

	out := postUpload(t, map[string]string{"domain": "example.com", "path": dir},
		testArchive(t, map[string]string{"../escaped.php": "pwned"}))

	if out["error"] == nil {
		t.Fatalf("an escaping archive was accepted: %v", out)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("the refused upload left %d entries behind", len(entries))
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escaped.php")); err == nil {
		t.Error("the archive wrote outside the site directory")
	}
}

// The directory servlo made for a refused upload goes back, so the retry is not
// blocked by a stub of servlo's own making.
func TestHandleSiteUpload_TakesBackADirectoryItCreatedForARefusedArchive(t *testing.T) {
	panelDirs(t)
	target := filepath.Join(t.TempDir(), "example.com")

	postUpload(t, map[string]string{"domain": "example.com", "path": target},
		testArchive(t, map[string]string{"../escaped.php": "pwned"}))

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("the directory survived a refused upload")
	}
}

func TestHandleSiteUpload_RequiresPost(t *testing.T) {
	panelDirs(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sites/upload", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	handleSiteUpload(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}
