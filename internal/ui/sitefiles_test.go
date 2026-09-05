package ui

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/sitefs"
)

// filesSite registers a site with the given contents and returns its directory.
func filesSite(t *testing.T, files map[string]string) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	for name, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := config.AddSite(config.Site{Name: "acme", Path: dir, Domains: []string{"acme.com"}}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func filesRequest(t *testing.T, method, target string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, body)
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	return rec
}

func decodeFiles[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding %q: %v", rec.Body.String(), err)
	}
	return out
}

func TestHandleFilesList_ReturnsTheSiteRootAndItsEntries(t *testing.T) {
	filesSite(t, map[string]string{"index.php": "<?php", "app/User.php": "<?php"})

	rec := filesRequest(t, http.MethodGet, "/api/sites/acme.com/files", nil)

	res := decodeFiles[filesListResponse](t, rec)
	if res.Error != "" {
		t.Fatalf("error = %q", res.Error)
	}
	if len(res.Entries) != 2 || res.Entries[0].Name != "app" {
		t.Fatalf("entries = %+v", res.Entries)
	}
	if res.Root == "" {
		t.Fatal("the listing does not say which directory it is")
	}
}

// The refusal an operator sees names no absolute path. A message that reported
// what it refused would map the filesystem one request at a time.
func TestHandleFiles_RefusesTraversalWithoutDescribingTheFilesystem(t *testing.T) {
	dir := filesSite(t, map[string]string{"index.php": "<?php"})
	outside := filepath.Join(filepath.Dir(dir), "secrets.txt")
	if err := os.WriteFile(outside, []byte("top secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, target := range []string{
		"/api/sites/acme.com/files?path=" + url.QueryEscape("../"),
		"/api/sites/acme.com/files/content?path=" + url.QueryEscape("../secrets.txt"),
		"/api/sites/acme.com/files/content?path=" + url.QueryEscape("/etc/passwd"),
		"/api/sites/acme.com/files/content?path=" + url.QueryEscape("app/../../secrets.txt"),
	} {
		rec := filesRequest(t, http.MethodGet, target, nil)
		// The site's own directory is shown to the operator on purpose; what a
		// refusal must not do is name anything above it or hand back what it
		// refused to read.
		message := decodeFiles[struct {
			Error string `json:"error"`
			Text  string `json:"text"`
		}](t, rec)
		if !strings.Contains(message.Error, "outside the site directory") {
			t.Fatalf("%s returned %q, want a refusal", target, rec.Body.String())
		}
		if message.Text != "" || strings.Contains(message.Error, filepath.Dir(dir)) {
			t.Fatalf("%s leaked something: %q", target, rec.Body.String())
		}
	}
}

func TestHandleFilesContent_ReadsAndSaves(t *testing.T) {
	dir := filesSite(t, map[string]string{"index.php": "<?php echo 1;"})

	rec := filesRequest(t, http.MethodGet, "/api/sites/acme.com/files/content?path=index.php", nil)
	content := decodeFiles[sitefs.Content](t, rec)
	if content.Text != "<?php echo 1;" {
		t.Fatalf("content = %+v", content)
	}

	save, _ := json.Marshal(filesSaveRequest{Path: "index.php", Content: "<?php echo 2;"})
	rec = filesRequest(t, http.MethodPut, "/api/sites/acme.com/files/content", bytes.NewReader(save))
	if res := decodeFiles[filesResponse](t, rec); !res.OK {
		t.Fatalf("save = %+v", res)
	}
	saved, err := os.ReadFile(filepath.Join(dir, "index.php"))
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != "<?php echo 2;" {
		t.Fatalf("file = %q", saved)
	}
}

func TestHandleFilesContent_RefusesToSaveOutsideTheSite(t *testing.T) {
	dir := filesSite(t, map[string]string{"index.php": "<?php"})

	save, _ := json.Marshal(filesSaveRequest{Path: "../planted.php", Content: "<?php system($_GET[0]);"})
	rec := filesRequest(t, http.MethodPut, "/api/sites/acme.com/files/content", bytes.NewReader(save))

	if res := decodeFiles[filesResponse](t, rec); res.OK {
		t.Fatal("a save above the site root was accepted")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "planted.php")); !os.IsNotExist(err) {
		t.Fatal("the file was written outside the site")
	}
}

func TestHandleFilesUpload_LandsInTheChosenDirectory(t *testing.T) {
	dir := filesSite(t, map[string]string{"public/keep": "x"})

	body, contentType := multipartUpload(t, "public", "app.js", "console.log(1)")
	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.com/files/upload", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	if res := decodeFiles[filesResponse](t, rec); !res.OK {
		t.Fatalf("upload = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "public", "app.js")); err != nil {
		t.Fatal(err)
	}
}

