// Package monitor is the loop that keeps the alert list honest.
//
// Most alerts are raised by the thing that failed, at the moment it failed: a
// deploy raises its own, a backup raises its own. Three cannot work that way,
// because nothing fails at a moment an operator could be told about. A site
// that stops answering, a worker that quietly went down and a disk filling up
// all have to be looked for, so this looks for them on a timer.
//
// Every check clears as well as raises. That is the half that makes the list
// worth reading: an alert that stays after the thing it is about recovered
// teaches an operator to ignore the list, which costs more than the alert was
// ever worth.
package monitor

import (
	"fmt"
	"log"
	"syscall"
	"time"

	"github.com/realrashid/servlo/internal/alerts"
	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/uptime"
	"github.com/realrashid/servlo/internal/workerheal"
)

// Interval is how often the checks run.
//
// Five minutes rather than thirty seconds. A site that has been down for four
// minutes is down either way, nobody acts inside that window, and the cheap
// option is a check that does not add a request per site per half minute to a
// droplet that is already the thing being measured.
const Interval = 5 * time.Minute

// DiskWarnPercent is how full is full enough to say something.
//
// Ninety, not ninety-nine. The point of a disk alert is the time it buys: at
// ninety-nine percent a database has already failed a write, and being told
// then is being told after the outage rather than before it.
const DiskWarnPercent = 90

// Seams. Every one of these reaches the machine, and none of them can in a
// test, so each is replaceable and the loop above them is the real thing.
var (
	loadSites      = config.LoadSites
	loadFramework  = frameworkFor
	checkSite      = uptime.Check
	detectWorkers  = workerheal.Detect
	diskUsed       = diskUsedPercent
	raise          = alerts.Raise
	clearAlert     = alerts.Clear
	monitoredPaths = defaultMonitoredPaths
)

// Watch runs the checks until the process ends.
func Watch(interval time.Duration) {
	// Once at startup as well as on the tick. A watcher that restarts more
	// often than the interval would otherwise never check anything, and a
	// server that has just come back from a reboot is exactly when a site that
	// did not come back with it needs finding.
	Run()
	t := time.NewTicker(interval)
	defer t.Stop()
	for range t.C {
		Run()
	}
}

// Run is one pass of every check.
func Run() {
	checkSites()
	checkWorkers()
	checkDisk()
}

// checkSites asks every site whether it is answering, and moves its alert to
// match.
func checkSites() {
	reg, err := loadSites()
	if err != nil {
		log.Printf("[monitor] could not read the sites: %v", err)
		return
	}
	for i := range reg.Sites {
		site := &reg.Sites[i]
		// A paused site is meant not to be answering. Reporting it down would
		// be reporting the operator's own decision back at them as a fault.
		if site.Paused {
			_ = clearAlert(alerts.KindSiteDown, site.Name)
			continue
		}
		res := checkSite(site, loadFramework(site))
		if res.Up {
			_ = clearAlert(alerts.KindSiteDown, site.Name)
			continue
		}
		report(alerts.Alert{
			Kind:    alerts.KindSiteDown,
			Site:    site.Name,
			Message: fmt.Sprintf("%s\n\nChecked %s from this server.", res.Reason, res.URL),
		})
	}
}

// checkWorkers reports a worker that is not running when it should be.
//
// One alert per site rather than per worker: a site whose queue, schedule and
// horizon all went down at once went down for one reason, and three emails
// about it is three times the noise for the same news.
func checkWorkers() {
	unhealthy, err := detectWorkers()
	if err != nil {
		log.Printf("[monitor] could not check the workers: %v", err)
		return
	}
	bySite := map[string][]workerheal.UnhealthyWorker{}
	for _, w := range unhealthy {
		bySite[w.Site] = append(bySite[w.Site], w)
	}

	reg, err := loadSites()
	if err != nil {
		return
	}
	for i := range reg.Sites {
		name := reg.Sites[i].Name
		down, bad := bySite[name]
		if !bad {
			_ = clearAlert(alerts.KindWorkerDown, name)
			continue
		}
		var detail string
		for _, w := range down {
			detail += fmt.Sprintf("%s is %s", w.Worker, workerheal.HumanState(w.State))
			if w.LastError != "" {
				detail += ": " + w.LastError
			}
			detail += "\n"
		}
		report(alerts.Alert{Kind: alerts.KindWorkerDown, Site: name, Message: detail})
	}
}

// checkDisk reports a filesystem close enough to full to be worth acting on.
func checkDisk() {
	worst, worstPath := 0, ""
	for _, path := range monitoredPaths() {
		used, err := diskUsed(path)
		if err != nil {
			continue
		}
		if used > worst {
			worst, worstPath = used, path
		}
	}
	if worstPath == "" {
		return
	}
	if worst < DiskWarnPercent {
		_ = clearAlert(alerts.KindDiskFilling, "")
		return
	}
	report(alerts.Alert{
		Kind: alerts.KindDiskFilling,
		Message: fmt.Sprintf("%s is %d%% full.\n\nBackups, container images and logs are "+
			"what usually fill a droplet. servlo cleanup reclaims unused images, and "+
			"servlo backup list shows what retention is keeping.", worstPath, worst),
	})
}

// report raises an alert and says so if it could not. A monitor that silently
// fails to record what it found is worse than one that never ran, because the
// empty list reads as good news.
func report(a alerts.Alert) {
	if err := raise(a); err != nil {
		log.Printf("[monitor] could not record the %s alert: %v", a.Kind, err)
	}
}

// defaultMonitoredPaths are the filesystems worth watching: where servlo keeps
// its data and its archives, and where each site lives.
//
// Deduplicated by the filesystem rather than by the path, because a droplet
// with everything on one root volume would otherwise raise the same alert once
// per site, and a server with sites on a separate volume genuinely has two
// disks that can fill independently.
func defaultMonitoredPaths() []string {
	candidates := []string{config.DataDir(), config.SiteBackupsDir()}
	if reg, err := loadSites(); err == nil {
		for i := range reg.Sites {
			candidates = append(candidates, reg.Sites[i].Path)
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, path := range candidates {
		var st syscall.Statfs_t
		if err := syscall.Statfs(path, &st); err != nil {
			continue
		}
		id := fmt.Sprint(st.Fsid)
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, path)
	}
	return out
}

// diskUsedPercent is how full the filesystem holding a path is.
func diskUsedPercent(path string) (int, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	used, ok := usedPercent(st.Blocks, st.Bfree, st.Bavail)
	if !ok {
		return 0, fmt.Errorf("%s reports no usable space at all", path)
	}
	return used, nil
}

// usedPercent is how full a filesystem is, measured against the space a
// non-root process can actually use rather than against the total.
//
// The difference is the reserved blocks: ext4 keeps five percent for root by
// default, and it is space servlo will never get. Counting it as free is how a
// disk reports five percent left while every write servlo makes is already
// failing, which is exactly the moment the alert was supposed to have fired.
func usedPercent(blocks, free, avail uint64) (int, bool) {
	if free < avail || blocks < free {
		// Not a filesystem as statfs describes one. Refusing to answer beats
		// underflowing into a reassuring number.
		return 0, false
	}
	usable := blocks - (free - avail)
	if usable == 0 {
		return 0, false
	}
	return int((usable - avail) * 100 / usable), true
}

// frameworkFor is the site's framework definition, or nil when it has none that
// can be read. A site whose framework is unknown is still checked, against its
// home page.
func frameworkFor(site *config.Site) *config.Framework {
	if site.Framework == "" {
		return nil
	}
	fw, ok := config.GetFramework(site.Framework)
	if !ok {
		return nil
	}
	return fw
}
