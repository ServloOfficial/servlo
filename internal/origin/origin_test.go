package origin

import (
	"strings"
	"testing"
)

// The fork boundary, asserted endpoint by endpoint. Servlo's own artefacts
// resolve against Servlo's repository; only the one dependency PRD §0 retains on
// purpose may still point upstream. Nothing anywhere references geodro, and no
// endpoint returns an empty list that would panic store.NewClient's urls[0].
func TestEndpointsRespectTheForkBoundary(t *testing.T) {
	servlo := map[string][]string{
		"releases":        ReleaseBaseURLs(),
		"downloads":       ReleaseDownloadBases(),
		"api":             ReleaseAPIBaseURLs(),
		"changelog":       ChangelogURLs(),
		"tools":           ToolsManifestURLs(),
		"framework-store": StoreBaseURLs(),
		"service-store":   ServiceStoreBaseURLs(),
		"app-store":       AppStoreBaseURLs(),
	}
	servlo["baseimage"] = BaseImageRefs("85", "h")

	for name, got := range servlo {
		if len(got) == 0 {
			t.Fatalf("%s: empty base list", name)
		}
		if !strings.Contains(strings.ToLower(got[0]), "servloofficial/servlo") {
			t.Errorf("%s: primary %q must resolve against ServloOfficial/servlo", name, got[0])
		}
	}

	for name, got := range servlo {
		for _, u := range got {
			if strings.Contains(u, "geodro") {
				t.Errorf("%s: must not reference geodro, got %q", name, u)
			}
		}
	}
}

func TestBaseImageRefFormat(t *testing.T) {
	refs := BaseImageRefs("84", "abc")
	if len(refs) != 1 || refs[0] != "ghcr.io/servloofficial/servlo-php84-fpm-base:abc" {
		t.Errorf("base ref = %v, want [ghcr.io/servloofficial/servlo-php84-fpm-base:abc]", refs)
	}
}

func TestBaseImageRegistryOverride(t *testing.T) {
	t.Setenv("SERVLO_BASE_IMAGE_REGISTRY", "registry.example/mirror")
	refs := BaseImageRefs("85", "h")
	if len(refs) != 1 || refs[0] != "registry.example/mirror/servlo-php85-fpm-base:h" {
		t.Errorf("override base ref = %v", refs)
	}
}

func TestStoreEnvOverrideReplacesList(t *testing.T) {
	t.Setenv("SERVLO_STORE_BASE_URL", "https://store.example/a, https://store.example/b")
	got := StoreBaseURLs()
	want := []string{"https://store.example/a", "https://store.example/b"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("override list = %v, want %v", got, want)
	}
}

func TestServiceStoreEnvOverride(t *testing.T) {
	t.Setenv("SERVLO_SERVICES_BASE_URL", "https://svc.example/a, https://svc.example/b")
	got := ServiceStoreBaseURLs()
	want := []string{"https://svc.example/a", "https://svc.example/b"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("service override list = %v, want %v", got, want)
	}
}

// A malformed override (only commas/whitespace) must be ignored and fall back to
// the default, never an empty list that would panic store.NewClient's urls[0].
func TestEnvOverrideIgnoredWhenEmpty(t *testing.T) {
	t.Setenv("SERVLO_STORE_BASE_URL", " , , ")
	got := StoreBaseURLs()
	if len(got) == 0 || !strings.Contains(got[0], "ServloOfficial/servlo") {
		t.Fatalf("empty override must fall back to the in-repo store, got %v", got)
	}
}

// GHCR refuses a namespace containing capitals, and an organisation is free to
// have them in its name, so the namespace is lowercased rather than passed
// through from mainRepo. Pinned by a test because the failure surfaces at the
// registry on push, a long way from the build that produced the ref, and only
// on the day the project moves to such an organisation.
func TestGHCRNamespaceIsLowercasedWhateverTheOrgIsCalled(t *testing.T) {
	for _, tc := range []struct{ repo, want string }{
		{"ServloOfficial/servlo", "servloofficial"},
		{"acme/servlo", "acme"},
	} {
		if got := ghcrNamespace(tc.repo); got != tc.want {
			t.Errorf("ghcrNamespace(%q) = %q, want %q", tc.repo, got, tc.want)
		}
	}
}

// The whole ref, not just the namespace, since that is what gets pushed.
func TestBaseImageRefIsAValidGHCRReference(t *testing.T) {
	ref := BaseImageRefs("84", "0db4a0b5cdaa")[0]
	name, _, _ := strings.Cut(ref, ":")
	if name != strings.ToLower(name) {
		t.Errorf("base image ref has capitals GHCR will reject: %q", ref)
	}
}
