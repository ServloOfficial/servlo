package tui

import (
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/sitedoctor"
	"github.com/geodro/lerd/internal/siteinfo"
)

func TestSiteDoctorContent_PromptsToRunWhenNoResult(t *testing.T) {
	m := NewModel("test")
	site := &siteinfo.EnrichedSite{Name: "acme", Domains: []string{"acme.test"}, FrameworkName: "laravel"}
	joined := stripANSI(strings.Join(siteDoctorContentLines(m, site, 120), "\n"))
	if !strings.Contains(joined, "press 5 to run") {
		t.Errorf("expected a run prompt before any result:\n%s", joined)
	}
}

func TestSiteDoctorContent_ShowsLoading(t *testing.T) {
	m := NewModel("test")
	m.doctorSite = "acme"
	m.doctorLoading = true
	site := &siteinfo.EnrichedSite{Name: "acme", FrameworkName: "laravel"}
	joined := stripANSI(strings.Join(siteDoctorContentLines(m, site, 120), "\n"))
	if !strings.Contains(joined, "running checks") {
		t.Errorf("expected loading placeholder:\n%s", joined)
	}
}

func TestSiteDoctorContent_RendersChecks(t *testing.T) {
	m := NewModel("test")
	m.doctorSite = "acme"
	m.doctorChecks = []sitedoctor.Check{
		{Name: "app_key", Status: sitedoctor.StatusFail, Detail: "APP_KEY is empty", Fix: "key:generate"},
		{Name: "app_debug", Status: sitedoctor.StatusOK},
	}
	site := &siteinfo.EnrichedSite{Name: "acme", FrameworkName: "laravel"}
	joined := stripANSI(strings.Join(siteDoctorContentLines(m, site, 120), "\n"))
	for _, want := range []string{"app key", "fail", "APP_KEY is empty", "key:generate", "1 failing"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in doctor panel:\n%s", want, joined)
		}
	}
}

func TestOpenDoctorTab_NoOpForNonLaravel(t *testing.T) {
	m := NewModel("test")
	m.snap.Sites = []siteinfo.EnrichedSite{{Name: "static", Domains: []string{"static.test"}}}
	m.focus = paneSites
	m.siteCursor = 0
	if cmd := m.openDoctorTab(); cmd != nil {
		t.Error("openDoctorTab should be a no-op for a non-Laravel site")
	}
	if m.siteTab == tabSiteDoctor {
		t.Error("siteTab should not switch to Doctor for a non-Laravel site")
	}
}
