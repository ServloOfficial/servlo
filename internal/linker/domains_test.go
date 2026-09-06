package linker

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

// setupSitesYAML writes a sites.yaml into a temp XDG_DATA_HOME so the registry
// lookups read from it instead of the real user config.
func setupSitesYAML(t *testing.T, yaml string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	dir := filepath.Join(tmp, "servlo")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sites.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
}

// sliceEq compares two string slices treating nil and empty as equal.
func sliceEq(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

func TestFilterConflictingDomains(t *testing.T) {
	allSites := []config.Site{
		{Name: "blog", Path: "/home/me/blog", Domains: []string{"blog.test"}},
		{Name: "shop", Path: "/home/me/shop", Domains: []string{"shop.test", "store.test"}},
	}

	cases := []struct {
		name        string
		desired     []string
		ownPath     string
		wantKept    []string
		wantRemoved []string
	}{
		{
			name:     "no conflicts — everything kept in order",
			desired:  []string{"newsite.test", "alias.test"},
			ownPath:  "/home/me/newsite",
			wantKept: []string{"newsite.test", "alias.test"},
		},
		{
			name:        "primary conflicts — alias becomes primary",
			desired:     []string{"shop.test", "alias.test"},
			ownPath:     "/home/me/newshop",
			wantKept:    []string{"alias.test"},
			wantRemoved: []string{"shop.test"},
		},
		{
			name:        "all desired conflict — empty kept list",
			desired:     []string{"shop.test", "store.test"},
			ownPath:     "/home/me/newshop",
			wantRemoved: []string{"shop.test", "store.test"},
		},
		{
			name:     "re-link of same path is not a conflict",
			desired:  []string{"shop.test", "store.test"},
			ownPath:  "/home/me/shop",
			wantKept: []string{"shop.test", "store.test"},
		},
		{
			name:     "mix of own + new",
			desired:  []string{"shop.test", "newdomain.test"},
			ownPath:  "/home/me/shop",
			wantKept: []string{"shop.test", "newdomain.test"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kept, removed := FilterConflictingDomains(c.desired, c.ownPath, allSites)
			if !sliceEq(kept, c.wantKept) {
				t.Errorf("kept = %v, want %v", kept, c.wantKept)
			}
			if !sliceEq(removed, c.wantRemoved) {
				t.Errorf("removed = %v, want %v", removed, c.wantRemoved)
			}
		})
	}
}

// A conflicted domain used to be replaced with a generated one. It cannot be:
// the operator's domain is the only name that resolves to this server, so a
// clash is reported rather than worked around.
func TestResolveDomains_allConflictedKeepsNothing(t *testing.T) {
	setupSitesYAML(t, `sites:
  - name: shop
    domains:
      - shop.example.com
    path: /projects/shop
`)
	kept, removed := ResolveDomains([]string{"shop.example.com"}, "/projects/newshop")
	if len(kept) != 0 {
		t.Errorf("kept = %v, want nothing", kept)
	}
	if !sliceEq(removed, []string{"shop.example.com"}) {
		t.Errorf("removed = %v, want [shop.example.com]", removed)
	}
}

// The same path re-linking its own domain is not a conflict.
func TestResolveDomains_ownPathKeepsItsDomain(t *testing.T) {
	setupSitesYAML(t, `sites:
  - name: shop
    domains:
      - shop.example.com
    path: /projects/shop
`)
	kept, removed := ResolveDomains([]string{"shop.example.com"}, "/projects/shop")
	if !sliceEq(kept, []string{"shop.example.com"}) {
		t.Errorf("kept = %v, want [shop.example.com]", kept)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want nothing", removed)
	}
}

func TestFreeSiteName_unused(t *testing.T) {
	setupSitesYAML(t, "sites: []\n")
	if got := FreeSiteName("myapp", "/projects/myapp"); got != "myapp" {
		t.Errorf("got %q, want %q", got, "myapp")
	}
}

func TestFreeSiteName_samePath_relink(t *testing.T) {
	setupSitesYAML(t, `sites:
  - name: myapp
    domains:
      - myapp.test
    path: /projects/myapp
    php_version: "8.3"
    node_version: "22"
`)
	if got := FreeSiteName("myapp", "/projects/myapp"); got != "myapp" {
		t.Errorf("got %q, want %q", got, "myapp")
	}
}

func TestFreeSiteName_collision_differentPath(t *testing.T) {
	setupSitesYAML(t, `sites:
  - name: myapp
    domains:
      - myapp.test
    path: /projects/other-myapp
    php_version: "8.3"
    node_version: "22"
`)
	if got := FreeSiteName("myapp", "/projects/myapp"); got != "myapp-2" {
		t.Errorf("got %q, want %q", got, "myapp-2")
	}
}

func TestFreeSiteName_multipleCollisions(t *testing.T) {
	setupSitesYAML(t, `sites:
  - name: myapp
    domains:
      - myapp.test
    path: /projects/one
  - name: myapp-2
    domains:
      - myapp-2.test
    path: /projects/two
`)
	if got := FreeSiteName("myapp", "/projects/three"); got != "myapp-3" {
		t.Errorf("got %q, want %q", got, "myapp-3")
	}
}

func TestFreeSiteName_legacyDomainField(t *testing.T) {
	setupSitesYAML(t, `sites:
  - name: myapp
    domain: myapp.test
    path: /projects/other
`)
	if got := FreeSiteName("myapp", "/projects/new"); got != "myapp-2" {
		t.Errorf("got %q, want %q", got, "myapp-2")
	}
}

func TestDesiredDomains(t *testing.T) {
	cases := []struct {
		name      string
		proj      *config.ProjectConfig
		requested string
		dir       string
		want      []string
		wantErr   bool
	}{
		{
			name: "a directory named for its domain is the default",
			dir:  "/projects/example.com",
			want: []string{"example.com"},
		},
		{
			name:    "a directory that is not a domain has no default",
			dir:     "/projects/myapp",
			wantErr: true,
		},
		{
			name: "project domains are used verbatim, with no suffix appended",
			proj: &config.ProjectConfig{Domains: []string{"Shop.example.com", "admin.example.com"}},
			dir:  "/projects/myapp",
			want: []string{"shop.example.com", "admin.example.com"},
		},
		{
			name:      "the requested domain leads",
			proj:      &config.ProjectConfig{Domains: []string{"shop.example.com", "admin.example.com"}},
			requested: "admin.example.com",
			dir:       "/projects/myapp",
			want:      []string{"admin.example.com", "shop.example.com"},
		},
		{
			name:      "the requested domain is not duplicated",
			proj:      &config.ProjectConfig{Domains: []string{"shop.example.com"}},
			requested: "shop.example.com",
			dir:       "/projects/myapp",
			want:      []string{"shop.example.com"},
		},
		{
			name:      "a bare label is refused rather than completed",
			requested: "myapp",
			dir:       "/projects/myapp",
			wantErr:   true,
		},
		{
			name:    "a project declaring a bare label is refused too",
			proj:    &config.ProjectConfig{Domains: []string{"shop"}},
			dir:     "/projects/myapp",
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := desiredDomains(c.proj, c.requested, c.dir)
			if c.wantErr {
				if err == nil {
					t.Fatalf("got %v, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("desiredDomains: %v", err)
			}
			if !sliceEq(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}
