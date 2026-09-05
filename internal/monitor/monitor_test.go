package monitor

import (
	"errors"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/alerts"
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/uptime"
	"github.com/ServloOfficial/servlo/internal/workerheal"
)

// harness replaces everything that reaches the machine, and records what the
// loop above them decided.
type harness struct {
	raised  []alerts.Alert
	cleared []string
}

func stub(t *testing.T, sites ...config.Site) *harness {
	t.Helper()
	h := &harness{}

	restore := []func(){
		swap(&loadSites, func() (*config.SiteRegistry, error) {
			return &config.SiteRegistry{Sites: sites}, nil
		}),
		swap(&loadFramework, func(*config.Site) *config.Framework { return nil }),
		swap(&checkSite, func(s *config.Site, _ *config.Framework) uptime.Result {
			return uptime.Result{Site: s.Name, Up: true}
		}),
		swap(&detectWorkers, func() ([]workerheal.UnhealthyWorker, error) { return nil, nil }),
		swap(&monitoredPaths, func() []string { return nil }),
		swap(&raise, func(a alerts.Alert) error { h.raised = append(h.raised, a); return nil }),
		swap(&clearAlert, func(kind, site string) error {
			h.cleared = append(h.cleared, kind+"/"+site)
			return nil
		}),
	}
	t.Cleanup(func() {
		for _, r := range restore {
			r()
		}
	})
	return h
}

func swap[T any](target *T, with T) func() {
	prev := *target
	*target = with
	return func() { *target = prev }
}

func (h *harness) raisedKinds() []string {
	var out []string
	for _, a := range h.raised {
		out = append(out, a.Kind+"/"+a.Site)
	}
	return out
}

func site(name string) config.Site {
	return config.Site{Name: name, Path: "/srv/" + name, Domains: []string{name + ".example"}}
}

// A site that stopped answering is the alert the whole loop exists for: nothing
// fails at a moment anyone could be told about, so it has to be looked for.
func TestRun_RaisesWhenASiteStopsAnswering(t *testing.T) {
	h := stub(t, site("acme"))
	swap(&checkSite, func(s *config.Site, _ *config.Framework) uptime.Result {
		return uptime.Result{Site: s.Name, URL: "https://acme.example/up", Reason: "the site answered 502 Bad Gateway"}
	})

	Run()

	if len(h.raised) != 1 || h.raised[0].Kind != alerts.KindSiteDown {
		t.Fatalf("raised %v, want one site-down alert", h.raisedKinds())
	}
	if !strings.Contains(h.raised[0].Message, "502") {
		t.Errorf("the alert does not say what happened:\n%s", h.raised[0].Message)
	}
	// The URL is in the message so an operator can reproduce the check by hand
	// rather than guess what servlo asked for.
	if !strings.Contains(h.raised[0].Message, "acme.example/up") {
		t.Errorf("the alert does not say what was checked:\n%s", h.raised[0].Message)
	}
}

// The clearing half is what makes the list worth reading. An alert that stays
// after the site came back teaches an operator to ignore the list.
func TestRun_ClearsWhenTheSiteComesBack(t *testing.T) {
	h := stub(t, site("acme"))
	Run()

	if len(h.raised) != 0 {
		t.Errorf("a site that is up raised %v", h.raisedKinds())
	}
	if !contains(h.cleared, alerts.KindSiteDown+"/acme") {
		t.Errorf("cleared %v, want the site-down alert cleared", h.cleared)
	}
}

// A paused site is meant not to be answering. Reporting it down is reporting
// the operator's own decision back at them as a fault, every five minutes.
func TestRun_DoesNotReportAPausedSiteAsDown(t *testing.T) {
	paused := site("acme")
	paused.Paused = true
	h := stub(t, paused)
	swap(&checkSite, func(s *config.Site, _ *config.Framework) uptime.Result {
		t.Error("a paused site was checked")
		return uptime.Result{}
	})

	Run()

	if len(h.raised) != 0 {
		t.Errorf("a paused site raised %v", h.raisedKinds())
	}
}

// A worker that quietly went down is the other failure nobody is told about at
// the time.
func TestRun_RaisesWhenAWorkerIsDown(t *testing.T) {
	h := stub(t, site("acme"), site("shopfront"))
	swap(&detectWorkers, func() ([]workerheal.UnhealthyWorker, error) {
		return []workerheal.UnhealthyWorker{
			{Site: "acme", Worker: "queue", State: "failed", LastError: "connection to redis refused"},
		}, nil
	})

	Run()

	var worker []alerts.Alert
	for _, a := range h.raised {
		if a.Kind == alerts.KindWorkerDown {
			worker = append(worker, a)
		}
	}
	if len(worker) != 1 || worker[0].Site != "acme" {
		t.Fatalf("raised %v, want one worker alert against acme", h.raisedKinds())
	}
	if !strings.Contains(worker[0].Message, "queue") || !strings.Contains(worker[0].Message, "redis") {
		t.Errorf("the alert does not name the worker or the reason:\n%s", worker[0].Message)
	}
	// The site whose workers are fine has its alert cleared, not left standing.
	if !contains(h.cleared, alerts.KindWorkerDown+"/shopfront") {
		t.Errorf("cleared %v, want shopfront's worker alert cleared", h.cleared)
	}
}