// A browser sends whatever the form said the filename was, and a form can say
// anything at all.
func TestHandleFilesUpload_RefusesAFilenameThatIsAPath(t *testing.T) {
	dir := filesSite(t, map[string]string{"public/keep": "x"})

	body, contentType := multipartUpload(t, "public", "../../planted.php", "<?php")
	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.com/files/upload", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "planted.php")); !os.IsNotExist(err) {
		t.Fatal("the upload escaped the site")
	}
	// The base name is all that survives, so the file lands in the directory
	// that was actually chosen.
	if _, err := os.Stat(filepath.Join(dir, "public", "planted.php")); err != nil {
		t.Fatalf("the upload should have landed under its base name: %v", err)
	}
}

func TestHandleFilesUnzip_ExtractsBesideTheArchive(t *testing.T) {
	dir := filesSite(t, map[string]string{"plugins/keep": "x"})
	writeZip(t, filepath.Join(dir, "plugins", "acme.zip"), map[string]string{"acme/acme.php": "<?php"})

	body, _ := json.Marshal(filesUnzipRequest{Path: "plugins/acme.zip"})
	rec := filesRequest(t, http.MethodPost, "/api/sites/acme.com/files/unzip", bytes.NewReader(body))

	res := decodeFiles[filesUnzipResponse](t, rec)
	if !res.OK || res.Files != 1 {
		t.Fatalf("unzip = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "plugins", "acme", "acme.php")); err != nil {
		t.Fatal(err)
	}
}

func TestHandleFilesUnzip_RefusesAnArchiveThatClimbsOut(t *testing.T) {
	dir := filesSite(t, map[string]string{"plugins/keep": "x"})
	writeZip(t, filepath.Join(dir, "plugins", "evil.zip"), map[string]string{"../../planted.php": "<?php"})

	body, _ := json.Marshal(filesUnzipRequest{Path: "plugins/evil.zip"})
	rec := filesRequest(t, http.MethodPost, "/api/sites/acme.com/files/unzip", bytes.NewReader(body))

	if res := decodeFiles[filesUnzipResponse](t, rec); res.OK {
		t.Fatal("zip slip was accepted")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "planted.php")); !os.IsNotExist(err) {
		t.Fatal("the archive escaped the site")
	}
}

func TestHandleFilesPermissions_PlansBeforeItApplies(t *testing.T) {
	dir := filesSite(t, map[string]string{"index.php": "<?php", ".env": "APP_KEY=x"})
	if err := os.Chmod(filepath.Join(dir, ".env"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := filesRequest(t, http.MethodGet, "/api/sites/acme.com/files/permissions", nil)
	plan := decodeFiles[filesPermissionsResponse](t, rec)
	if !plan.OK || plan.Changes == 0 {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.Applied != 0 {
		t.Fatal("asking for the plan changed something")
	}
	info, err := os.Stat(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatal("the plan chmodded a file")
	}

	rec = filesRequest(t, http.MethodPost, "/api/sites/acme.com/files/permissions", nil)
	applied := decodeFiles[filesPermissionsResponse](t, rec)
	if !applied.OK || applied.Applied == 0 {
		t.Fatalf("apply = %+v", applied)
	}
	info, err = os.Stat(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf(".env mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestHandleFilesDelete_RemovesAFileAndRefusesTheRoot(t *testing.T) {
	dir := filesSite(t, map[string]string{"old.php": "<?php"})

	rec := filesRequest(t, http.MethodDelete, "/api/sites/acme.com/files/entry?path=old.php", nil)
	if res := decodeFiles[filesResponse](t, rec); !res.OK {
		t.Fatalf("delete = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "old.php")); !os.IsNotExist(err) {
		t.Fatal("the file is still there")
	}

	rec = filesRequest(t, http.MethodDelete, "/api/sites/acme.com/files/entry?path=", nil)
	if res := decodeFiles[filesResponse](t, rec); res.OK {
		t.Fatal("deleting the site root was accepted")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("the site directory was removed")
	}
}

func TestFilesRoutes_RefuseTheWrongMethod(t *testing.T) {
	filesSite(t, map[string]string{"index.php": "<?php"})

	for _, c := range []struct{ method, target string }{
		{http.MethodPost, "/api/sites/acme.com/files"},
		{http.MethodGet, "/api/sites/acme.com/files/upload"},
		{http.MethodGet, "/api/sites/acme.com/files/unzip"},
		{http.MethodGet, "/api/sites/acme.com/files/entry?path=index.php"},
		{http.MethodDelete, "/api/sites/acme.com/files/permissions"},
	} {
		rec := filesRequest(t, c.method, c.target, nil)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s = %d, want 405", c.method, c.target, rec.Code)
		}
	}
}

func multipartUpload(t *testing.T, dir, filename, body string) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("path", dir); err != nil {
		t.Fatal(err)
	}
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
