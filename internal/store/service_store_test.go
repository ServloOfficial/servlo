package store

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

func serviceTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/index.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"services":[
			{"name":"demo","description":"Demo document store","family":"demo","dashboard":"http://localhost:9","depends_on":["mysql"]},
			{"name":"mariadb","description":"MariaDB","family":"mariadb","env_role":"mysql"},
			{"name":"widget","description":"Widget cache"}
		]}`))
	})
	mux.HandleFunc("/demo.yaml", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("name: demo\nimage: example/demo:1\ndescription: Demo document store\nports:\n  - \"1234:1234\"\n"))
	})
	mux.HandleFunc("/bad.yaml", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("name: bad\n")) // no image and no versions => invalid
	})
	return httptest.NewServer(mux)
}

func serviceTestClient(srv *httptest.Server) *Client {
	return &Client{BaseURL: srv.URL}
}

func TestFetchServiceIndex(t *testing.T) {
	srv := serviceTestServer(t)
	defer srv.Close()
	idx, err := serviceTestClient(srv).FetchServiceIndex()
	if err != nil {
		t.Fatalf("FetchServiceIndex: %v", err)
	}
	if len(idx.Services) != 3 || idx.Services[0].Name != "demo" {
		t.Fatalf("unexpected index: %+v", idx)
	}
	if idx.Services[0].Family != "demo" || idx.Services[0].Dashboard == "" {
		t.Errorf("index entry lost metadata: %+v", idx.Services[0])
	}
	if idx.Services[1].EnvRole != "mysql" {
		t.Errorf("index entry lost env_role: %+v", idx.Services[1])
	}
}

func TestFetchServicePreset_SavesToCacheAndSeamServesIt(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	srv := serviceTestServer(t)
	defer srv.Close()

	data, err := serviceTestClient(srv).FetchServicePreset("demo")
	if err != nil {
		t.Fatalf("FetchServicePreset: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("FetchServicePreset returned no bytes")
	}
	if _, err := os.Stat(filepath.Join(config.StorePresetsDir(), "demo.yaml")); err != nil {
		t.Errorf("preset not written to store cache: %v", err)
	}
	// The seam must now serve the fetched preset by name.
	if !config.PresetExists("demo") {
		t.Error("PresetExists(demo) false after fetch")
	}
	p, err := config.LoadPreset("demo")
	if err != nil || p.Image != "example/demo:1" {
		t.Errorf("LoadPreset(demo) = %+v, %v", p, err)
	}
}

func TestFetchServicePreset_RejectsInvalid(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	srv := serviceTestServer(t)
	defer srv.Close()

	if _, err := serviceTestClient(srv).FetchServicePreset("bad"); err == nil {
		t.Fatal("expected FetchServicePreset to reject an invalid preset")
	}
	if _, err := os.Stat(filepath.Join(config.StorePresetsDir(), "bad.yaml")); err == nil {
		t.Error("invalid preset must not be written to the store cache")
	}
}

// Full production path: the SERVLO_SERVICES_BASE_URL override flows through
// origin into NewServiceClient, which fetches, validates, saves to the cache
// dir, and the config seam then serves the preset by name.
func TestNewServiceClient_HonorsEnvOverrideEndToEnd(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	srv := serviceTestServer(t)
	defer srv.Close()
	t.Setenv("SERVLO_SERVICES_BASE_URL", srv.URL)

	if _, err := NewServiceClient().FetchServicePreset("demo"); err != nil {
		t.Fatalf("FetchServicePreset via env override: %v", err)
	}
	if !config.PresetExists("demo") {
		t.Error("seam does not serve the preset fetched through the real client")
	}
}

func TestSearchServices(t *testing.T) {
	srv := serviceTestServer(t)
	defer srv.Close()
	c := serviceTestClient(srv)

	got, err := c.SearchServices("cache") // matches "Widget cache" description
	if err != nil {
		t.Fatalf("SearchServices: %v", err)
	}
	if len(got) != 1 || got[0].Name != "widget" {
		t.Errorf("SearchServices(cache) = %+v, want [widget]", got)
	}
	if all, _ := c.SearchServices(""); len(all) != 3 {
		t.Errorf("empty query should match all, got %d", len(all))
	}
}

// A service preset names the image servlo runs, what it mounts and what it is
// given on its command line, so a swapped one is a container of somebody else's
// choosing on the droplet. The framework half of the store has been checked
// against the index digest since digests existed; this half had not been.
func TestFetchServicePreset_RefusesABodyThatDoesNotMatchTheIndexDigest(t *testing.T) {
	const served = "name: demo\nimage: example/evil:1\ndescription: Demo document store\nports:\n  - \"1234:1234\"\n"
	mux := http.NewServeMux()
	mux.HandleFunc("/index.json", func(w http.ResponseWriter, _ *http.Request) {
		// The digest of something else entirely, which is what a swapped file
		// looks like from here.
		_, _ = w.Write([]byte(`{"services":[{"name":"demo","description":"Demo","digest":"sha256:` +
			hex.EncodeToString(sha256Of("name: demo\nimage: example/demo:1\n")) + `"}]}`))
	})
	mux.HandleFunc("/demo.yaml", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(served))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	_, err := serviceTestClient(srv).FetchServicePreset("demo")
	if err == nil {
		t.Fatal("a preset whose body does not match the index digest was accepted")
	}
	if !strings.Contains(err.Error(), "does not match the digest") {
		t.Errorf("error = %v, does not say why it was refused", err)
	}
	// And nothing of it reached the cache the seam serves from.
	if _, statErr := os.Stat(filepath.Join(config.StorePresetsDir(), "demo.yaml")); statErr == nil {
		t.Error("the refused preset was saved anyway")
	}
}

// A preset the index vouches for is saved as it always was.
func TestFetchServicePreset_AcceptsABodyThatMatchesTheIndexDigest(t *testing.T) {
	const served = "name: demo\nimage: example/demo:1\ndescription: Demo document store\nports:\n  - \"1234:1234\"\n"
	mux := http.NewServeMux()
	mux.HandleFunc("/index.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"services":[{"name":"demo","description":"Demo","digest":"sha256:` +
			hex.EncodeToString(sha256Of(served)) + `"}]}`))
	})
	mux.HandleFunc("/demo.yaml", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(served))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if _, err := serviceTestClient(srv).FetchServicePreset("demo"); err != nil {
		t.Fatalf("FetchServicePreset: %v", err)
	}
	if _, err := os.Stat(filepath.Join(config.StorePresetsDir(), "demo.yaml")); err != nil {
		t.Errorf("the preset was not saved: %v", err)
	}
}

func sha256Of(s string) []byte {
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}
