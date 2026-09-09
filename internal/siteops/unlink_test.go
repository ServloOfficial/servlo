package siteops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ServloOfficial/servlo/internal/certs"
	"github.com/ServloOfficial/servlo/internal/config"
)

func TestIsParkedSite(t *testing.T) {
	cases := []struct {
		sitePath   string
		parkedDirs []string
		want       bool
	}{
		{"/home/user/Projects/myapp", []string{"/home/user/Projects"}, true},
		{"/home/user/Projects/myapp", []string{"/home/user/Other"}, false},
		{"/home/user/Projects/myapp", []string{"/home/user/Projects", "/home/user/Servlo"}, true},
		{"/home/user/Servlo/myapp", []string{"/home/user/Projects", "/home/user/Servlo"}, true},
		{"/home/user/Projects/myapp", []string{}, false},
		{"/home/user/Projects/sub/deep", []string{"/home/user/Projects"}, false}, // not direct child
	}

	for _, c := range cases {
		got := IsParkedSite(c.sitePath, c.parkedDirs)
		if got != c.want {
			t.Errorf("IsParkedSite(%q, %v) = %v, want %v", c.sitePath, c.parkedDirs, got, c.want)
		}
	}
}

// The certificate and its private key are the one thing a removed site must not
// leave behind, and unlink removed them from a directory they have not lived in
// since the layout gained its sites/ segment: the join here was its own copy
// rather than the one the issuer writes through, so it silently stopped
// matching and every site ever unlinked kept its key.
func TestUnlinkSiteCore_TakesTheCertificateAndKeyWithTheSite(t *testing.T) {
	for _, secured := range []bool{true, false} {
		name := "unsecured"
		if secured {
			name = "secured"
		}
		t.Run(name, func(t *testing.T) {
			domain := "gone.example.com"
			certPath, keyPath := certs.SitePaths(domain)
			if err := os.MkdirAll(filepath.Dir(certPath), 0755); err != nil {
				t.Fatalf("creating the certs directory: %v", err)
			}
			for _, p := range []string{certPath, keyPath} {
				if err := os.WriteFile(p, []byte("material"), 0600); err != nil {
					t.Fatalf("writing %s: %v", p, err)
				}
			}

			site := &config.Site{
				Name:    "gone",
				Path:    t.TempDir(),
				Domains: []string{domain},
				Secured: secured,
			}
			// nginx is not running in a test, so the reload at the end fails.
			// Everything this test is about happens before it.
			_ = UnlinkSiteCore(site, nil)

			for _, p := range []string{certPath, keyPath} {
				if _, err := os.Stat(p); !os.IsNotExist(err) {
					t.Errorf("%s survived the site (stat error %v)", filepath.Base(p), err)
				}
			}
		})
	}
}

// The backup pair is torn down through a hook because internal/backup imports
// this package. A hook nothing calls is the same as no teardown at all, and the
// timer it leaves is named for the site rather than for anything about the
// site, so the next site to take that name runs on it.
func TestUnlinkSiteCore_TearsDownTheBackupSchedule(t *testing.T) {
	var got []string
	prev := RemoveSiteBackupSchedules
	RemoveSiteBackupSchedules = func(name string) { got = append(got, name) }
	t.Cleanup(func() { RemoveSiteBackupSchedules = prev })

	site := &config.Site{Name: "shop", Path: t.TempDir(), Domains: []string{"shop.example.com"}}
	_ = UnlinkSiteCore(site, nil)

	if len(got) != 1 || got[0] != "shop" {
		t.Errorf("the backup schedule was not torn down for the site being removed: %v", got)
	}
}

