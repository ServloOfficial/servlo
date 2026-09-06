package cli

import (
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/services"
)

type cronRecorder struct {
	services.ServiceManager
	calls []string
}

func (m *cronRecorder) Stop(name string) error { m.calls = append(m.calls, "stop:"+name); return nil }
func (m *cronRecorder) Disable(name string) error {
	m.calls = append(m.calls, "disable:"+name)
	return nil
}

func TestPauseSite_StopsTheSitesScheduledCommands(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	site := config.Site{
		Name: "acme", Domains: []string{"acme-supply.com"}, Path: t.TempDir(), PHPVersion: "8.4",
		Cron: []config.CronEntry{{ID: "prune", Name: "Prune", Command: "php artisan prune", Calendar: "daily"}},
	}
	if err := config.AddSite(site); err != nil {
		t.Fatal(err)
	}

	rec := &cronRecorder{ServiceManager: services.Mgr}
	prev := services.Mgr
	services.Mgr = rec
	t.Cleanup(func() { services.Mgr = prev })

	_ = PauseSite("acme")

	if !strings.Contains(strings.Join(rec.calls, " "), "servlo-cron-acme-prune.timer") {
		t.Errorf("pausing left the site's timers running; calls were %v", rec.calls)
	}
}
