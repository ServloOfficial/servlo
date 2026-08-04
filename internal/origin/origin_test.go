package origin

import (
	"strings"
	"testing"
)

// The fork boundary, asserted endpoint by endpoint. Servlo's own artefacts
// resolve against Servlo's repository; only the two dependencies PRD §0 retains
// on purpose may still point upstream. Nothing anywhere references geodro, and
// no endpoint returns an empty list that would panic store.NewClient's urls[0].
func TestEndpointsRespectTheForkBoundary(t *testing.T) {
	servlo := map[string][]string{
		"releases":  ReleaseBaseURLs(),
		"downloads": ReleaseDownloadBases(),
		"api":       ReleaseAPIBaseURLs(),
		"changelog": ChangelogURLs(),
		"tools":     ToolsManifestURLs(),
	}
	upstream := map[string][]string{
		"framework-store": StoreBaseURLs(),
		"service-store":   ServiceStoreBaseURLs(),
		"baseimage":       BaseImageRefs("85", "h"),
	}

	for name, got := range servlo {
		if len(got) == 0 {
			t.Fatalf("%s: empty base list", name)
		}
		if !strings.Contains(got[0], "realrashid/servlo") {
			t.Errorf("%s: primary %q must resolve against realrashid/servlo", name, got[0])
		}
		if strings.Contains(got[0], "lerd-env") {
			t.Errorf("%s: primary %q still points at upstream", name, got[0])
		}
	}

	for name, got := range upstream {
		if len(got) == 0 {
			t.Fatalf("%s: empty base list", name)
		}
		if !strings.Contains(got[0], "lerd-env") {
			t.Errorf("%s: primary %q is not the retained upstream location", name, got[0])
		}
	}

	for _, lists := range []map[string][]string{servlo, upstream} {
		for name, got := range lists {
			for _, u := range got {
				if strings.Contains(u, "geodro") {
					t.Errorf("%s: must not reference geodro, got %q", name, u)
				}
			}
		}
	}
}

func TestBaseImageRefFormat(t *testing.T) {
	refs := BaseImageRefs("84", "abc")
	if len(refs) != 1 || refs[0] != "ghcr.io/lerd-env/lerd-php84-fpm-base:abc" {
		t.Errorf("base ref = %v, want [ghcr.io/lerd-env/lerd-php84-fpm-base:abc]", refs)
	}
}

func TestBaseImageRegistryOverride(t *testing.T) {
	t.Setenv("SERVLO_BASE_IMAGE_REGISTRY", "registry.example/mirror")
	refs := BaseImageRefs("85", "h")
	if len(refs) != 1 || refs[0] != "registry.example/mirror/lerd-php85-fpm-base:h" {
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
	if len(got) == 0 || !strings.Contains(got[0], "lerd-env") {
		t.Fatalf("empty override must fall back to the lerd-env default, got %v", got)
	}
}