// Not for a parked one, though. A scheduled backup reads the site's files and
// its database rather than running anything inside it, so a tombstoned site is
// still there to back up, and taking the timer away would leave a relinked site
// silently unbacked until the next servlo start rewrote it.
func TestUnlinkSiteCore_LeavesAParkedSiteItsBackupSchedule(t *testing.T) {
	var got []string
	prev := RemoveSiteBackupSchedules
	RemoveSiteBackupSchedules = func(name string) { got = append(got, name) }
	t.Cleanup(func() { RemoveSiteBackupSchedules = prev })

	parked := t.TempDir()
	site := &config.Site{Name: "parkedshop", Path: filepath.Join(parked, "parkedshop"), Domains: []string{"parkedshop.example.com"}}
	if err := config.AddSite(*site); err != nil {
		t.Fatal(err)
	}
	_ = UnlinkSiteCore(site, []string{parked})

	if len(got) != 0 {
		t.Errorf("a parked site lost its backup schedule: %v", got)
	}
}

// The override is included by name from the site's own vhost, so once the vhost
// goes it is inert and cannot break a reload. What it can do is come back. Both
// it and its backups are keyed on the primary domain, and a later site on that
// domain generates a vhost that includes the file again: somebody else's
// hand-written nginx directives, applied to a site whose operator never wrote
// them and cannot see where they came from.
func TestUnlinkSiteCore_TakesTheHandWrittenNginxOverrideWithIt(t *testing.T) {
	domain := "gone.example.com"
	if err := os.MkdirAll(config.NginxCustomD(), 0755); err != nil {
		t.Fatalf("creating custom.d: %v", err)
	}
	if err := os.MkdirAll(config.NginxCustomDBkp(), 0755); err != nil {
		t.Fatalf("creating custom.d.bkp: %v", err)
	}
	override := CustomNginxPath(domain)
	if err := os.WriteFile(override, []byte("auth_basic \"staging\";\n"), 0644); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(config.NginxCustomDBkp(), domain+".conf.bkp.20260101-000000")
	if err := os.WriteFile(backup, []byte("older\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// A different site's override, to prove the removal is keyed and not a sweep.
	other := CustomNginxPath("kept.example.com")
	if err := os.WriteFile(other, []byte("keep me\n"), 0644); err != nil {
		t.Fatal(err)
	}

	site := &config.Site{Name: "gone", Path: t.TempDir(), Domains: []string{domain}}
	_ = UnlinkSiteCore(site, nil)

	for _, p := range []string{override, backup} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s survived the site (stat error %v)", filepath.Base(p), err)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("another site's override went with it: %v", err)
	}
}

// A parked site is unlinked by being marked ignored, and linking the directory
// again brings it back. The two things relinking cannot recreate for free have
// to still be there when it does: an override is something a person typed, and
// a certificate costs a rate-limited exchange with an authority that can refuse.
func TestUnlinkSiteCore_KeepsWhatAParkedSiteCannotCheaplyGetBack(t *testing.T) {
	domain := "parked.example.com"
	parked := t.TempDir()
	if err := os.MkdirAll(config.NginxCustomD(), 0755); err != nil {
		t.Fatalf("creating custom.d: %v", err)
	}
	override := CustomNginxPath(domain)
	if err := os.WriteFile(override, []byte("client_max_body_size 200m;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := certs.SitePaths(domain)
	if err := os.MkdirAll(filepath.Dir(certPath), 0755); err != nil {
		t.Fatalf("creating the certs directory: %v", err)
	}
	for _, p := range []string{certPath, keyPath} {
		if err := os.WriteFile(p, []byte("material"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	site := &config.Site{Name: "parked", Path: filepath.Join(parked, "parked"), Domains: []string{domain}, Secured: true}
	if err := config.AddSite(*site); err != nil {
		t.Fatal(err)
	}
	_ = UnlinkSiteCore(site, []string{parked})

	if _, err := os.Stat(override); err != nil {
		t.Errorf("a parked site's override was deleted, so relinking it would not bring the config back: %v", err)
	}
	for _, p := range []string{certPath, keyPath} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("a parked site's %s was deleted, so relinking it would spend an issuance: %v", filepath.Base(p), err)
		}
	}
}
