package main

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/siteops"
)

func isolateConfig(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, ".local", "share"))
	for _, d := range []string{
		config.ConfigDir(),
		config.DataDir(),
		config.NginxConfD(),
	} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
}

// A deleted host-proxy site must have its dev-server worker torn down, not just
// its registry entry removed, or the always-restart unit leaks. removeStale
// routes through siteops.UnlinkSiteCore, which calls the StopSiteWorkers hook.
func TestRemoveStale_tearsDownStaleHostProxyWorkers(t *testing.T) {
	isolateConfig(t)

	prev := siteops.StopSiteWorkers
	var stopped []string
	siteops.StopSiteWorkers = func(s *config.Site) { stopped = append(stopped, s.Name) }
	t.Cleanup(func() { siteops.StopSiteWorkers = prev })

	deletedDir := filepath.Join(t.TempDir(), "ghost")
	reg := &config.SiteRegistry{Sites: []config.Site{
		{Name: "ghost", Domains: []string{"ghost.test"}, Path: deletedDir, HostPort: 5197, HostCommand: "sleep 600"},
	}}
	if err := config.SaveSites(reg); err != nil {
		t.Fatal(err)
	}

	if !removeStale(&config.GlobalConfig{}) {
		t.Fatal("expected removeStale to report a removal")
	}
	if len(stopped) != 1 || stopped[0] != "ghost" {
		t.Errorf("removeStale must stop a stale host-proxy site's workers; stopped=%v", stopped)
	}
	if after, _ := config.LoadSites(); len(after.Sites) != 0 {
		t.Errorf("stale site should be removed from the registry; got %d sites", len(after.Sites))
	}
}

func TestRemoveStale_removesDeletedNonParkedSite(t *testing.T) {
	isolateConfig(t)

	liveDir := t.TempDir()
	deletedDir := filepath.Join(t.TempDir(), "ghost")

	reg := &config.SiteRegistry{Sites: []config.Site{
		{Name: "live", Domains: []string{"live.test"}, Path: liveDir},
		{Name: "ghost", Domains: []string{"ghost.test"}, Path: deletedDir},
	}}
	if err := config.SaveSites(reg); err != nil {
		t.Fatal(err)
	}

	if !removeStale(&config.GlobalConfig{}) {
		t.Fatal("expected removeStale to report a removal")
	}

	after, err := config.LoadSites()
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(after.Sites))
	for _, s := range after.Sites {
		names = append(names, s.Name)
	}
	if len(names) != 1 || names[0] != "live" {
		t.Errorf("expected only [live] after sweep, got %v", names)
	}
}

func TestRemoveStale_keepsLiveSite(t *testing.T) {
	isolateConfig(t)

	liveDir := t.TempDir()
	reg := &config.SiteRegistry{Sites: []config.Site{
		{Name: "live", Domains: []string{"live.test"}, Path: liveDir},
	}}
	if err := config.SaveSites(reg); err != nil {
		t.Fatal(err)
	}

	if removeStale(&config.GlobalConfig{}) {
		t.Errorf("expected no removals when all site dirs exist")
	}
	after, _ := config.LoadSites()
	if len(after.Sites) != 1 {
		t.Errorf("expected live site preserved, got %d sites", len(after.Sites))
	}
}

func TestRemoveStale_skipsIgnoredSites(t *testing.T) {
	isolateConfig(t)

	// Ignored site with a deleted path should NOT be touched — the user has
	// intentionally parked it in the "ignored" state and the sweep shouldn't
	// reap it out from under them.
	reg := &config.SiteRegistry{Sites: []config.Site{
		{Name: "archived", Domains: []string{"archived.test"}, Path: "/var/empty/does-not-exist", Ignored: true},
	}}
	if err := config.SaveSites(reg); err != nil {
		t.Fatal(err)
	}

	if removeStale(&config.GlobalConfig{}) {
		t.Errorf("removeStale should not touch ignored sites")
	}
	after, _ := config.LoadSites()
	if len(after.Sites) != 1 {
		t.Errorf("ignored site should be preserved, got %d sites", len(after.Sites))
	}
}

// Putting a server back is two steps: restore the state, which brings the
// registry back, then restore each site, which brings its directory back.
// Between them every site is registered with nothing on disk, and the sweep
// read that as the operator having deleted their projects. It unregistered the
// sites the rebuild was in the middle of restoring, and the next site archive
// was refused as being of a site that is not on this server, which by then was
// true. CI lost that race in a thirty-second gap; an operator working through a
// dozen archives by hand has a far wider one.
func TestRemoveStale_LeavesARestoreInProgressAlone(t *testing.T) {
	isolateConfig(t)

	reg := &config.SiteRegistry{Sites: []config.Site{
		{Name: "ci-one-example", Domains: []string{"ci-one.example"}, Path: "/var/empty/not-restored-yet"},
	}}
	if err := config.SaveSites(reg); err != nil {
		t.Fatal(err)
	}
	if err := config.MarkRestoring(); err != nil {
		t.Fatal(err)
	}

	if removeStale(&config.GlobalConfig{}) {
		t.Error("the sweep unregistered a site whose directory the operator is still restoring")
	}
	after, _ := config.LoadSites()
	if len(after.Sites) != 1 {
		t.Errorf("expected the site to survive the restore window, got %d sites", len(after.Sites))
	}
}

