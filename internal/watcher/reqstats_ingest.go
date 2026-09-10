package watcher

import (
	"net"
	"os"
	"sync"
	"time"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/push"
	"github.com/ServloOfficial/servlo/internal/reqstats"
)

// The nginx access feed is a single datagram socket every served request is
// written to. It drives the request-timing analytics: a rolling in-memory
// aggregate the panel reads live, and a durable SQLite record behind the
// per-site Request timing view.

// reqStatsSaveInterval is how often the request-timing snapshot is flushed to
// disk for servlo-panel. Shorter than the idle tick so the panel feels live.
const reqStatsSaveInterval = 10 * time.Second

// reqStatsPruneInterval throttles how often rows past reqstats.Retention are
// pruned, which is what keeps the DB small.
const reqStatsPruneInterval = time.Hour

// accessFeedRetryInterval paces the rebind attempts when the feed socket can't
// be bound at boot.
const accessFeedRetryInterval = 30 * time.Second

// slowNotifier fires a one-time push per newly-flagged slow route on each save
// tick.
var slowNotifier = newSlowRouteNotifier()

// reqAggregator holds the rolling per-site request-timing windows the access
// feed fills, persisted to config.RequestStatsFile() for servlo-panel to read.
var reqAggregator *reqstats.Aggregator

// reqStore is the durable SQLite record of individual requests, written from the
// access feed for the analytics view. Buffered between save ticks and flushed in
// one batch so a busy site isn't a transaction per request. Best-effort: a failed
// open leaves it nil and the aggregator snapshot still drives the live panel.
var (
	reqStore    *reqstats.Store
	reqBufMu    sync.Mutex
	reqBuf      []reqstats.Record
	lastPrune   time.Time
	reqLastSeen = map[string]time.Time{} // per-site last request time, for cold-start detection
	coldGap     time.Duration            // a gap this long or longer marks the next request a cold start
)

// StartRequestStats wires the request-timing subsystem: the aggregate the panel
// reads live, the durable store behind the Request timing view, and the
// always-on access-feed reader.
func StartRequestStats() {
	reqAggregator = reqstats.New(resolveHostToStatsKey)
	if st, err := reqstats.OpenStore(config.RequestStatsDB()); err == nil {
		reqStore = st
		// Seed the cold-start clock from the durable store so the first request
		// after a daemon restart is judged against the real last-seen time instead
		// of counting the wake as warm and skewing the route p95.
		if seen, err := st.LastSeenBySite(); err == nil {
			reqLastSeen = seen
		}
	}
	coldGap = reqstats.DefaultColdGap
	startAccessFeed()
	go runReqStatsSaver()
}

// handleAccessDatagram parses one nginx access datagram into the timing stats.
func handleAccessDatagram(b []byte) {
	if reqAggregator == nil {
		return
	}
	if rec, ok := reqstats.ParseAccessRecord(b); ok {
		ingestAccessRecord(rec)
	}
}

// siteForHost is the seam the request-stats fan-out resolves through; a var so a
// test can inject a resolver without a live site registry.
var siteForHost = resolveHostToStatsKey

// ingestAccessRecord fans one parsed access record out to the durable store and
// the live aggregator. It flags a cold start (the first request after the site
// sat idle past coldGap) and keeps it out of the aggregator, whose snapshot feeds
// the slow-route notifier and the doctor: a wake's inflated time must not trip
// them, while the store still records it (marked cold, excluded from its timing).
func ingestAccessRecord(rec reqstats.AccessRecord) {
	appServed := reqstats.IsAppRequest(rec.Status, rec.URI, rec.SecondsToMillis())
	site, resolved := siteForHost(rec.Host)
	cold := false
	if appServed && resolved {
		now := time.Now()
		reqBufMu.Lock()
		last, seen := reqLastSeen[site]
		cold = reqstats.IsColdStart(last, seen, now, coldGap)
		reqLastSeen[site] = now
		if reqStore != nil {
			sr := reqstats.RecordFrom(rec, site, now)
			sr.Cold = cold
			reqBuf = append(reqBuf, sr)
		}
		reqBufMu.Unlock()
	}
	if !cold {
		reqAggregator.Record(rec)
	}
}

// runReqStatsSaver flushes the request-timing snapshot to disk on a fixed tick
// for servlo-panel to read, and fires a one-time push for any route newly flagged as
// slow. Runs for the daemon's life, independent of idle. push.Send is a no-op
// when no subscription has opted into the slow_route kind.
func runReqStatsSaver() {
	t := time.NewTicker(reqStatsSaveInterval)
	defer t.Stop()
	for range t.C {
		if reqAggregator == nil {
			continue
		}
		snap := forgetUnregisteredSites(reqAggregator, reqAggregator.Snapshot())
		_ = reqstats.SaveSnapshot(snap, config.RequestStatsFile())
		flushReqStore()
		domainOf := lazyResolver(siteDomainResolver)
		for _, n := range slowNotifier.notifications(snap, domainOf) {
			_ = push.Send(n)
		}
	}
}

