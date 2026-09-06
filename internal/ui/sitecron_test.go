package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/services"
	"github.com/ServloOfficial/servlo/internal/sitecron"
	"gopkg.in/yaml.v3"
)

// cronFakeMgr stands in for systemd so the handler can be exercised without a
// bus: unit bodies in maps, everything else a no-op that succeeds.
type cronFakeMgr struct {
	units  map[string]string
	timers map[string]string
}

func (m *cronFakeMgr) WriteServiceUnit(name, body string) error { m.units[name] = body; return nil }
func (m *cronFakeMgr) WriteServiceUnitIfChanged(name, body string) (bool, error) {
	m.units[name] = body
	return true, nil
}
func (m *cronFakeMgr) RemoveServiceUnit(name string) error { delete(m.units, name); return nil }
func (m *cronFakeMgr) WriteTimerUnitIfChanged(name, body string) (bool, error) {
	m.timers[name] = body
	return true, nil
}
func (m *cronFakeMgr) RemoveTimerUnit(name string) error { delete(m.timers, name); return nil }
func (m *cronFakeMgr) ListTimerUnits(string) []string    { return nil }
func (m *cronFakeMgr) ListServiceUnits(glob string) []string {
	var out []string
	prefix := strings.TrimSuffix(glob, "*")
	for name := range m.units {
		if strings.HasPrefix(name, prefix) {
			out = append(out, name)
		}
	}
	return out
}
func (m *cronFakeMgr) WriteContainerUnit(string, string) error { return nil }
func (m *cronFakeMgr) ContainerUnitInstalled(string) bool      { return false }
func (m *cronFakeMgr) RemoveContainerUnit(string) error        { return nil }
func (m *cronFakeMgr) ListContainerUnits(string) []string      { return nil }
func (m *cronFakeMgr) DaemonReload() error                     { return nil }
func (m *cronFakeMgr) Start(string) error                      { return nil }
func (m *cronFakeMgr) Stop(string) error                       { return nil }
func (m *cronFakeMgr) Restart(string) error                    { return nil }
func (m *cronFakeMgr) Enable(string) error                     { return nil }
func (m *cronFakeMgr) Disable(string) error                    { return nil }
func (m *cronFakeMgr) IsActive(string) bool                    { return false }
func (m *cronFakeMgr) IsEnabled(string) bool                   { return false }
func (m *cronFakeMgr) UnitStatus(string) (string, error)       { return "", nil }

// cronTestSite registers a WordPress site with its store definition, which is
// where the pseudo-cron declaration comes from.
func cronTestSite(t *testing.T) (*cronFakeMgr, *config.Site) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")

	body, err := os.ReadFile(filepath.Join("..", "..", "stores", "frameworks", "wordpress", "6.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var fw config.Framework
	if err := yaml.Unmarshal(body, &fw); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveStoreFramework(&fw); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "shop")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"wp-login.php":  "<?php\n",
		"wp-config.php": "<?php\ndefine( 'DB_NAME', 'shop' );\n\n/* That's all, stop editing! */\n",
	} {
		if err := os.WriteFile(filepath.Join(path, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	site := config.Site{
		Name: "shop", Domains: []string{"shop.example"},
		Path: path, Framework: "wordpress", PHPVersion: "8.3",
	}
	if err := config.AddSite(site); err != nil {
		t.Fatal(err)
	}

	mgr := &cronFakeMgr{units: map[string]string{}, timers: map[string]string{}}
	prev := services.Mgr
	services.Mgr = mgr
	t.Cleanup(func() { services.Mgr = prev })
	return mgr, &site
}

func cronRequest(t *testing.T, method, path string, body string, rest []string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, reader)
	req.RemoteAddr = "127.0.0.1:5000"
	if !cronRoute(rec, req, "shop.example", rest) {
		t.Fatalf("the cron route did not claim %s %s", method, path)
	}
	return rec
}

func decodeCron(t *testing.T, rec *httptest.ResponseRecorder) SiteCronResponse {
	t.Helper()
	var out SiteCronResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding: %v (%s)", err, rec.Body.String())
	}
	return out
}

// The round trip a panel actually makes: save an entry, then read the list back
// and find it there with its translated schedule.
func TestCronRoute_SavesThenLists(t *testing.T) {
	mgr, _ := cronTestSite(t)

	rec := cronRequest(t, http.MethodPost, "/api/sites/shop.example/cron",
		`{"name":"Prune batches","command":"php artisan queue:prune-batches","schedule":"0 3 * * *","capture_output":true}`,
		[]string{"cron"})
	var saved SiteCronSaveResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if !saved.OK {
		t.Fatalf("the save failed: %s", saved.Error)
	}
	if saved.Entry == nil || saved.Entry.Calendar != "*-*-* 03:00:00" {
		t.Fatalf("the saved entry did not come back translated: %+v", saved.Entry)
	}
	if _, ok := mgr.timers[sitecron.UnitName("shop", "prune-batches")]; !ok {
		t.Error("no timer reached systemd")
	}

	list := decodeCron(t, cronRequest(t, http.MethodGet, "/api/sites/shop.example/cron", "", []string{"cron"}))
	if len(list.Entries) != 1 || list.Entries[0].Name != "Prune batches" {
		t.Fatalf("the entry did not come back in the listing: %+v", list.Entries)
	}
	if list.Entries[0].LastRun != nil {
		t.Errorf("a brand new entry reported a last run: %+v", list.Entries[0].LastRun)
	}
	if !list.Supported {
		t.Error("a container-backed site reported that it cannot run a schedule")
	}
}