// A site whose queue, schedule and horizon all went down at once went down for
// one reason. Three emails about it is three times the noise for one piece of
// news.
func TestRun_OneWorkerAlertPerSite(t *testing.T) {
	h := stub(t, site("acme"))
	swap(&detectWorkers, func() ([]workerheal.UnhealthyWorker, error) {
		return []workerheal.UnhealthyWorker{
			{Site: "acme", Worker: "queue", State: "failed"},
			{Site: "acme", Worker: "schedule", State: "failed"},
			{Site: "acme", Worker: "horizon", State: "failed"},
		}, nil
	})

	Run()

	var n int
	for _, a := range h.raised {
		if a.Kind == alerts.KindWorkerDown {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("three down workers on one site raised %d alerts", n)
	}
	for _, want := range []string{"queue", "schedule", "horizon"} {
		if !strings.Contains(h.raised[0].Message, want) {
			t.Errorf("the alert leaves out %s:\n%s", want, h.raised[0].Message)
		}
	}
}

// A disk close to full is the alert with the most warning in it, and the point
// is the time it buys.
func TestRun_RaisesWhenTheDiskIsFilling(t *testing.T) {
	h := stub(t)
	swap(&monitoredPaths, func() []string { return []string{"/", "/srv"} })
	swap(&diskUsed, func(path string) (int, error) {
		if path == "/srv" {
			return 94, nil
		}
		return 40, nil
	})

	Run()

	if len(h.raised) != 1 || h.raised[0].Kind != alerts.KindDiskFilling {
		t.Fatalf("raised %v, want one disk alert", h.raisedKinds())
	}
	// The fullest one, not the first one: a root volume at 40% is not the news.
	if !strings.Contains(h.raised[0].Message, "/srv") || !strings.Contains(h.raised[0].Message, "94") {
		t.Errorf("the alert names the wrong filesystem:\n%s", h.raised[0].Message)
	}
	if h.raised[0].Site != "" {
		t.Errorf("the disk alert is against site %q, which makes a server problem look like a site's", h.raised[0].Site)
	}
}

// Below the threshold the alert goes away, so an operator who freed some space
// sees that it worked.
func TestRun_ClearsTheDiskAlertOnceThereIsRoom(t *testing.T) {
	h := stub(t)
	swap(&monitoredPaths, func() []string { return []string{"/"} })
	swap(&diskUsed, func(string) (int, error) { return 40, nil })

	Run()

	if len(h.raised) != 0 {
		t.Errorf("a disk at 40%% raised %v", h.raisedKinds())
	}
	if !contains(h.cleared, alerts.KindDiskFilling+"/") {
		t.Errorf("cleared %v, want the disk alert cleared", h.cleared)
	}
}

// A filesystem servlo cannot measure is skipped rather than counted as empty.
// Treating an unreadable disk as 0% full is the most reassuring possible lie.
func TestRun_SkipsAFilesystemItCannotMeasure(t *testing.T) {
	h := stub(t)
	swap(&monitoredPaths, func() []string { return []string{"/gone", "/srv"} })
	swap(&diskUsed, func(path string) (int, error) {
		if path == "/gone" {
			return 0, errors.New("no such file or directory")
		}
		return 95, nil
	})

	Run()

	if len(h.raised) != 1 || !strings.Contains(h.raised[0].Message, "/srv") {
		t.Fatalf("raised %v, want the disk that could be measured", h.raisedKinds())
	}
}

// A check that cannot run says nothing rather than saying everything is fine.
// Clearing every site's alert because the registry would not load is how an
// outage becomes invisible.
func TestRun_ClearsNothingWhenItCannotReadTheSites(t *testing.T) {
	h := stub(t)
	swap(&loadSites, func() (*config.SiteRegistry, error) { return nil, errors.New("unreadable") })

	Run()

	if len(h.cleared) != 0 || len(h.raised) != 0 {
		t.Errorf("a monitor that could not read anything raised %v and cleared %v", h.raisedKinds(), h.cleared)
	}
}

// Reserved blocks are not free space. Ext4 keeps five percent for root by
// default, and counting it as available is how a disk reports room left while
// everything servlo runs is already failing to write.
func TestUsedPercent_DoesNotCountRootsReservedBlocksAsFree(t *testing.T) {
	// A 100-block ext4 filesystem with the usual 5% reserve, entirely full as
	// far as anything that is not root is concerned.
	used, ok := usedPercent(100, 5, 0)
	if !ok {
		t.Fatal("a full filesystem could not be measured")
	}
	if used != 100 {
		t.Errorf("a disk with nothing left for servlo reports %d%% full", used)
	}

	// The same filesystem half empty, to prove the correction is not simply
	// pinning everything at 100.
	if used, _ := usedPercent(100, 55, 50); used != 47 {
		t.Errorf("a half-empty disk reports %d%% full, want 47", used)
	}
}

// Numbers statfs would never produce are refused rather than underflowed into a
// reassuring answer.
func TestUsedPercent_RefusesNonsenseRatherThanReportingRoom(t *testing.T) {
	if _, ok := usedPercent(0, 0, 0); ok {
		t.Error("a filesystem with no blocks at all was measured")
	}
	if _, ok := usedPercent(100, 0, 10); ok {
		t.Error("a filesystem with more available than free was measured")
	}
	if _, ok := usedPercent(10, 100, 100); ok {
		t.Error("a filesystem with more free than it has was measured")
	}
}

// And the real syscall still answers for a real path.
func TestDiskUsedPercent_MeasuresARealFilesystem(t *testing.T) {
	used, err := diskUsedPercent(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if used < 0 || used > 100 {
		t.Errorf("a filesystem is %d%% full", used)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