// flushReqStore writes the buffered access records to the durable store in one
// batch and prunes rows past the retention window on a throttled cadence, so the
// store stays bounded without a delete on every tick.
func flushReqStore() {
	if reqStore == nil {
		return
	}
	reqBufMu.Lock()
	batch := reqBuf
	reqBuf = nil
	reqBufMu.Unlock()
	if len(batch) > 0 {
		_ = reqStore.Insert(batch)
	}
	if now := time.Now(); now.Sub(lastPrune) >= reqStatsPruneInterval {
		_, _ = reqStore.Prune(now.Add(-reqstats.Retention))
		lastPrune = now
	}
}

// startAccessFeed binds the always-on access-feed reader. A bind that fails at
// boot (a transient FS/permission hiccup on the socket path) is retried in the
// background so the feed and its consumers recover on their own; until it binds
// both consumers degrade to their other signals.
func startAccessFeed() {
	if conn, ok := accessFeedConn(); ok {
		go readDatagrams(conn, handleAccessDatagram)
		return
	}
	go func() {
		t := time.NewTicker(accessFeedRetryInterval)
		defer t.Stop()
		for range t.C {
			if conn, ok := accessFeedConn(); ok {
				go readDatagrams(conn, handleAccessDatagram)
				return
			}
		}
	}()
}

// accessFeedConn binds the nginx access feed listener: a unix datagram socket,
// bind-mounted into the rootless nginx container.
func accessFeedConn() (net.PacketConn, bool) {
	return listenDatagram(config.AccessSocketPath())
}

// listenDatagram binds a unix datagram socket at path under RunDir, replacing any
// stale one. ok=false on failure, so callers skip.
// 0660 matches nginx's writer uid on the access socket and the UI stream socket.
func listenDatagram(path string) (net.PacketConn, bool) {
	if err := os.MkdirAll(config.RunDir(), 0755); err != nil {
		return nil, false
	}
	_ = os.Remove(path)
	conn, err := net.ListenPacket("unixgram", path)
	if err != nil {
		return nil, false
	}
	_ = os.Chmod(path, 0660)
	return conn, true
}

// readDatagrams delivers each datagram to handle until the socket closes (daemon
// shutdown), at which point ReadFrom errors and the loop exits.
func readDatagrams(conn net.PacketConn, handle func([]byte)) {
	buf := make([]byte, 4096)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			return
		}
		handle(buf[:n])
	}
}

// resolveHostToStatsKey maps a request host to its request-store key.
func resolveHostToStatsKey(host string) (string, bool) {
	return siteNameForHost(host)
}

// siteNameForHost resolves a host to the site that owns it, ok=false when no
// registered site does.
func siteNameForHost(host string) (string, bool) {
	site, err := config.FindSiteByDomain(host)
	if err != nil || site == nil {
		return "", false
	}
	return site.Name, true
}

// forgetUnregisteredSites drops every site the registry no longer carries, from
// the snapshot about to be written and from the aggregator holding it.
//
// Unlinking a site clears its rows from the snapshot file and the durable store,
// and this process keeps its rolling windows in memory, so without this the next
// tick writes the site straight back and it stays in the panel until the watcher
// restarts. The comment on the unlink path said a control socket told the
// watcher; that socket went with the idle engine and nothing has told it since.
//
// Read here rather than sent from there, because an unlink happens in whichever
// process the operator used, the panel, the CLI over SSH or this one, and the
// registry is the truth all three already share. It also heals a site removed
// while the watcher was down, which no message could.
//
// A paused or ignored site is still registered and is kept. A registry that
// cannot be read drops nothing: losing one read must not empty the snapshot of
// every site on the machine.
func forgetUnregisteredSites(agg *reqstats.Aggregator, snap []reqstats.SiteStats) []reqstats.SiteStats {
	reg, err := config.LoadSites()
	if err != nil || reg == nil {
		return snap
	}
	registered := make(map[string]bool, len(reg.Sites))
	for _, s := range reg.Sites {
		registered[s.Name] = true
	}
	kept := make([]reqstats.SiteStats, 0, len(snap))
	for _, st := range snap {
		if registered[st.Site] {
			kept = append(kept, st)
			continue
		}
		agg.Forget(st.Site)
	}
	return kept
}