// A schedule the parser cannot read comes back as an answer the form can show,
// naming a form that would work.
func TestCronRoute_RefusesAnUnreadableSchedule(t *testing.T) {
	cronTestSite(t)

	rec := cronRequest(t, http.MethodPost, "/api/sites/shop.example/cron",
		`{"name":"Bad","command":"php x","schedule":"every so often"}`, []string{"cron"})
	var saved SiteCronSaveResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.OK {
		t.Fatal("an unreadable schedule was accepted")
	}
	if !strings.Contains(saved.Error, "*/5 * * * *") {
		t.Errorf("the refusal %q shows nothing to copy", saved.Error)
	}
}

func TestCronRoute_DeleteRemovesTheEntryAndItsUnits(t *testing.T) {
	mgr, _ := cronTestSite(t)
	cronRequest(t, http.MethodPost, "/api/sites/shop.example/cron",
		`{"name":"Prune","command":"php x","schedule":"daily"}`, []string{"cron"})

	rec := cronRequest(t, http.MethodDelete, "/api/sites/shop.example/cron/prune", "", []string{"cron", "prune"})
	var out SiteActionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK {
		t.Fatalf("the delete failed: %s", out.Error)
	}
	if _, ok := mgr.timers[sitecron.UnitName("shop", "prune")]; ok {
		t.Error("the timer survived the delete")
	}

	list := decodeCron(t, cronRequest(t, http.MethodGet, "/api/sites/shop.example/cron", "", []string{"cron"}))
	if len(list.Entries) != 0 {
		t.Errorf("the entry survived the delete: %+v", list.Entries)
	}
}

// The framework's declaration is what puts the switch on the page. No Go here
// knows which framework has one.
func TestCronRoute_ReportsTheFrameworksPseudoCron(t *testing.T) {
	cronTestSite(t)

	list := decodeCron(t, cronRequest(t, http.MethodGet, "/api/sites/shop.example/cron", "", []string{"cron"}))
	if !list.PseudoCron.Available {
		t.Fatal("the framework's declaration did not reach the panel")
	}
	if list.PseudoCron.Replaced {
		t.Error("a site nobody switched reports the replacement in place")
	}
	if list.PseudoCron.Label == "" || list.PseudoCron.Constant == "" {
		t.Errorf("the switch has nothing to label itself with: %+v", list.PseudoCron)
	}
}

func TestPseudoCronRoute_ReplacesAndRestores(t *testing.T) {
	mgr, site := cronTestSite(t)
	wpConfig := filepath.Join(site.Path, "wp-config.php")

	post := func(body string) SiteActionResponse {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/sites/shop.example/pseudo-cron", strings.NewReader(body))
		fresh, err := config.FindSiteByDomain("shop.example")
		if err != nil {
			t.Fatal(err)
		}
		handleSitePseudoCron(rec, req, fresh)
		var out SiteActionResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decoding: %v (%s)", err, rec.Body.String())
		}
		return out
	}

	if out := post(`{"replace":true}`); !out.OK {
		t.Fatalf("switching to a real cron failed: %s", out.Error)
	}
	body, _ := os.ReadFile(wpConfig)
	if !strings.Contains(string(body), "define( 'DISABLE_WP_CRON', true );") {
		t.Errorf("the page-load scheduler was not switched off:\n%s", body)
	}
	if _, ok := mgr.timers[sitecron.UnitName("shop", "wp-cron")]; !ok {
		t.Error("no replacement timer was installed")
	}

	list := decodeCron(t, cronRequest(t, http.MethodGet, "/api/sites/shop.example/cron", "", []string{"cron"}))
	if !list.PseudoCron.Replaced {
		t.Error("the panel does not show the replacement as in place")
	}
	if len(list.Entries) != 1 || !list.Entries[0].Managed {
		t.Errorf("the installed entry is not listed as one servlo manages: %+v", list.Entries)
	}

	if out := post(`{"replace":false}`); !out.OK {
		t.Fatalf("switching back failed: %s", out.Error)
	}
	body, _ = os.ReadFile(wpConfig)
	if !strings.Contains(string(body), "define( 'DISABLE_WP_CRON', false );") {
		t.Errorf("the framework's own scheduler was not restored, leaving the site with no cron:\n%s", body)
	}
	if _, ok := mgr.timers[sitecron.UnitName("shop", "wp-cron")]; ok {
		t.Error("the replacement timer survived being switched off")
	}
}

// A site with nowhere to run a command says so, rather than offering a form
// whose every entry would fail once a minute.
func TestCronRoute_AHostProxySiteIsNotOfferedASchedule(t *testing.T) {
	_, site := cronTestSite(t)
	site.HostPort = 3000
	if err := config.AddSite(*site); err != nil {
		t.Fatal(err)
	}

	list := decodeCron(t, cronRequest(t, http.MethodGet, "/api/sites/shop.example/cron", "", []string{"cron"}))
	if list.Supported {
		t.Fatal("a site with no container was offered a schedule")
	}
	if list.Unsupported == "" {
		t.Error("the panel is told it cannot, but not why")
	}

	rec := cronRequest(t, http.MethodPost, "/api/sites/shop.example/cron",
		`{"name":"Prune","command":"php x","schedule":"daily"}`, []string{"cron"})
	var saved SiteCronSaveResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.OK {
		t.Error("a site with no container saved a scheduled command")
	}
}

func TestCronRoute_IgnoresPathsThatAreNotItsOwn(t *testing.T) {
	cronTestSite(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sites/shop.example/deploy", nil)
	if cronRoute(rec, req, "shop.example", []string{"deploy"}) {
		t.Fatal("the cron route claimed a path that is not its own")
	}
}
