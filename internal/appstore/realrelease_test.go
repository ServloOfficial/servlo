package appstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/realrashid/servlo/internal/siteops"
	"github.com/realrashid/servlo/stores"
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

	// Only when the definition claims one. A release laid out flat declares no
	// strip_prefix, and asserting an unwrap there would fail every archive that
	// is already at the right level.
	if app.Source.StripPrefix != "" && !res.Unwrapped {
		t.Error("the declared strip_prefix did not match the real archive")
	}
	// The document root has to be where the vhost will look for it, and where
	// the vhost looks is the framework's public_dir, not anything the app says.
	// A project that publishes a release archive laid out differently from the
	// project layout its framework definition describes gives a site whose
	// document root is a directory that is not there, and the symptom is a 404
	// on a site that installed without complaint.
	docRoot := dir
	if pub := frameworkPublicDir(t, app.Framework); pub != "" && pub != "." {
		docRoot = filepath.Join(dir, pub)
	}
	if _, err := os.Stat(filepath.Join(docRoot, "index.php")); err != nil {
		t.Errorf("the release has no index.php where the %s vhost will look for it: %v", app.Framework, err)
	}
	// The config file the definition writes must not already be in the release,
	// or the exclusive open would fail on every install. Guarded, because an app
	// that writes none joins an empty path onto the site directory, which always
	// exists and so always reads as a collision.
	if app.ConfigFile.Path != "" {
		if _, err := os.Stat(filepath.Join(dir, app.ConfigFile.Path)); err == nil {
			t.Errorf("the release already ships %s, which the install would refuse to overwrite", app.ConfigFile.Path)
		}
	}
	// The real entry count against the ceiling the definition runs under.
	if res.Files > releaseMaxFiles {
		t.Errorf("the real release has %d files, over the %d ceiling", res.Files, releaseMaxFiles)
	}
}

// frameworkPublicDir returns the document root the framework store declares for
// the framework's newest version, which is the one a fresh install lands on.
func frameworkPublicDir(t *testing.T, framework string) string {
	t.Helper()
	data, ok := stores.Read(stores.Frameworks, "index.json")
	if !ok {
		t.Fatal("the framework store has no index")
	}
	var index struct {
		Frameworks []struct {
			Name   string `json:"name"`
			Latest string `json:"latest"`
		} `json:"frameworks"`
	}
	if err := json.Unmarshal(data, &index); err != nil {
		t.Fatal(err)
	}
	for _, row := range index.Frameworks {
		if row.Name != framework {
			continue
		}
		def, ok := stores.Read(stores.Frameworks, framework+"/"+row.Latest+".yaml")
		if !ok {
			t.Fatalf("the index says %s %s exists and the store has no file for it", framework, row.Latest)
		}
		var fw struct {
			PublicDir string `yaml:"public_dir"`
		}
		if err := yaml.Unmarshal(def, &fw); err != nil {
			t.Fatal(err)
		}
		return fw.PublicDir
	}
	t.Fatalf("no framework definition for %q", framework)
	return ""
}
