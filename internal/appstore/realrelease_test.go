package appstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/realrashid/servlo/internal/siteops"
)

// The definition checked against the artefact it actually pins, rather than a
// three-file archive built in memory. Skipped when the release is not on disk,
// so it costs nothing in CI and proves something real when run beside a copy.
//
// Fetch it by taking the url out of the definition being checked, so this
// comment does not name an app and go stale when the pin moves:
//
//	curl -o /tmp/servlo-app-release.zip "$(grep -h '  url:' stores/apps/*.yaml | head -1 | cut -d' ' -f4)"
func TestRealRelease_MatchesItsDefinitionAndUnpacks(t *testing.T) {
	body, err := os.ReadFile("/tmp/servlo-app-release.zip")
	if err != nil {
		t.Skip("no local copy of the release; see the comment above")
	}

	apps, err := List()
	if err != nil {
		t.Fatal(err)
	}
	// Whichever definition the local copy actually is, found by its checksum.
	// Naming one here would tie the test to an app and to a version.
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	var app App
	for _, a := range apps {
		if a.Source.SHA256 == digest {
			app = a
		}
	}
	if app.Name == "" {
		t.Skipf("the local copy (%s) is not a release any definition pins", digest[:12])
	}

	dir := t.TempDir()
	res, err := siteops.Unzip(bytes.NewReader(body), int64(len(body)), dir, siteops.UnzipOptions{
		MaxBytes: maxReleaseBytes, MaxFiles: releaseMaxFiles, StripPrefix: app.Source.StripPrefix,
	})
	if err != nil {
		t.Fatalf("the real release does not unpack: %v", err)
	}
	t.Logf("unpacked %d files, %d bytes, unwrapped=%v", res.Files, res.Bytes, res.Unwrapped)

	if !res.Unwrapped {
		t.Error("the declared strip_prefix did not match the real archive")
	}
	// The document root has to be where the vhost will look for it.
	for _, want := range []string{"index.php", "wp-includes", "wp-admin"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("%s is not at the site root after unpacking: %v", want, err)
		}
	}
	// The config file the definition writes must not already be in the release,
	// or the exclusive open would fail on every install.
	if _, err := os.Stat(filepath.Join(dir, app.ConfigFile.Path)); err == nil {
		t.Errorf("the release already ships %s, which the install would refuse to overwrite", app.ConfigFile.Path)
	}
	// The real entry count against the ceiling the definition runs under.
	if res.Files > releaseMaxFiles {
		t.Errorf("the real release has %d files, over the %d ceiling", res.Files, releaseMaxFiles)
	}
}
