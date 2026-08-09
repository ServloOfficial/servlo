package siteops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/services"
	"github.com/realrashid/servlo/internal/sitecron"
)

// cronMgr is systemd for the duration of a test: unit bodies in a map, so an
// assertion can ask whether the timer for a deleted entry is really gone.
type cronMgr struct {
	units  map[string]string
	timers map[string]string
}

func newCronMgr() *cronMgr {
	return &cronMgr{units: map[string]string{}, timers: map[string]string{}}
}

func (m *cronMgr) WriteServiceUnit(name, body string) error { m.units[name] = body; return nil }
func (m *cronMgr) WriteServiceUnitIfChanged(name, body string) (bool, error) {
	m.units[name] = body
	return true, nil
}
func (m *cronMgr) RemoveServiceUnit(name string) error { delete(m.units, name); return nil }
func (m *cronMgr) WriteTimerUnitIfChanged(name, body string) (bool, error) {
	m.timers[name] = body
	return true, nil
}
func (m *cronMgr) RemoveTimerUnit(name string) error { delete(m.timers, name); return nil }
func (m *cronMgr) ListTimerUnits(string) []string    { return nil }
func (m *cronMgr) ListServiceUnits(glob string) []string {
	var out []string
	prefix := strings.TrimSuffix(glob, "*")
	for name := range m.units {
		if strings.HasPrefix(name, prefix) {
			out = append(out, name)
		}
	}
	return out
}
func (m *cronMgr) WriteContainerUnit(string, string) error { return nil }
func (m *cronMgr) ContainerUnitInstalled(string) bool      { return false }
func (m *cronMgr) RemoveContainerUnit(string) error        { return nil }
func (m *cronMgr) ListContainerUnits(string) []string      { return nil }
func (m *cronMgr) DaemonReload() error                     { return nil }
func (m *cronMgr) Start(string) error                      { return nil }
func (m *cronMgr) Stop(string) error                       { return nil }
func (m *cronMgr) Restart(string) error                    { return nil }
func (m *cronMgr) Enable(string) error                     { return nil }
func (m *cronMgr) Disable(string) error                    { return nil }
func (m *cronMgr) IsActive(string) bool                    { return false }
func (m *cronMgr) IsEnabled(string) bool                   { return false }
func (m *cronMgr) UnitStatus(string) (string, error)       { return "", nil }

func useCronMgr(t *testing.T) *cronMgr {
	t.Helper()
	mgr := newCronMgr()
	prev := services.Mgr
	services.Mgr = mgr
	t.Cleanup(func() { services.Mgr = prev })
	return mgr
}

