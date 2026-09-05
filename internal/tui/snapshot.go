package tui

import (
	"sort"
	"strings"

	"github.com/ServloOfficial/servlo/internal/config"
	phpPkg "github.com/ServloOfficial/servlo/internal/php"
	"github.com/ServloOfficial/servlo/internal/podman"
	"github.com/ServloOfficial/servlo/internal/siteinfo"
	servloSystemd "github.com/ServloOfficial/servlo/internal/systemd"
)

// Snapshot is the full view-model the TUI renders from. Produced by
// loadSnapshot from the same sources servlo-panel uses, so every pane reflects the
// same point-in-time state.
type Snapshot struct {
	Sites    []siteinfo.EnrichedSite
	Services []ServiceRow
	Status   StatusRow
	// Workspaces group sites for display only, in the order the user's config
	// lists them. The TUI shows them and never edits them.
	Workspaces []config.Workspace
}

// ServiceRow is a flat row for the services pane. Sourced from podman unit
// status and service config so the TUI doesn't need to reach into ui/server.go.
// For site-owned workers (queue, schedule, horizon, reverb, custom framework
// workers) WorkerSite / WorkerKind are set so actions can run the right
// per-site command instead of `servlo service start/stop` which would fail.
type ServiceRow struct {
	Name      string
	Version   string
	State     ServiceState
	Custom    bool
	Pinned    bool
	SiteCount int
	Dashboard string
	DependsOn []string

	WorkerSite string
	WorkerKind string
	WorkerPath string
}

// ServiceState is a tri-state the TUI uses to pick a glyph and colour.
type ServiceState int

const (
	stateStopped ServiceState = iota
	stateRunning
	statePaused
)

// StatusRow drives the top header bar.
type StatusRow struct {
	TLD            string
	NginxRunning   bool
	WatcherRunning bool
	PHPRunning     []string
	Version        string
	Autostart      bool
}

// loadSnapshot collects everything the TUI needs in one shot. Called on every
// refresh tick and on every eventbus publish.
func loadSnapshot() Snapshot {
	snap := Snapshot{}

	enriched, err := siteinfo.LoadAll(siteinfo.EnrichUI)
	if err == nil {
		_ = siteinfo.PersistVersionChanges(enriched)
		sort.Slice(enriched, func(i, j int) bool { return enriched[i].Name < enriched[j].Name })
		snap.Sites = enriched
	}

	snap.Workspaces, _ = config.ListWorkspaces()

	snap.Services = loadServices()
	// Workers live on sites but belong in the services pane too — same
	// operational surface servlo-panel surfaces as "active" rows for queue,
	// schedule, etc. Append them after the shared services so the user
	// sees the full running-units picture in one list.
	snap.Services = append(snap.Services, workerRows(snap.Sites)...)
	snap.Status = loadStatus()
	return snap
}

// workerRows synthesises one ServiceRow per active / defined worker across
// every enriched site. Naming matches the web UI (`queue-<site>`, etc.) so
// users moving between surfaces see familiar identifiers.
func workerRows(sites []siteinfo.EnrichedSite) []ServiceRow {
	var out []ServiceRow
	add := func(site siteinfo.EnrichedSite, kind string, running, failing bool) {
		state := stateStopped
		switch {
		case failing:
			state = stateStopped
		case running:
			state = stateRunning
		}
		out = append(out, ServiceRow{
			Name:       kind + "-" + site.Name,
			State:      state,
			WorkerSite: site.Name,
			WorkerKind: kind,
			WorkerPath: site.Path,
			SiteCount:  1,
		})
	}
	for _, s := range sites {
		if s.HasQueueWorker {
			add(s, "queue", s.QueueRunning, s.QueueFailing)
		}
		if s.HasScheduleWorker {
			add(s, "schedule", s.ScheduleRunning, s.ScheduleFailing)
		}
		if s.HasHorizon {
			add(s, "horizon", s.HorizonRunning, s.HorizonFailing)
		}
		if s.HasReverb {
			add(s, "reverb", s.ReverbRunning, s.ReverbFailing)
		}
		for _, fw := range s.FrameworkWorkers {
			switch fw.Name {
			case "queue", "schedule", "horizon", "reverb":
				continue
			}
			add(s, fw.Name, fw.Running, fw.Failing)
		}
	}
	return out
}

func loadServices() []ServiceRow {
	rows := make([]ServiceRow, 0, 16)
	for _, name := range siteinfo.KnownServices() {
		rows = append(rows, buildServiceRow(name, false))
	}
	customs, _ := config.ListCustomServices()
	for _, svc := range customs {
		r := buildServiceRow(svc.Name, true)
		r.DependsOn = svc.DependsOn
		r.Dashboard = svc.Dashboard
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows
}

func buildServiceRow(name string, custom bool) ServiceRow {
	unit := "servlo-" + name
	status, _ := podman.UnitStatus(unit)
	state := stateStopped
	switch status {
	case "active", "activating":
		state = stateRunning
	}
	if config.ServiceIsPaused(name) {
		state = statePaused
	}
	version := podman.ServiceVersionLabel(podman.InstalledImage(unit))
	if custom && version == "" {
		if svc, err := config.LoadCustomService(name); err == nil {
			version = podman.ServiceVersionLabel(svc.Image)
		}
	}
	return ServiceRow{
		Name:      name,
		Version:   version,
		State:     state,
		Custom:    custom,
		Pinned:    config.ServiceIsPinned(name),
		SiteCount: config.CountSitesUsingService(name),
		// Default presets (phpMyAdmin, …) carry a dashboard URL too; custom
		// services have theirs set by the caller. Populating it here lets the
		// detail pane offer "open dashboard" for built-ins, not just customs.
		Dashboard: config.DefaultPresetDashboard(name),
	}
}

func loadStatus() StatusRow {
	cfg, _ := config.LoadGlobal()
	tld := "test"
	if cfg != nil && cfg.DNS.TLD != "" {
		tld = cfg.DNS.TLD
	}
	row := StatusRow{
		TLD:          tld,
		NginxRunning: podman.Cache.Running("servlo-nginx"),
		Autostart:    servloSystemd.IsAutostartEnabled(),
	}

	if versions, err := phpPkg.ListInstalled(); err == nil {
		for _, v := range versions {
			short := strings.ReplaceAll(v, ".", "")
			if podman.Cache.Running("servlo-php" + short + "-fpm") {
				row.PHPRunning = append(row.PHPRunning, v)
			}
		}
	}

	st, _ := podman.UnitStatus("servlo-watcher")
	row.WatcherRunning = st == "active" || st == "activating"

	return row
}
