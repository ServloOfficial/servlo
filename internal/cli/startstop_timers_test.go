package cli

import (
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/services"
)

type timerRecorder struct {
	services.ServiceManager
	written []string
}

func (m *timerRecorder) WriteTimerUnitIfChanged(name, body string) (bool, error) {
	m.written = append(m.written, name)
	return true, nil
}

func (m *timerRecorder) WriteServiceUnitIfChanged(name, body string) (bool, error) {
	return true, nil
}

func (m *timerRecorder) DaemonReload() error { return nil }

// A rebuild is the case that stays quiet when it goes wrong. `servlo start` put
// back everything that makes a restored site answer requests and nothing that
// makes it do anything on a schedule, so the server came up serving every site
// correctly and never ran a backup, a test restore, or a single cron command
// again. Nothing failed, because a timer that was never written cannot.
func TestRestoreSiteInfrastructure_PutsTheSitesTimersBack(t *testing.T) {
	isolate(t)

	site := config.Site{
		Name: "acme", Domains: []string{"acme-supply.com"}, Path: t.TempDir(), PHPVersion: "8.5",
		Cron:   []config.CronEntry{{ID: "prune", Name: "Prune", Command: "php artisan prune", Calendar: "daily"}},
		Backup: &config.SiteBackup{Schedule: "daily"},
	}
	if err := config.AddSite(site); err != nil {
		t.Fatal(err)
	}

	rec := &timerRecorder{ServiceManager: services.Mgr}
	prev := services.Mgr
	services.Mgr = rec
	t.Cleanup(func() { services.Mgr = prev })

	restoreSiteInfrastructure()

	got := strings.Join(rec.written, " ")
	if !strings.Contains(got, "servlo-cron-acme-prune") {
		t.Errorf("the site's scheduled command was not restored; timers written: %v", rec.written)
	}
	if !strings.Contains(got, "backup") {
		t.Errorf("the site's backup schedule was not restored; timers written: %v", rec.written)
	}
}