// The window is a pause, not an off switch: once it closes a directory the
// operator really did delete is swept as before.
func TestRemoveStale_SweepsAgainOnceTheRestoreWindowCloses(t *testing.T) {
	isolateConfig(t)

	// A deleted project, which is a missing directory inside one that is still
	// there. A path whose parent is missing too reads as a directory tree that
	// went away, and the sweep leaves those alone whatever the restore window
	// says.
	reg := &config.SiteRegistry{Sites: []config.Site{
		{Name: "gone", Domains: []string{"gone.example"}, Path: filepath.Join(t.TempDir(), "gone")},
	}}
	if err := config.SaveSites(reg); err != nil {
		t.Fatal(err)
	}
	if err := config.MarkRestoring(); err != nil {
		t.Fatal(err)
	}
	config.ExpireRestoreWindowForTest(t)

	if !removeStale(&config.GlobalConfig{}) {
		t.Error("the sweep stayed off after the restore window closed")
	}
	after, _ := config.LoadSites()
	if len(after.Sites) != 0 {
		t.Errorf("expected the stale site to be swept, got %d sites", len(after.Sites))
	}
}

func TestNotifyReadyThenScan_readinessDoesNotWaitOnTheScan(t *testing.T) {
	release := make(chan struct{})
	// A scan run synchronously would block on release forever, so it is let go
	// on a timer as well and the assertions below report the ordering.
	releaseScan := sync.OnceFunc(func() { close(release) })
	time.AfterFunc(5*time.Second, releaseScan)

	var ready atomic.Bool
	scanDone := make(chan struct{})
	notifyReadyThenScan(
		func() { ready.Store(true) },
		func() { <-release; close(scanDone) },
	)

	if !ready.Load() {
		t.Error("readiness was not signalled before the watcher moved on")
	}
	select {
	case <-scanDone:
		t.Error("readiness waited for the boot scan to finish")
	default:
	}

	releaseScan()
	<-scanDone
}

// A detached volume must not be read as a pile of deleted projects.
//
// The sweep asks os.Stat for each site path and treats ENOENT as "the operator
// deleted this project". A block volume that detaches, or a mount missing from
// fstab after a reboot, leaves the mountpoint as an empty directory, so every
// site under it answers ENOENT at once. What the sweep then does to each is not
// a registry edit: it stops the workers, removes the per-site container, deletes
// the vhost and drops the entry. The volume comes back and the sites are gone,
// which is the one failure a panel for other people's sites cannot have.
//
// A project the operator deleted leaves its parent standing. A subtree that went
// with its mount does not, which is the difference the sweep can see.
func TestRemoveStale_leavesSitesWhoseParentWentWithThem(t *testing.T) {
	isolateConfig(t)

	// The mountpoint exists and is empty, as it is after an unmount.
	mount := t.TempDir()
	sitesDir := filepath.Join(mount, "sites")

	reg := &config.SiteRegistry{Sites: []config.Site{
		{Name: "acme", Domains: []string{"acme.example"}, Path: filepath.Join(sitesDir, "acme")},
		{Name: "beta", Domains: []string{"beta.example"}, Path: filepath.Join(sitesDir, "beta")},
	}}
	if err := config.SaveSites(reg); err != nil {
		t.Fatal(err)
	}

	if removeStale(&config.GlobalConfig{}) {
		t.Error("the sweep unregistered sites whose whole directory tree is missing, which is what a detached volume looks like")
	}
	after, err := config.LoadSites()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Sites) != 2 {
		t.Errorf("kept %d of 2 sites after the mount went away", len(after.Sites))
	}
}

// The ordinary case still sweeps: one project deleted out of a directory that
// is still there.
func TestRemoveStale_removesOneDeletedProjectFromALiveParent(t *testing.T) {
	isolateConfig(t)

	sitesDir := t.TempDir()
	live := filepath.Join(sitesDir, "live")
	if err := os.MkdirAll(live, 0755); err != nil {
		t.Fatal(err)
	}

	reg := &config.SiteRegistry{Sites: []config.Site{
		{Name: "live", Domains: []string{"live.example"}, Path: live},
		{Name: "gone", Domains: []string{"gone.example"}, Path: filepath.Join(sitesDir, "gone")},
	}}
	if err := config.SaveSites(reg); err != nil {
		t.Fatal(err)
	}

	if !removeStale(&config.GlobalConfig{}) {
		t.Fatal("a project deleted from a directory that still exists must still be swept")
	}
	after, _ := config.LoadSites()
	if len(after.Sites) != 1 || after.Sites[0].Name != "live" {
		t.Errorf("after the sweep = %+v, want only the live site", after.Sites)
	}
}