// cronSite is a registered WordPress site with its store definition in place,
// which is where the pseudo-cron declaration has to come from.
func cronSite(t *testing.T) *config.Site {
	t.Helper()
	scriptHome(t)
	seedStoreFramework(t, "wordpress", "6")

	path := filepath.Join(t.TempDir(), "shop")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"wp-login.php"} {
		if err := os.WriteFile(filepath.Join(path, f), []byte("<?php\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wpConfig := "<?php\ndefine( 'DB_NAME', 'shop' );\n\n/* That's all, stop editing! */\nrequire_once ABSPATH . 'wp-settings.php';\n"
	if err := os.WriteFile(filepath.Join(path, "wp-config.php"), []byte(wpConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	site := config.Site{
		Name: "shop", Domains: []string{"shop.example"},
		Path: path, Framework: "wordpress", PHPVersion: "8.3",
	}
	if err := config.AddSite(site); err != nil {
		t.Fatal(err)
	}
	return &site
}

func TestSaveCron_DerivesAnIdentifierAndTranslatesTheSchedule(t *testing.T) {
	mgr := useCronMgr(t)
	site := cronSite(t)

	entry, err := SaveCron(site, config.CronEntry{Name: "Prune batches", Command: "php artisan queue:prune-batches", Schedule: "0 3 * * *"})
	if err != nil {
		t.Fatal(err)
	}
	if entry.ID != "prune-batches" {
		t.Errorf("ID = %q, want one derived from the name", entry.ID)
	}
	if entry.Calendar != "*-*-* 03:00:00" {
		t.Errorf("Calendar = %q, want the translated cron line", entry.Calendar)
	}
	if len(site.Cron) != 1 {
		t.Fatalf("the site holds %d entries, want 1", len(site.Cron))
	}
	if _, ok := mgr.timers[sitecron.UnitName("shop", "prune-batches")]; !ok {
		t.Error("no timer was written for the saved entry")
	}

	// Reloaded from the registry rather than the in-memory site, since surviving
	// a restart is the whole point of storing it.
	reloaded, err := config.FindSiteByDomain("shop.example")
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Cron) != 1 || reloaded.Cron[0].Command != "php artisan queue:prune-batches" {
		t.Errorf("the entry did not survive the registry write: %+v", reloaded.Cron)
	}
}

// Two entries with the same name are two entries, not one overwriting the other.
func TestSaveCron_SecondEntryWithTheSameNameGetsItsOwnIdentifier(t *testing.T) {
	useCronMgr(t)
	site := cronSite(t)

	first, err := SaveCron(site, config.CronEntry{Name: "Report", Command: "php a", Schedule: "daily"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := SaveCron(site, config.CronEntry{Name: "Report", Command: "php b", Schedule: "daily"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatalf("both entries got the identifier %q, so one replaced the other", first.ID)
	}
	if len(site.Cron) != 2 {
		t.Errorf("the site holds %d entries, want 2", len(site.Cron))
	}
}

// Editing keeps the row where it was: a list that reorders itself every time
// somebody fixes a typo is a list nobody can scan.
func TestSaveCron_EditingKeepsThePosition(t *testing.T) {
	useCronMgr(t)
	site := cronSite(t)

	first, _ := SaveCron(site, config.CronEntry{Name: "First", Command: "php a", Schedule: "daily"})
	if _, err := SaveCron(site, config.CronEntry{Name: "Second", Command: "php b", Schedule: "daily"}); err != nil {
		t.Fatal(err)
	}
	first.Command = "php a --verbose"
	if _, err := SaveCron(site, first); err != nil {
		t.Fatal(err)
	}
	if site.Cron[0].ID != "first" || len(site.Cron) != 2 {
		t.Errorf("editing moved the row: %+v", site.Cron)
	}
	if site.Cron[0].Command != "php a --verbose" {
		t.Errorf("the edit did not land: %+v", site.Cron[0])
	}
}

func TestSaveCron_RefusesAScheduleItCannotRead(t *testing.T) {
	useCronMgr(t)
	site := cronSite(t)

	if _, err := SaveCron(site, config.CronEntry{Name: "Bad", Command: "php a", Schedule: "every so often"}); err == nil {
		t.Fatal("an unreadable schedule was saved")
	}
	if len(site.Cron) != 0 {
		t.Errorf("a refused entry was stored anyway: %+v", site.Cron)
	}
}

// The one that matters for a delete: nothing left on the machine.
func TestDeleteCron_TakesTheUnitPairWithIt(t *testing.T) {
	mgr := useCronMgr(t)
	site := cronSite(t)

	entry, err := SaveCron(site, config.CronEntry{Name: "Prune", Command: "php a", Schedule: "daily"})
	if err != nil {
		t.Fatal(err)
	}
	if err := DeleteCron(site, entry.ID); err != nil {
		t.Fatal(err)
	}

	unit := sitecron.UnitName("shop", entry.ID)
	if _, ok := mgr.units[unit]; ok {
		t.Error("the service unit survived the delete")
	}
	if _, ok := mgr.timers[unit]; ok {
		t.Error("the timer survived the delete")
	}
	if len(site.Cron) != 0 {
		t.Errorf("the entry survived the delete: %+v", site.Cron)
	}
	reloaded, _ := config.FindSiteByDomain("shop.example")
	if len(reloaded.Cron) != 0 {
		t.Errorf("the registry still holds the entry: %+v", reloaded.Cron)
	}
}

func TestDeleteCron_UnknownEntry(t *testing.T) {
	useCronMgr(t)
	site := cronSite(t)
	if err := DeleteCron(site, "nothing"); err == nil {
		t.Fatal("deleting an entry that is not there reported success")
	}
}

// The framework's declaration is what makes the switch appear. A framework with
// no pseudo-cron offers nothing, and no Go code decides which is which.
func TestPseudoCronFor_ComesFromTheFrameworkDefinition(t *testing.T) {
	useCronMgr(t)
	site := cronSite(t)

	got := PseudoCronFor(site)
	if !got.Available {
		t.Fatal("WordPress's declaration did not reach the panel")
	}
	if got.Replaced {
		t.Error("a site nobody switched reports the replacement in place")
	}
	if got.Constant != "DISABLE_WP_CRON" {
		t.Errorf("Constant = %q", got.Constant)
	}
	if !strings.Contains(got.Command, "wp-cron.php") {
		t.Errorf("Command = %q, which does not run the framework's cron", got.Command)
	}

	site.Framework = "laravel"
	if PseudoCronFor(site).Available {
		t.Error("a framework with no declaration was offered the switch")
	}
}

func TestSetPseudoCron_ReplacesAndRestores(t *testing.T) {
	mgr := useCronMgr(t)
	site := cronSite(t)
	configPath := filepath.Join(site.Path, "wp-config.php")

	if err := SetPseudoCron(site, true); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(configPath)
	if !strings.Contains(string(body), "define( 'DISABLE_WP_CRON', true );") {
		t.Errorf("the page-load scheduler was not switched off:\n%s", body)
	}
	entry, ok := site.FindCron("wp-cron")
	if !ok {
		t.Fatal("no replacement schedule was installed")
	}
	if !entry.Managed {
		t.Error("the installed entry is not marked as one servlo manages")
	}
	if entry.Calendar != "*-*-* *:*:00" {
		t.Errorf("the replacement runs on %q, not every minute", entry.Calendar)
	}
	if _, ok := mgr.timers[sitecron.UnitName("shop", "wp-cron")]; !ok {
		t.Error("no timer was written for the replacement")
	}

	// Turning it off has to give the framework its own scheduler back. A site
	// with the constant still set and the timer gone has no cron at all.
	if err := SetPseudoCron(site, false); err != nil {
		t.Fatal(err)
	}
	body, _ = os.ReadFile(configPath)
	if !strings.Contains(string(body), "define( 'DISABLE_WP_CRON', false );") {
		t.Errorf("the page-load scheduler was not restored:\n%s", body)
	}
	if strings.Contains(string(body), "'false'") {
		t.Errorf("the constant was written as a string, which PHP reads as true:\n%s", body)
	}
	if _, ok := site.FindCron("wp-cron"); ok {
		t.Error("the replacement entry survived being switched off")
	}
	if _, ok := mgr.timers[sitecron.UnitName("shop", "wp-cron")]; ok {
		t.Error("the replacement timer survived being switched off")
	}
}

// An entry servlo installed on the framework's behalf is not one to hand-edit:
// it has its own switch, and a hand-edit would be undone by it.
func TestSaveCron_RefusesToEditAManagedEntry(t *testing.T) {
	useCronMgr(t)
	site := cronSite(t)
	if err := SetPseudoCron(site, true); err != nil {
		t.Fatal(err)
	}

	if _, err := SaveCron(site, config.CronEntry{ID: "wp-cron", Name: "Mine", Command: "php x", Schedule: "daily"}); err == nil {
		t.Fatal("a framework-managed entry was overwritten by hand")
	}
}

// A site that goes away takes its schedule with it. Left behind, the timer
// keeps firing at a directory nothing serves any more, and the page that would
// have shown it is gone too.
func TestRemoveSiteCron_UnlinkingTakesTheTimersWithIt(t *testing.T) {
	mgr := useCronMgr(t)
	site := cronSite(t)
	if _, err := SaveCron(site, config.CronEntry{Name: "Prune", Command: "php a", Schedule: "daily"}); err != nil {
		t.Fatal(err)
	}

	RemoveSiteCron(site)

	if len(mgr.timers) != 0 || len(mgr.units) != 0 {
		t.Errorf("units survived the site: %v %v", mgr.units, mgr.timers)
	}
}
