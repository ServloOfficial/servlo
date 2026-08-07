package appstore

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func releaseZip(t *testing.T, entries map[string]string) []byte {
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

func digestOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// serving returns a test server handing out body, and the URL to reach it.
func serving(t *testing.T, body []byte) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/release.zip"
}

func TestFetchRelease_VerifiesAndExtracts(t *testing.T) {
	archive := releaseZip(t, map[string]string{
		"example-6.7.1/index.php":         "<?php",
		"example-6.7.1/wp-includes/x.php": "<?php",
	})
	src := Source{
		Version: "6.7.1", URL: serving(t, archive), SHA256: digestOf(archive),
		StripPrefix: "example-6.7.1",
	}
	dir := t.TempDir()

	if err := FetchRelease(context.Background(), src, dir); err != nil {
		t.Fatalf("FetchRelease: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "index.php")); err != nil {
		t.Errorf("the release did not land at the site root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "example-6.7.1")); err == nil {
		t.Error("the wrapper directory survived")
	}
}

// The checksum is the whole reason the definition carries one. A release that
// does not match it is a different release, whatever the reason.
func TestFetchRelease_RefusesAReleaseThatDoesNotMatchItsChecksum(t *testing.T) {
	archive := releaseZip(t, map[string]string{"index.php": "<?php"})
	src := Source{
		Version: "6.7.1", URL: serving(t, archive),
		SHA256: digestOf([]byte("something else entirely")),
	}
	dir := t.TempDir()

	err := FetchRelease(context.Background(), src, dir)
	if err == nil {
		t.Fatal("a release that failed its checksum was installed")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Errorf("error = %q, does not name the checksum", err)
	}
	// Nothing of a release that failed verification may reach the site.
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("a release that failed its checksum left %d entries behind", len(entries))
	}
}

// A server that answers with an error page rather than a release would
// otherwise fail its checksum, which is the right outcome by the wrong route
// and a confusing thing to read.
func TestFetchRelease_ReportsAnHTTPFailureAsOne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	err := FetchRelease(context.Background(), Source{
		Version: "1", URL: srv.URL + "/x.zip", SHA256: digestOf(nil),
	}, t.TempDir())

	if err == nil {
		t.Fatal("a 404 was treated as a release")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %q, does not carry the status", err)
	}
}

// A release is bounded like an upload is. The definitions are reviewed, but a
// compromised or mistaken one should not be able to fill the disk before the
// checksum gets a chance to disagree with it.
func TestFetchRelease_StopsAtTheDownloadCeiling(t *testing.T) {
	// A real archive with a correct checksum, so the ceiling is the only thing
	// that can refuse it. The first version of this test used raw bytes, which
	// failed as "not a zip archive" whether the ceiling was there or not.
	archive := releaseZip(t, map[string]string{"index.php": strings.Repeat("A", 4096)})
	src := Source{Version: "1", URL: serving(t, archive), SHA256: digestOf(archive)}

	prev := maxReleaseBytes
	maxReleaseBytes = 64
	t.Cleanup(func() { maxReleaseBytes = prev })

	err := FetchRelease(context.Background(), src, t.TempDir())
	if err == nil {
		t.Fatal("a release over the ceiling was downloaded")
	}
	if !strings.Contains(err.Error(), "larger than") {
		t.Errorf("error = %q, does not say the release is too large", err)
	}
}

// The site directory has to be empty: an app install creates the site, it does
// not land on top of one.
func TestFetchRelease_RefusesANonEmptyDirectory(t *testing.T) {
	archive := releaseZip(t, map[string]string{"index.php": "<?php"})
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "existing.php"), []byte("<?php"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := FetchRelease(context.Background(),
		Source{Version: "1", URL: serving(t, archive), SHA256: digestOf(archive)}, dir)
	if err == nil {
		t.Fatal("a release was unpacked over an existing project")
	}
	if body, _ := os.ReadFile(filepath.Join(dir, "existing.php")); string(body) != "<?php" {
		t.Error("the existing file was overwritten")
	}
}

// The strip prefix comes from the definition and is compared against names in
// the archive, so a definition naming the wrong one must say so rather than
// silently extracting the wrapper it failed to strip.
func TestFetchRelease_RefusesAStripPrefixTheArchiveDoesNotHave(t *testing.T) {
	archive := releaseZip(t, map[string]string{"example-6.7.1/index.php": "<?php"})
	src := Source{
		Version: "6.7.1", URL: serving(t, archive), SHA256: digestOf(archive),
		StripPrefix: "example-9.9.9",
	}

	err := FetchRelease(context.Background(), src, t.TempDir())
	if err == nil {
		t.Fatal("a definition naming a prefix the archive lacks was accepted")
	}
	if !strings.Contains(err.Error(), "example-9.9.9") {
		t.Errorf("error = %q, does not name the prefix it could not find", err)
	}
}
