package ui

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "embed"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/realrashid/servlo/internal/applog"
	"github.com/realrashid/servlo/internal/authz"
	"github.com/realrashid/servlo/internal/cfgedit"
	"github.com/realrashid/servlo/internal/cli"
	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/envfile"
	"github.com/realrashid/servlo/internal/eventbus"
	"github.com/realrashid/servlo/internal/grouping"
	"github.com/realrashid/servlo/internal/nginx"
	servloNode "github.com/realrashid/servlo/internal/node"
	phpPkg "github.com/realrashid/servlo/internal/php"
	"github.com/realrashid/servlo/internal/phpsets"
	"github.com/realrashid/servlo/internal/podman"
	"github.com/realrashid/servlo/internal/serviceops"
	"github.com/realrashid/servlo/internal/services"
	"github.com/realrashid/servlo/internal/shims"
	"github.com/realrashid/servlo/internal/sitedoctor"
	"github.com/realrashid/servlo/internal/siteinfo"
	"github.com/realrashid/servlo/internal/siteops"
	servloSystemd "github.com/realrashid/servlo/internal/systemd"
	"github.com/realrashid/servlo/internal/tools"
	servloUpdate "github.com/realrashid/servlo/internal/update"
	"github.com/realrashid/servlo/internal/version"
	"github.com/realrashid/servlo/internal/workerheal"
)

//go:embed icons/icon.svg
var iconSVG []byte

//go:embed icons/icon-maskable.svg
var iconMaskableSVG []byte

//go:embed icons/icon-192.png
var icon192PNG []byte

//go:embed icons/icon-512.png
var icon512PNG []byte

//go:embed icons/icon-maskable-192.png
var iconMaskable192PNG []byte

//go:embed icons/icon-maskable-512.png
var iconMaskable512PNG []byte

//go:embed sw.js
var swJS []byte

//go:embed offline.html
var offlineHTML []byte

// listenAddr is the TCP address servlo-panel binds to. It listens on 0.0.0.0:7073
// so browsers can hit it directly and LAN clients (gated by the remote-control
// middleware) can reach it when lan:expose is on. The gate — not the bind
// address — is the security boundary.
//
// servlo-panel ALSO listens on a unix socket at config.UISocketPath() for the
// servlo.localhost nginx vhost. Bind-mounting a socket into servlo-nginx is more
// reliable than reaching the host over TCP via host.containers.internal,
// which depends on netavark / pasta / rootless routing wiring up 169.254.1.2
// (something that differs across podman versions and breaks silently).
const listenAddr = "0.0.0.0:7073"

// ctxKeyUnixSocket marks HTTP requests that arrived over the unix socket
// listener. These are treated as loopback by isLoopbackRequest since only
// processes with filesystem access to the socket can connect.
type ctxKeyUnixSocket struct{}

// Start starts the HTTP server on listenAddr.
func Start(currentVersion string) error {
	// Every unit lifecycle change (from CLI, HTTP handlers, or the
	// file watcher) funnels through podman.StartUnit/StopUnit/RestartUnit.
	// Hook into that choke point so any mutation — regardless of which
	// surface triggered it — invalidates the systemctl unit cache and
	// pushes a fresh snapshot to every connected browser. The bus debounces,
	// so bursty mutations (e.g. restarting a set of workers) still collapse
	// into one broadcast.
	// Start the container state cache. One background goroutine polls
	// podman ps every N seconds; all hot paths (buildStatus, siteinfo,
	// IsActive) read from the cache instead of spawning per-container
	// podman inspect subprocesses.
	podman.Cache.Start(context.Background())

	// Restart any LAN share proxies that were active before this process started.

	// A public tunnel must not outlive the process that owns it. Stop them on
	// the way out, and kill anything a previous run was killed too hard to
	// clean up itself.
	// A tunnel container is invisible to the pid-based reap: conmon is
	// reparented out of the client's tree, so a killed servlo-panel leaves it
	// running and there is no pid left to recognise it by.

	// Single coalescer for the two event sources that need to refresh the
	// container cache and broadcast a snapshot: in-process mutations
	// (AfterUnitChange) and CLI notifications (/api/internal/notify).
	// systemd's DBus subscription is deliberately not one of them: it wakes on
	// every unit property change on the bus, which costs more at idle than the
	// polling it would replace. A burst of state
	// transitions (e.g. a unit cycling activating→active during start) used to
	// spawn one podman ps subprocess per transition; the coalescer collapses
	// them into one poll + one publish per ~250ms quiet period.
	//
	// When no UI tab is open we don't fork podman ps — runSnapshotInvalidator
	// also skips the rebuild for the same reason, and the periodic cache poll
	// (60s while idle) catches container state on its own. An incoming WS
	// connection forces a fresh PollNow before sending the initial snapshot.
	publisher := newPollPublisher(250*time.Millisecond, func() {
		if visibleClients.Load() > 0 {
			podman.Cache.PollNow()
		}
		siteinfo.InvalidateUnitCache()
		eventbus.Default.Publish(eventbus.KindSites)
		eventbus.Default.Publish(eventbus.KindServices)
		eventbus.Default.Publish(eventbus.KindStatus)
	})

	podman.AfterUnitChange = func(string) { publisher.trigger() }

	// External state changes (container crash, systemctl outside servlo-panel,
	// timer firings) are caught by the periodic podman cache poll instead
	// of a DBus PropertiesChanged subscription. The subscription used to
	// burn ~50% of one core because go-systemd's dispatch goroutine fetches
	// unit properties on every signal, and active containers emit a steady
	// stream of property updates (CPU accounting, exec status, restart
	// counters). Polling at 15s when a UI tab is focused / 60s otherwise
	// trades off-up-to-15s latency for an order-of-magnitude CPU saving.
	podman.Cache.SetOnChange(publisher.trigger)

	// Drop the cache to idle cadence whenever the desktop session is idle
	// or locked, so a focused tab on an unattended laptop still saves
	// battery. Recomputes on every transition.
	startIdleWatcher(context.Background())

	// A single goroutine subscribes to the eventbus and invalidates the
	// relevant snapshot on every mutation. The /api/ws handler broadcasts
	// the freshly rebuilt bytes to every connected browser.
	go runSnapshotInvalidator()

	// systemd transitions a worker to "failed" without telling servlo-panel (e.g.
	// after start-limit-hit on a crash loop). The health watcher closes
	// that gap by polling the cached detector on a slow tick and publishing
	// KindSites only when the unhealthy set actually changes.
	go runWorkerHealthWatcher()

	// WatchDNS lives in the servlo-watcher process; its eventbus publishes
	// don't cross over here. This in-process probe surfaces DNS transitions
	// (notably servlo-dns coming up after a boot where the dashboard opened
	// before resolver was ready) to live WebSocket clients.

	mux := http.NewServeMux()

	// Gated inside the handler (marker file plus loopback), so a daemon
	// without profiling turned on answers as if the route did not exist.
	mux.HandleFunc("/debug/pprof/", handlePprof)

	mux.HandleFunc("/api/status", withCORS(handleStatus))
	mux.HandleFunc("/api/sites", withCORS(handleSites))
	mux.HandleFunc("/api/certs/alerts", withCORS(handleCertAlerts))
	mux.HandleFunc("/api/services", withCORS(handleServices))
	mux.HandleFunc("/api/ws", handleWS)
	mux.HandleFunc("/api/webhooks/deploy/", withCORS(handleWebhookDeploy))
	mux.HandleFunc("/api/push/vapid-public-key", withCORS(handlePushVAPIDPublicKey))
	mux.HandleFunc("/api/push/subscribe", withCORS(handlePushSubscribe))
	mux.HandleFunc("/api/push/unsubscribe", withCORS(handlePushUnsubscribe))
	mux.HandleFunc("/api/push/devices", withCORS(handlePushDevices))
	mux.HandleFunc("/api/push/test", withCORS(handlePushTest))
	mux.HandleFunc("/api/tools/", withCORS(publishAfter(handleTools, eventbus.KindStatus)))
	mux.HandleFunc("/api/dashboard-qr", withCORS(handleDashboardQR))

	// Cross-process notifier for CLI. It requires dashboard-control
	// authority. PollNow runs in a goroutine so the handler returns under the
	// CLI's 500 ms POST timeout while the next WebSocket broadcast refreshes.
	mux.HandleFunc("/api/internal/notify", func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackRequest(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		publisher.trigger()
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/api/services/presets", withCORS(handleServicePresets))
	mux.HandleFunc("/api/services/presets/", withCORS(publishAfter(handleServicePresetInstall, eventbus.KindServices, eventbus.KindStatus)))
	mux.HandleFunc("/api/services/", withCORS(publishAfter(handleServiceAction, eventbus.KindServices, eventbus.KindStatus, eventbus.KindSites)))
	mux.HandleFunc("/api/databases", withCORS(handleDatabases))
	mux.HandleFunc("/api/db-connections", withCORS(handleDBConnections))
	mux.HandleFunc("/api/databases/", withCORS(handleDatabaseAction))
	mux.HandleFunc("/api/entities/", withCORS(handleEntities))
	mux.HandleFunc("/api/version", withCORS(func(w http.ResponseWriter, r *http.Request) {
		handleVersion(w, r, currentVersion)
	}))
	mux.HandleFunc("/api/nginx/", withCORS(handleNginxRoutes))
	mux.HandleFunc("/api/php-versions", withCORS(handlePHPVersions))
	mux.HandleFunc("/api/php-installable", withCORS(handlePHPInstallable))
	mux.HandleFunc("/api/php-versions/install", withCORS(publishAfter(handlePHPInstall, eventbus.KindStatus, eventbus.KindSites)))
	mux.HandleFunc("/api/php-versions/", withCORS(publishAfter(handlePHPVersionAction, eventbus.KindStatus, eventbus.KindSites)))
	mux.HandleFunc("/api/node-versions", withCORS(handleNodeVersions))
	mux.HandleFunc("/api/node-versions/install", withCORS(publishAfter(handleInstallNodeVersion, eventbus.KindStatus)))
	mux.HandleFunc("/api/node-versions/", withCORS(publishAfter(handleNodeVersionAction, eventbus.KindStatus, eventbus.KindSites)))
	mux.HandleFunc("/api/node/manage", withCORS(publishAfter(handleNodeManage, eventbus.KindStatus, eventbus.KindSites)))
	mux.HandleFunc("/api/node/unmanage", withCORS(publishAfter(handleNodeUnmanage, eventbus.KindStatus, eventbus.KindSites)))
	mux.HandleFunc("/api/node/set-manager", withCORS(publishAfter(handleNodeSetManager, eventbus.KindStatus, eventbus.KindSites)))
	mux.HandleFunc("/api/sites/create", withCORS(publishAfter(handleSiteCreate, eventbus.KindSites)))
	mux.HandleFunc("/api/sites/inspect", withCORS(handleSiteInspect))
	mux.HandleFunc("/api/sites/clone", withCORS(publishAfter(handleSiteClone, eventbus.KindSites)))
	mux.HandleFunc("/api/sites/clone-test", withCORS(handleCloneTest))
	mux.HandleFunc("/api/sites/deploy-key", withCORS(handleDeployKey))
	mux.HandleFunc("/api/sites/upload", withCORS(publishAfter(handleSiteUpload, eventbus.KindSites)))
	mux.HandleFunc("/api/sites/reorder", withCORS(publishAfter(handleSiteReorder, eventbus.KindSites)))
	mux.HandleFunc("/api/browse", withCORS(handleBrowse))
	mux.HandleFunc("/api/sftp", withCORS(handleSFTP))
	mux.HandleFunc("/api/sftp/", withCORS(handleSFTPKey))
	mux.HandleFunc("/api/workspaces", withCORS(publishAfter(handleWorkspaces, eventbus.KindStatus, eventbus.KindSites)))
	mux.HandleFunc("/api/workspaces/", withCORS(publishAfter(handleWorkspaceRoutes, eventbus.KindStatus, eventbus.KindSites)))
	mux.HandleFunc("/api/sites/", withCORS(publishAfter(handleSiteAction, eventbus.KindSites, eventbus.KindServices)))
	mux.HandleFunc("/api/logs/", withCORS(handleLogs))
	mux.HandleFunc("/api/queries/route-timing", withCORS(handleRouteTiming))
	mux.HandleFunc("/_svc/", handleDashProxy)
	mux.HandleFunc("/api/queue/", withCORS(handleUnitLogStream))
	mux.HandleFunc("/api/horizon/", withCORS(handleUnitLogStream))
	mux.HandleFunc("/api/stripe/", withCORS(handleUnitLogStream))
	mux.HandleFunc("/api/schedule/", withCORS(handleUnitLogStream))
	mux.HandleFunc("/api/reverb/", withCORS(handleUnitLogStream))
	mux.HandleFunc("/api/worker/", withCORS(handleUnitLogStream))
	mux.HandleFunc("/api/app-logs/", withCORS(handleAppLogs))
	mux.HandleFunc("/api/watcher/logs", withCORS(handleUnitLogStream))
	mux.HandleFunc("/api/watcher/start", withCORS(handleWatcherStart))
	mux.HandleFunc("/api/settings", withCORS(handleSettings))
	mux.HandleFunc("/api/settings/autostart", withCORS(handleSettingsAutostart))
	mux.HandleFunc("/api/settings/worker-mode", withCORS(handleSettingsWorkerMode))
	mux.HandleFunc("/api/settings/smtp", withCORS(handlePanelSMTP))
	mux.HandleFunc("/api/settings/smtp/test", withCORS(handlePanelSMTPTest))
	mux.HandleFunc("/api/workers/health", withCORS(handleWorkersHealth))
	mux.HandleFunc("/api/workers/heal", withCORS(handleWorkersHeal))
	mux.HandleFunc("/api/stats", withCORS(handleStats))
	mux.HandleFunc("/api/disk", withCORS(handleDisk))
	mux.HandleFunc("/api/servlo/start", withCORS(handleServloStart))
	mux.HandleFunc("/api/servlo/stop", withCORS(handleServloStop))
	mux.HandleFunc("/api/servlo/quit", withCORS(handleServloQuit))
	mux.HandleFunc("/api/remote-control", withCORS(handleRemoteControl))
	mux.HandleFunc("/api/access-mode", withCORS(handleAccessMode))
	mux.HandleFunc("/api/lan/status", withCORS(handleLANStatus))
	mux.HandleFunc("/manifest.webmanifest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json")
		base := "http://" + r.Host
		w.Write([]byte(`{"name":"Servlo","short_name":"Servlo","description":"Local Laravel development environment","start_url":"` + base + `/","display":"standalone","background_color":"#0d0d0d","theme_color":"#FF2D20","protocol_handlers":[{"protocol":"web+servlo","url":"` + base + `/?servlo=%s"}],"icons":[{"src":"` + base + `/icons/icon-192.png","sizes":"192x192","type":"image/png","purpose":"any"},{"src":"` + base + `/icons/icon-512.png","sizes":"512x512","type":"image/png","purpose":"any"},{"src":"` + base + `/icons/icon-maskable-192.png","sizes":"192x192","type":"image/png","purpose":"maskable"},{"src":"` + base + `/icons/icon-maskable-512.png","sizes":"512x512","type":"image/png","purpose":"maskable"},{"src":"` + base + `/icons/icon.svg","sizes":"any","type":"image/svg+xml","purpose":"any"}]}`)) //nolint:errcheck
	})
	mux.HandleFunc("/icons/icon.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write(iconSVG) //nolint:errcheck
	})
	mux.HandleFunc("/icons/icon-maskable.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write(iconMaskableSVG) //nolint:errcheck
	})
	mux.HandleFunc("/icons/icon-192.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(icon192PNG) //nolint:errcheck
	})
	mux.HandleFunc("/icons/icon-512.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(icon512PNG) //nolint:errcheck
	})
	mux.HandleFunc("/icons/icon-maskable-192.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(iconMaskable192PNG) //nolint:errcheck
	})
	mux.HandleFunc("/icons/icon-maskable-512.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(iconMaskable512PNG) //nolint:errcheck
	})
	swHash := sha256.Sum256(swJS)
	swVersion := version.Version + "-" + version.Commit + "-" + hex.EncodeToString(swHash[:6])
	swBody := bytes.ReplaceAll(swJS, []byte("{{SERVLO_VERSION}}"), []byte(swVersion))
	mux.HandleFunc("/sw.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Service-Worker-Allowed", "/")
		w.Write(swBody) //nolint:errcheck
	})
	mux.HandleFunc("/offline.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(offlineHTML) //nolint:errcheck
	})
	mux.Handle("/", serveSvelte())

	guard, err := authz.NewGuard()
	if err != nil {
		return fmt.Errorf("opening the panel's account store: %w", err)
	}
	// What an authenticator app lists this panel under. The domain when there
	// is one, so three servers in someone's app do not all read the same.
	guard.Issuer = PanelDomain()
	handler := withPanelAuth(guard, withRemoteControlGate(mux))

	// Unix socket listener for the servlo.localhost nginx vhost. Linux only:
	// on macOS, servlo-nginx runs inside the podman-machine VM and unix
	// sockets don't traverse virtio-fs as functional sockets, so the
	// vhost falls back to TCP via host.containers.internal there.
	// Errors are non-fatal — direct http://localhost:7073 access still
	// works even if the socket can't be created.
	if err := os.MkdirAll(config.RunDir(), 0755); err != nil {
		fmt.Printf("[WARN] creating %s: %v — servlo.localhost vhost will not work\n", config.RunDir(), err)
	} else {
		sockPath := config.UISocketPath()
		_ = os.Remove(sockPath)
		unixLn, err := net.Listen("unix", sockPath)
		if err != nil {
			fmt.Printf("[WARN] binding %s: %v — servlo.localhost vhost will not work\n", sockPath, err)
		} else {
			if err := os.Chmod(sockPath, 0660); err != nil {
				fmt.Printf("[WARN] chmod %s: %v\n", sockPath, err)
			}
			unixSrv := &http.Server{
				Handler: handler,
				ConnContext: func(ctx context.Context, _ net.Conn) context.Context {
					return context.WithValue(ctx, ctxKeyUnixSocket{}, true)
				},
			}
			go func() {
				fmt.Printf("Servlo UI listening on unix:%s\n", sockPath)
				if err := unixSrv.Serve(unixLn); err != nil && err != http.ErrServerClosed {
					fmt.Printf("[WARN] unix socket server exited: %v\n", err)
				}
			}()
		}
	}

	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", listenAddr, err)
	}
	fmt.Printf("Servlo panel listening on https://%s\n", listenAddr)
	if domain := PanelDomain(); domain != "" {
		fmt.Printf("Panel domain: https://%s\n", domain)
	} else {
		fmt.Println("No panel domain yet, so the certificate is self-signed and your browser will warn once.")
		fmt.Println("Attach one with: servlo panel domain set panel.example.com")
	}
	// Notify systemd we're ready only after the listener is accepting, so
	// Type=notify units make systemctl start block until the UI can serve.
	servloSystemd.NotifyReady()
	return servePanelTLS(ln, handler)
}

// allowedCORSOrigins are the origins servlo's own dashboard is served from.
// The :7073 entries are https only: the panel listener speaks TLS and answers
// a plain-HTTP request with a redirect, so an http origin on that port is
// either a stale bookmark or someone else's page.
var allowedCORSOrigins = map[string]bool{
	"http://servlo.localhost":  true,
	"https://servlo.localhost": true,
	"https://localhost:7073":   true,
	"https://127.0.0.1:7073":   true,
}

func withCORS(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowedCORSOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, "+csrfHeader)
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v) //nolint:errcheck
	return string(b)
}

// StatusResponse is the response for GET /api/status.
type StatusResponse struct {
	Nginx               ServiceCheck `json:"nginx"`
	PHPFPMs             []PHPStatus  `json:"php_fpms"`
	PHPDefault          string       `json:"php_default"`
	NodeDefault         string       `json:"node_default"`
	NodeManagedByServlo bool         `json:"node_managed_by_servlo"`
	// NodeManager is the active Node version manager servlo drives: "fnm" or "nvm".
	NodeManager string `json:"node_manager"`
	// NvmAvailable is true when a user-installed nvm is present (nvm.sh found),
	// so the dashboard can disable the nvm switch rather than error on click.
	NvmAvailable bool `json:"nvm_available"`
	// BunAvailable is true when a bun binary is installed on the host;
	// BunVersion carries its version for an at-a-glance reference.
	// UsingSystemBun is true when servlo isn't managing Node and there's no system
	// Node, so bun is what actually runs JS host workers (Vite, installs).
	BunAvailable   bool   `json:"bun_available"`
	BunVersion     string `json:"bun_version"`
	UsingSystemBun bool   `json:"using_system_bun"`
	WatcherRunning bool   `json:"watcher_running"`
	// FrankenPHPVersions are the PHP versions dunglas/frankenphp publishes an
	// image for, so the UI can limit a FrankenPHP site's version dropdown to
	// the ones it can actually run (intersected client-side with installed).
	FrankenPHPVersions []string `json:"frankenphp_php_versions"`
	// Home is the user's home directory, so the UI can shorten displayed paths
	// under it to a leading ~ without shipping the absolute path in the label.
	Home string `json:"home"`
	// Workspaces are the configured workspace names in display order, empty
	// ones included, so the sidebar can render a section the user just created.
	Workspaces []string `json:"workspaces"`
	// Instance identifies this servlo-panel process. An open dashboard reloads when
	// it changes, so a restarted server never leaves a stale page behind.
	Instance string `json:"instance"`
	// Tools reports the managed host binaries (composer, fnm) against
	// their pinned versions; fnm is omitted on nvm-managed setups where its
	// absence is deliberate.
	Tools []tools.ToolStatus `json:"tools"`
	// Production is whether this install is live. Shown prominently rather than
	// buried in a settings page: which mode a machine is in changes what a
	// visitor sees when something breaks, and someone about to debug a blank
	// page needs to know it before they start.
	Production bool `json:"production"`
	// ProductionSince is when it was turned on, empty when it is off.
	ProductionSince string `json:"production_since,omitempty"`
}

// productionSince renders when production mode was turned on, empty when off.
func productionSince(cfg *config.GlobalConfig) string {
	if since := cfg.ProductionSince(); !since.IsZero() {
		return since.Format(time.RFC3339)
	}
	return ""
}

// serverInstance identifies this servlo-panel process for the lifetime of the run.
var serverInstance = strconv.FormatInt(time.Now().UnixNano(), 36)

type ServiceCheck struct {
	Running bool `json:"running"`
}

type PHPStatus struct {
	Version string   `json:"version"`
	Patch   string   `json:"patch,omitempty"`
	Running bool     `json:"running"`
	Ports   []string `json:"ports,omitempty"`
	// UpdateAvailable is true when the prebuilt base this version's image was
	// built from has been republished since, so a rebuild picks up whatever
	// upstream shipped. Read from the digest cache, never from the network.
	UpdateAvailable bool `json:"update_available,omitempty"`
}

func handleStatus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(snapshots.Status())
}

func buildStatus() StatusResponse {
	cfg, _ := config.LoadGlobal()
	nginxRunning := podman.Cache.Running("servlo-nginx")
	watcherRunning := services.Mgr.IsActive("servlo-watcher")

	versions, _ := phpPkg.ListInstalled()
	var phpStatuses []PHPStatus
	for _, v := range versions {
		short := strings.ReplaceAll(v, ".", "")
		running := podman.Cache.Running("servlo-php" + short + "-fpm")
		var ports []string
		if cfg != nil {
			ports = cfg.PHP.FPMPorts[v]
		}
		baseStale := false
		if base := podman.BaseImageFreshness(v); base != nil {
			baseStale = base.Stale
		}
		phpStatuses = append(phpStatuses, PHPStatus{Version: v, Patch: podman.FPMPHPVersion(v), Running: running, Ports: ports, UpdateAvailable: baseStale})
	}

	phpDefault := ""
	nodeDefault := ""
	nodeManager := "fnm"
	if cfg != nil {
		phpDefault = cfg.PHP.DefaultVersion
		nodeDefault = cfg.Node.DefaultVersion
		nodeManager = cfg.NodeManager()
	}
	nodeManagedByServlo := servloNode.Managed()
	bunAvailable := servloNode.BunPath() != ""
	bunVersion := ""
	if bunAvailable {
		bunVersion = servloNode.BunVersion()
	}
	usingSystemBun := bunAvailable && !nodeManagedByServlo && !servloNode.SystemNodeAvailable()
	toolStatuses := []tools.ToolStatus{}
	for _, s := range tools.StatusAll(context.Background()) {
		if s.Name == "fnm" && nodeManager == "nvm" {
			continue
		}
		toolStatuses = append(toolStatuses, s)
	}
	homeDir, _ := os.UserHomeDir()
	workspaces := cfg.WorkspaceNames()
	if workspaces == nil {
		workspaces = []string{}
	}
	return StatusResponse{
		Nginx:               ServiceCheck{Running: nginxRunning},
		PHPFPMs:             phpStatuses,
		PHPDefault:          phpDefault,
		NodeDefault:         nodeDefault,
		NodeManagedByServlo: nodeManagedByServlo,
		NodeManager:         nodeManager,
		NvmAvailable:        servloNode.ManagerByName("nvm").Available(),
		BunAvailable:        bunAvailable,
		BunVersion:          bunVersion,
		UsingSystemBun:      usingSystemBun,
		WatcherRunning:      watcherRunning,
		FrankenPHPVersions:  config.FrankenPHPVersions(),
		Home:                homeDir,
		Workspaces:          workspaces,
		Instance:            serverInstance,
		Production:          cfg.ProductionMode(),
		ProductionSince:     productionSince(cfg),
		Tools:               toolStatuses,
	}
}

func buildStatusJSON() ([]byte, error) { return []byte(mustJSON(buildStatus())), nil }

// WorkerStatus represents a single framework worker and its running state.
type WorkerStatus struct {
	Name    string `json:"name"`
	Label   string `json:"label"`
	Running bool   `json:"running"`
	Failing bool   `json:"failing,omitempty"`
	// Unreachable: the unit is active but its server isn't accepting connections.
	// Distinct from Failing (systemd failed) so the row shows its own state.
	Unreachable bool `json:"unreachable,omitempty"`
}

// ConflictingDomain describes a domain declared in .servlo.yaml that wasn't
// registered for the site because another site on this machine already owns
// it. Surfaced to the UI so the domain modal can render a warning icon next
// to the entry with the owning site name.
type ConflictingDomain struct {
	Domain  string `json:"domain"`
	OwnedBy string `json:"owned_by"`
}

// SiteResponse is the response for GET /api/sites.
type SiteResponse struct {
	Name               string              `json:"name"`
	AppName            string              `json:"app_name,omitempty"`
	Domain             string              `json:"domain"`
	Domains            []string            `json:"domains"`
	ConflictingDomains []ConflictingDomain `json:"conflicting_domains,omitempty"`
	Path               string              `json:"path"`
	PHPVersion         string              `json:"php_version"`
	// PHPMin/PHPMax are the framework's supported PHP range. The dashboard
	// disables out-of-range versions in the picker. Empty when there is no
	// framework or its version was guessed (clamped), so nothing is disabled.
	PHPMin             string         `json:"php_min,omitempty"`
	PHPMax             string         `json:"php_max,omitempty"`
	UsesPHP            bool           `json:"uses_php"`
	NodeVersion        string         `json:"node_version"`
	JSRuntime          string         `json:"js_runtime,omitempty"`
	TLS                bool           `json:"tls"`
	Framework          string         `json:"framework"`
	FPMRunning         bool           `json:"fpm_running"`
	IsLaravel          bool           `json:"is_laravel"`
	FrameworkLabel     string         `json:"framework_label"`
	QueueRunning       bool           `json:"queue_running"`
	QueueFailing       bool           `json:"queue_failing,omitempty"`
	StripeRunning      bool           `json:"stripe_running"`
	StripeSecretSet    bool           `json:"stripe_secret_set"`
	StripeWebhookPath  string         `json:"stripe_webhook_path,omitempty"`
	ScheduleRunning    bool           `json:"schedule_running"`
	ScheduleFailing    bool           `json:"schedule_failing,omitempty"`
	ReverbRunning      bool           `json:"reverb_running"`
	ReverbFailing      bool           `json:"reverb_failing,omitempty"`
	HasReverb          bool           `json:"has_reverb"`
	HasHorizon         bool           `json:"has_horizon"`
	HorizonRunning     bool           `json:"horizon_running"`
	HorizonFailing     bool           `json:"horizon_failing,omitempty"`
	HorizonReload      bool           `json:"horizon_reload,omitempty"`       // horizon runs via horizon:listen (auto-reload)
	HorizonReloadReady bool           `json:"horizon_reload_ready,omitempty"` // chokidar present, so auto-reload can be enabled without installing it
	OctaneReload       bool           `json:"octane_reload,omitempty"`        // FrankenPHP worker serves via octane:start --watch (auto-reload)
	OctaneReloadReady  bool           `json:"octane_reload_ready,omitempty"`  // FrankenPHP worker mode + chokidar present, so auto-reload can be enabled
	HasQueueWorker     bool           `json:"has_queue_worker"`
	HasScheduleWorker  bool           `json:"has_schedule_worker"`
	FrameworkWorkers   []WorkerStatus `json:"framework_workers,omitempty"`
	HasAppLogs         bool           `json:"has_app_logs"`
	HasFavicon         bool           `json:"has_favicon"`
	HasEnv             bool           `json:"has_env"`
	Paused             bool           `json:"paused"`
	// Pinned keeps a site at the top of the list.
	Pinned bool `json:"pinned,omitempty"`
	// LastRequestAt (unix milliseconds) and RequestCount are the site's traffic
	// over the request store's retention window, filtered to the requests the
	// app actually served. The sites list orders by them.
	LastRequestAt int64  `json:"last_request_at,omitempty"`
	RequestCount  int    `json:"request_count,omitempty"`
	Branch        string `json:"branch"`
	// Services lists the service names this site uses, sourced from the
	// project's .servlo.yaml. Used by the dashboard to render service badges
	// on the site detail panel.
	Services []string `json:"services,omitempty"`
	// DBDatabase is the site's DB_DATABASE, so the overview's database card can
	// open the admin tool straight to this site's database.
	DBDatabase       string `json:"db_database,omitempty"`
	LANPort          int    `json:"lan_port,omitempty"`
	CustomContainer  bool   `json:"custom_container,omitempty"`
	ContainerPort    int    `json:"container_port,omitempty"`
	ContainerImage   string `json:"container_image,omitempty"`
	Runtime          string `json:"runtime,omitempty"`
	RuntimeWorker    bool   `json:"runtime_worker,omitempty"`
	HostProxy        bool   `json:"host_proxy,omitempty"`
	HostPort         int    `json:"host_port,omitempty"`
	HostHasDevServer bool   `json:"host_has_dev_server,omitempty"`
	// DoctorApplicable is false when no doctor check can apply (a host-proxy
	// Python/Ruby/Go site with no framework and no composer/package manifest), so
	// the dashboard hides the button rather than opening an empty modal. Not
	// omitempty: the dashboard keys on the explicit false to hide.
	DoctorApplicable bool `json:"doctor_applicable"`
	// Grouping — Group is the group key (main site's name); GroupSubdomain is the
	// label a secondary occupies; GroupMainDomain is the group main's base domain.
	// MultiTenant flags a main whose project declares env_overrides (wildcard
	// tenant subdomains) so the UI can warn that a secondary carves out a label.
	Group           string `json:"group,omitempty"`
	GroupSubdomain  string `json:"group_subdomain,omitempty"`
	GroupMainDomain string `json:"group_main_domain,omitempty"`
	GroupSharedDB   bool   `json:"group_shared_db,omitempty"`
	MultiTenant     bool   `json:"multi_tenant,omitempty"`
	// Workspace is the display-only grouping the site belongs to, if any. A
	// group secondary reports its main's workspace so a group renders whole.
	Workspace string `json:"workspace,omitempty"`
}

func handleSites(w http.ResponseWriter, _ *http.Request) {
	// A nil snapshot means the registry could not be read and there is nothing
	// cached to fall back on. Saying so beats writing a zero-byte body the
	// dashboard would render as "you have no sites".
	body := snapshots.Sites()
	if body == nil {
		http.Error(w, "sites are temporarily unavailable, the registry could not be read", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

func buildSitesJSON() ([]byte, error) {
	sites, err := buildSites()
	if err != nil {
		return nil, err
	}
	return []byte(mustJSON(sites)), nil
}

func buildSites() ([]SiteResponse, error) {
	enriched, err := siteinfo.LoadAll(siteinfo.EnrichUI)
	if err != nil {
		return nil, fmt.Errorf("loading sites: %w", err)
	}
	_ = siteinfo.PersistVersionChanges(enriched)

	// Traffic per site key, read once per snapshot, so the sites list can order by
	// what has actually been used rather than by log-file mtime.
	siteUsage := loadSiteUsage()

	// Per-site list of workers the engine suspended, so the dashboard can keep
	// showing their dots dimmed instead of dropping them.
	pinnedSites := map[string]bool{}
	if reg, err := config.LoadSites(); err == nil {
		for _, s := range reg.Sites {
			if s.Pinned {
				pinnedSites[s.Name] = true
			}
		}
	}

	// Map each group key to its main site's base domain so secondaries can
	// report group_main_domain without a second lookup.
	groupMainDomain := map[string]string{}
	groupMainName := map[string]string{}
	for _, e := range enriched {
		if e.Group != "" && e.GroupSubdomain == "" {
			groupMainDomain[e.Group] = e.PrimaryDomain()
			groupMainName[e.Group] = e.Name
		}
	}

	// Workspace membership is display-only and lives in the global config.
	cfg, _ := config.LoadGlobal()
	siteWorkspace := cfg.SiteWorkspaceMap()

	sites := make([]SiteResponse, 0, len(enriched))
	for _, e := range enriched {
		var fwWorkers []WorkerStatus
		for _, fw := range e.FrameworkWorkers {
			fwWorkers = append(fwWorkers, WorkerStatus{
				Name:        fw.Name,
				Label:       fw.Label,
				Running:     fw.Running,
				Failing:     fw.Failing,
				Unreachable: fw.Unreachable,
			})
		}

		var conflicting []ConflictingDomain
		for _, cd := range e.ConflictingDomains {
			conflicting = append(conflicting, ConflictingDomain{
				Domain:  cd.Domain,
				OwnedBy: cd.OwnedBy,
			})
		}

		usage := siteUsage[e.Name]

		sites = append(sites, SiteResponse{
			Name:               e.Name,
			AppName:            laravelAppName(e.FrameworkName, e.Path),
			Domain:             e.PrimaryDomain(),
			Domains:            e.Domains,
			ConflictingDomains: conflicting,
			Path:               e.Path,
			PHPVersion:         e.PHPVersion,
			PHPMin:             e.FrameworkPHPMin,
			PHPMax:             e.FrameworkPHPMax,
			UsesPHP:            e.UsesPHP,
			NodeVersion:        e.NodeVersion,
			JSRuntime:          projectJSRuntime(e.Path),
			TLS:                e.Secured,
			Framework:          e.FrameworkName,
			IsLaravel:          e.FrameworkName == "laravel",
			FrameworkLabel:     e.FrameworkLabel,
			FPMRunning:         e.FPMRunning,
			QueueRunning:       e.QueueRunning,
			QueueFailing:       e.QueueFailing,
			StripeRunning:      e.StripeRunning,
			StripeSecretSet:    e.StripeSecretSet,
			StripeWebhookPath:  e.StripeWebhookPath,
			ScheduleRunning:    e.ScheduleRunning,
			ScheduleFailing:    e.ScheduleFailing,
			ReverbRunning:      e.ReverbRunning,
			ReverbFailing:      e.ReverbFailing,
			HasReverb:          e.HasReverb,
			HasHorizon:         e.HasHorizon,
			HorizonRunning:     e.HorizonRunning,
			HorizonFailing:     e.HorizonFailing,
			HorizonReload:      e.HasHorizon && config.ProjectReloadsWorker(e.Path, "horizon"),
			HorizonReloadReady: e.HasHorizon && cli.ProjectHasChokidar(e.Path),
			OctaneReload:       e.Runtime == "frankenphp" && e.RuntimeWorker && config.ProjectReloadsWorker(e.Path, "octane"),
			OctaneReloadReady:  e.Runtime == "frankenphp" && e.RuntimeWorker && cli.SiteHasOctane(e.Path) && cli.ProjectHasChokidar(e.Path),
			HasQueueWorker:     e.HasQueueWorker,
			HasScheduleWorker:  e.HasScheduleWorker,
			FrameworkWorkers:   fwWorkers,
			HasAppLogs:         e.HasAppLogs,
			HasFavicon:         e.HasFavicon,
			HasEnv:             siteHasEnv(e.FrameworkName, e.Path),
			Paused:             e.Paused,
			LastRequestAt:      unixMilliOrZero(usage.LastAt),
			RequestCount:       usage.Count,
			Pinned:             pinnedSites[e.Name],
			Branch:             e.Branch,
			Services:           e.Services,
			DBDatabase:         envfile.ReadKey(filepath.Join(e.Path, ".env"), "DB_DATABASE"),
			LANPort:            e.LANPort,
			CustomContainer:    e.ContainerPort > 0,
			ContainerPort:      e.ContainerPort,
			ContainerImage:     e.ContainerImage,
			Runtime:            e.Runtime,
			RuntimeWorker:      e.RuntimeWorker,
			HostProxy:          e.HostPort > 0,
			HostPort:           e.HostPort,
			HostHasDevServer:   e.HostPort > 0 && e.HostCommand != "",
			DoctorApplicable:   sitedoctor.AppliesForPath(e.Path, e.FrameworkName),
			Group:              e.Group,
			GroupSubdomain:     e.GroupSubdomain,
			GroupMainDomain:    groupMainDomain[e.Group],
			GroupSharedDB:      e.GroupSharedDB,
			MultiTenant:        e.Group != "" && e.GroupSubdomain == "" && siteHasEnvOverrides(e.Path),
			Workspace:          resolveSiteWorkspace(e, groupMainName, siteWorkspace),
		})
	}
	return sites, nil
}

// ServicePortMapping describes one published port of a service: its
// container-internal port, its preset-default host port, and the current host
// override (0 = on the default). The ports modal renders and edits these.
type ServicePortMapping struct {
	Container int `json:"container"`
	Default   int `json:"default"`
	Published int `json:"published,omitempty"`
}

// secondaryPortMappings returns a service's published mappings past the primary
// (index 0), pairing each with its current host override from the config.
func secondaryPortMappings(defaultPorts []string, sc config.ServiceConfig) []ServicePortMapping {
	var out []ServicePortMapping
	for i, spec := range defaultPorts {
		if i == 0 {
			continue
		}
		c := podman.ContainerPort(spec)
		if c == 0 {
			continue
		}
		out = append(out, ServicePortMapping{
			Container: c,
			Default:   podman.PrimaryHostPort([]string{spec}),
			Published: sc.PublishedPorts[c],
		})
	}
	return out
}

// ServiceResponse is the response for GET /api/services.
type ServiceResponse struct {
	Name              string            `json:"name"`
	Status            string            `json:"status"`
	Version           string            `json:"version,omitempty"`
	EnvVars           map[string]string `json:"env_vars"`
	Dashboard         string            `json:"dashboard,omitempty"`
	DashboardExternal bool              `json:"dashboard_external,omitempty"`
	ConnectionURL     string            `json:"connection_url,omitempty"`
	Category          string            `json:"category,omitempty"`
	Icon              string            `json:"icon,omitempty"`
	AdminFor          []string          `json:"admin_for,omitempty"`
	// Preset this service was installed from ("mariadb" for "mariadb-11-8"), so
	// the UI can match it against another preset's admin_for without guessing.
	Preset string `json:"preset,omitempty"`
	// Port is the host (published) port the service is exposed on: the
	// published-port override when set, else the preset/version default. 0 when
	// the service publishes no host port (e.g. a worker). The UI shows it in the
	// status pill so a moved port reads at a glance.
	Port int `json:"port,omitempty"`
	// PublishedPort is the user's published-port override (0 when unset, i.e.
	// using the default). DefaultPort is the preset/version default host port.
	// ExtraPorts are the extra published mappings. The ports modal reads these
	// to show the current state and a reset-to-default affordance.
	PublishedPort int      `json:"published_port,omitempty"`
	DefaultPort   int      `json:"default_port,omitempty"`
	ExtraPorts    []string `json:"extra_ports,omitempty"`
	// SecondaryPorts are the service's published mappings past the primary (a
	// multi-port service like rustfs), each with its container-internal
	// port, preset-default host port, and current override. The ports modal renders
	// one editable host-port field per entry so every published port is movable.
	SecondaryPorts []ServicePortMapping `json:"secondary_ports,omitempty"`
	Custom         bool                 `json:"custom,omitempty"`
	IsDefault      bool                 `json:"is_default,omitempty"`
	// PresetOwned is true when servlo ships this service as a bundled preset
	// (default-stack or optional like gotenberg). The ports modal keys the
	// extra-ports affordance off this, not is_default, so every service we
	// provide can publish extra ports while genuinely custom services can't.
	PresetOwned bool `json:"preset_owned,omitempty"`
	// Tunable is true when the service exposes a user-editable runtime config
	// override (see config.ServiceTuningMount), so the UI can show a Tuning tab.
	Tunable bool `json:"tunable,omitempty"`
	// IsDatabase is true for a database engine, whether it is a wired family
	// (mysql/mariadb/postgres/mongo) or a store engine that declares databases
	// of its own, so the detail view can show its Databases tab. Excludes sqlite
	// and admin UIs, which are not queryable engines.
	IsDatabase bool `json:"is_database,omitempty"`
	// EntityKinds are the non-database entity kinds this service declares
	// (buckets, keyspaces…), so the detail view can show a generic overview tab.
	EntityKinds        []string `json:"entity_kinds,omitempty"`
	SiteCount          int      `json:"site_count"`
	SiteDomains        []string `json:"site_domains,omitempty"`
	Pinned             bool     `json:"pinned"`
	Paused             bool     `json:"paused,omitempty"`
	DependsOn          []string `json:"depends_on,omitempty"`
	QueueSite          string   `json:"queue_site,omitempty"`
	StripeListenerSite string   `json:"stripe_listener_site,omitempty"`
	ScheduleWorkerSite string   `json:"schedule_worker_site,omitempty"`
	ReverbSite         string   `json:"reverb_site,omitempty"`
	HorizonSite        string   `json:"horizon_site,omitempty"`
	WorkerSite         string   `json:"worker_site,omitempty"`
	WorkerName         string   `json:"worker_name,omitempty"`
	WorkerLabel        string   `json:"worker_label,omitempty"`
	UpdateStrategy     string   `json:"update_strategy,omitempty"`
	UpdateAvailable    bool     `json:"update_available,omitempty"`
	LatestVersion      string   `json:"latest_version,omitempty"`
	UpgradeVersion     string   `json:"upgrade_version,omitempty"`
	PreviousVersion    string   `json:"previous_version,omitempty"`
	// MigrationSupported and CanRollback intentionally drop omitempty so the
	// false case still appears in the JSON. The UI uses === to distinguish
	// "field missing" (no avail check ran) from "explicitly false".
	MigrationSupported bool           `json:"migration_supported"`
	CanRollback        bool           `json:"can_rollback"`
	PortConflicts      []PortConflict `json:"port_conflicts,omitempty"`
	// ClientShims are the client tools this service exposes as host shims
	// (mysqldump, pg_dump…), each with whether the host already has the tool and
	// the user's current decision. The service card renders a toggle per tool.
	ClientShims []shims.Info `json:"client_shims,omitempty"`
}

// PortConflict reports a host port servlo wants to bind that is already taken
// by another process. Surfaced for inactive services so the user sees the
// blocker before clicking Start.
type PortConflict struct {
	Port  string `json:"port"`
	Label string `json:"label,omitempty"`
}

// portConflictsFor returns conflicts for a single unit using a pre-fetched
// listening-port listing. ssOutput is shared across the whole snapshot
// rebuild so we never spawn ss/lsof more than once per refresh.
func portConflictsFor(unit, ssOutput string) []PortConflict {
	if ssOutput == "" {
		return nil
	}
	checks := cli.CollectPortChecks([]string{unit})
	if len(checks) == 0 {
		return nil
	}
	var out []PortConflict
	for _, c := range checks {
		if cli.PortInUseIn(c.Port, ssOutput) {
			out = append(out, PortConflict{Port: c.Port, Label: c.Label})
		}
	}
	return out
}

// effectiveHostPort returns the host port a service is exposed on, mirroring the
// precedence CollectPortChecks uses so the pill, the boot-time check and the
// connection URL all agree: the published-port override, else the configured
// port (which a non-canonical preset version seeds to its own host port, e.g.
// postgres 18 → 5418), else the primary host port from the default mappings.
// 0 when none applies (no host port published, e.g. a worker). services is the
// caller's already-loaded cfg.Services map so a full services-list rebuild loads
// (and deep-clones) the global config once rather than once per service.
func effectiveHostPort(services map[string]config.ServiceConfig, name string, defaultPorts []string) int {
	if sc, ok := services[name]; ok {
		if sc.PublishedPort > 0 {
			return sc.PublishedPort
		}
		if sc.Port > 0 {
			return sc.Port
		}
	}
	return podman.PrimaryHostPort(defaultPorts)
}

// loadServicesMap returns cfg.Services, or nil when the global config can't be
// loaded — the single load behind a one-off buildServiceResponse.
func loadServicesMap() map[string]config.ServiceConfig {
	if cfg, err := config.LoadGlobal(); err == nil && cfg != nil {
		return cfg.Services
	}
	return nil
}

func buildServiceResponse(name string) ServiceResponse {
	return buildServiceResponseWithPortList(loadServicesMap(), name, "", nil)
}

// buildServiceResponseWithPortList is the single builder for a service's UI
// response, covering both built-in default presets and custom/add-on services.
// The definition source differs (bundled preset helpers vs the stored YAML), so
// it is resolved once up front; every response field is then set in this one
// place. Do not add a second builder for a service kind — a field set here must
// never be silently missing for the other kind (that is exactly how client_shims
// went missing for add-ons).
// custom is the already-loaded definition for an add-on service (passed by the
// presetNameOf returns the preset a service was installed from, falling back to
// the service name for default-stack services that are their own preset.
func presetNameOf(name string, custom *config.CustomService) string {
	if custom != nil && custom.Preset != "" {
		return custom.Preset
	}
	if config.PresetExists(name) {
		return name
	}
	return ""
}

// servicePresentation resolves a service's discovery metadata from its preset,
// so a service installed before these fields existed still renders correctly.
// A genuinely user-defined service falls back to its own stored YAML.
func servicePresentation(name string, custom *config.CustomService) (category, icon string, adminFor []string) {
	presetName := name
	if custom != nil && custom.Preset != "" {
		presetName = custom.Preset
	}
	if p, err := config.LoadPreset(presetName); err == nil {
		return p.Category, p.Icon, p.AdminFor
	}
	if custom != nil {
		return custom.Category, custom.Icon, custom.AdminFor
	}
	return "", "", nil
}

// list rebuild so it is not re-read and an error cannot blank the card); pass
// nil for a default preset or to have it loaded by name.
func buildServiceResponseWithPortList(services map[string]config.ServiceConfig, name, ssOutput string, custom *config.CustomService) ServiceResponse {
	unit := "servlo-" + name
	status, _ := podman.UnitStatus(unit)
	if status == "" {
		status = "inactive"
	}

	// Resolve the definition source once: a default preset reads from the
	// bundled preset helpers, a custom/add-on service from its stored YAML.
	isDefault := config.IsDefaultPreset(name)
	if !isDefault && custom == nil {
		custom, _ = config.LoadCustomService(name)
	}

	var (
		envKVs       []string
		presetPorts  []string
		dashboardRaw string
		dashExternal bool
		connURL      string
		dependsOn    []string
	)
	switch {
	case isDefault:
		envKVs = config.DefaultPresetEnvVars(name)
		presetPorts = config.PresetPorts(name)
		dashboardRaw = config.DefaultPresetDashboard(name)
		connURL = config.DefaultPresetConnectionURL(name)
	case custom != nil:
		envKVs = custom.EnvVars
		presetPorts = custom.Ports
		dashboardRaw = custom.Dashboard
		dashExternal = custom.DashboardExternal
		connURL = custom.ConnectionURL
		dependsOn = custom.DependsOn
		// Bundled dashboards that asked to open externally (cross-origin cookie
		// trouble) are proxied same-origin under /_svc/<name>/ so they embed in
		// the iframe overlay; user custom services keep the new tab.
		if dashProxyEligible(custom) {
			dashboardRaw, dashExternal = dashProxyPath(name), false
		}
	}

	handle := "servlo-" + name
	envMap := map[string]string{}
	for _, kv := range envKVs {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			v := strings.ReplaceAll(parts[1], "{{site}}", handle)
			v = strings.ReplaceAll(v, "{{site_testing}}", handle+"_testing")
			envMap[parts[0]] = v
		}
	}

	hostPort := effectiveHostPort(services, name, presetPorts)
	defaultPort := podman.PrimaryHostPort(presetPorts)
	if sc, ok := services[name]; ok && sc.Port > 0 {
		defaultPort = sc.Port
	}
	// Prefer the installed quadlet's image; fall back to the stored definition so
	// a custom service whose quadlet is momentarily absent keeps its version.
	image := podman.InstalledImage(unit)
	if image == "" && custom != nil {
		image = custom.Image
	}
	category, icon, adminFor := servicePresentation(name, custom)
	resp := ServiceResponse{
		Name:              name,
		Status:            status,
		Version:           podman.ServiceVersionLabel(image),
		EnvVars:           envMap,
		Dashboard:         serviceops.WithDashboardPort(dashboardRaw, presetPorts, services[name]),
		DashboardExternal: dashExternal,
		ConnectionURL:     serviceops.WithURLPort(connURL, hostPort),
		Category:          category,
		Icon:              icon,
		AdminFor:          adminFor,
		Preset:            presetNameOf(name, custom),
		Port:              hostPort,
		SiteCount:         countSitesUsingService(name),
		SiteDomains:       sitesUsingService(name),
		Pinned:            config.ServiceIsPinned(name),
		Paused:            config.ServiceIsPaused(name),
		IsDefault:         isDefault,
		IsDatabase:        name != "sqlite" && isDatabaseEngine(name),
		EntityKinds:       serviceops.EntityKinds(name),
		Custom:            custom != nil,
		PresetOwned:       config.PresetExists(name),
		DefaultPort:       defaultPort,
		DependsOn:         serviceops.DependencyDisplayNames(dependsOn),
	}
	resp.SecondaryPorts = secondaryPortMappings(presetPorts, services[name])
	if sc, ok := services[name]; ok {
		resp.PublishedPort = sc.PublishedPort
		resp.ExtraPorts = sc.ExtraPorts
	}
	// Only advertise Tunable when the service is actually installed.
	// ResolveServiceForTuning resolves built-in default presets even when
	// the user has explicitly `servlo service remove`d them, so without the
	// ServiceInstalled gate the UI would render a Tuning tab on a removed
	// service — and clicking through would silently reinstall via the
	// materialise + quadlet regen + restart path (closed at the handler
	// level by the same guard, but the tab shouldn't appear in the first
	// place).
	if serviceops.ServiceInstalled(name) {
		if svc, err := config.ResolveServiceForTuning(name); err == nil {
			if _, ok := config.ServiceTuningMount(svc); ok {
				resp.Tunable = true
			}
		}
		resp.ClientShims = shims.ServiceShims(name)
	}
	// Default-preset services advertise update availability so the dashboard
	// can show an "→ v8.4.3" badge. Stopped services also run the check so the
	// user can pull a newer image without first starting the unit; the registry
	// layer's 6h disk cache absorbs repeated lookups.
	if avail, err := serviceops.CheckUpdateAvailable(name); err == nil && avail != nil {
		resp.UpdateStrategy = avail.Strategy
		resp.UpdateAvailable = avail.Available
		resp.LatestVersion = avail.LatestTag
		resp.UpgradeVersion = avail.UpgradeTag
		resp.PreviousVersion = avail.PreviousImage
		resp.MigrationSupported = serviceops.SupportsMigration(name)
		resp.CanRollback = avail.CanRollback
	}
	if status != "active" {
		resp.PortConflicts = portConflictsFor(unit, ssOutput)
	}
	return resp
}

// listActiveQueueWorkers returns the site names of active servlo-queue-* systemd units.
func listActiveQueueWorkers() []string {
	return listActiveUnitsBySuffix("servlo-queue-*.service", "servlo-queue-")
}

// listActiveScheduleWorkers returns site names of active servlo-schedule-* units.
// Includes timer-driven schedulers whose .service is static between firings.
func listActiveScheduleWorkers() []string {
	svc := listActiveUnitsBySuffix("servlo-schedule-*.service", "servlo-schedule-")
	timer := listActiveUnitsBySuffix("servlo-schedule-*.timer", "servlo-schedule-")
	seen := map[string]bool{}
	out := make([]string, 0, len(svc)+len(timer))
	for _, n := range append(svc, timer...) {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// listActiveReverbServers returns site names of active servlo-reverb-* units.
func listActiveReverbServers() []string {
	return listActiveUnitsBySuffix("servlo-reverb-*.service", "servlo-reverb-")
}

// listActiveHorizonWorkers returns site names of active servlo-horizon-* units.
func listActiveHorizonWorkers() []string {
	return listActiveUnitsBySuffix("servlo-horizon-*.service", "servlo-horizon-")
}

func handleServices(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(snapshots.Services())
}

func buildServicesJSON() ([]byte, error) { return []byte(mustJSON(buildServicesList())), nil }

func buildServicesList() []ServiceResponse {
	// One ss/lsof call shared across all installed-but-stopped services in
	// this rebuild; portConflictsFor is a no-op when ssOutput is empty.
	ssOutput := cli.PortListOutput()

	// Load the global config once for the whole rebuild; effectiveHostPort reads
	// the shared Services map instead of re-loading (and deep-cloning) it per service.
	svcCfg := loadServicesMap()

	defaultNames := siteinfo.KnownServices()
	services := make([]ServiceResponse, 0, len(defaultNames))
	for _, name := range defaultNames {
		// A removed default preset (no unit) is not installed, so it belongs in
		// the preset picker as installable, not lingering here as "inactive".
		// ServiceInstalled is the #678 single source of truth.
		if !serviceops.ServiceInstalled(name) {
			continue
		}
		services = append(services, buildServiceResponseWithPortList(svcCfg, name, ssOutput, nil))
	}
	// Optional presets (gotenberg, mongo, …) and user services materialise as
	// custom services; they go through the SAME builder as the default presets
	// so every field is populated identically for both kinds.
	customs, _ := config.ListCustomServices()
	for _, svc := range customs {
		services = append(services, buildServiceResponseWithPortList(svcCfg, svc.Name, ssOutput, svc))
	}
	for _, siteName := range listActiveQueueWorkers() {
		services = append(services, ServiceResponse{
			Name:      "queue-" + siteName,
			Status:    "active",
			EnvVars:   map[string]string{},
			QueueSite: siteName,
		})
	}
	for _, siteName := range listActiveStripeListeners() {
		services = append(services, ServiceResponse{
			Name:               "stripe-" + siteName,
			Status:             "active",
			EnvVars:            map[string]string{},
			StripeListenerSite: siteName,
		})
	}
	for _, siteName := range listActiveScheduleWorkers() {
		services = append(services, ServiceResponse{
			Name:               "schedule-" + siteName,
			Status:             "active",
			EnvVars:            map[string]string{},
			ScheduleWorkerSite: siteName,
		})
	}
	for _, siteName := range listActiveReverbServers() {
		services = append(services, ServiceResponse{
			Name:       "reverb-" + siteName,
			Status:     "active",
			EnvVars:    map[string]string{},
			ReverbSite: siteName,
		})
	}
	for _, siteName := range listActiveHorizonWorkers() {
		services = append(services, ServiceResponse{
			Name:        "horizon-" + siteName,
			Status:      "active",
			EnvVars:     map[string]string{},
			HorizonSite: siteName,
		})
	}
	// GetFrameworkForDir reads the versioned store YAML; plain GetFramework
	// returns the built-in skeleton and misses store-defined workers like vite.
	if reg2, err2 := config.LoadSites(); err2 == nil {
		for _, s := range reg2.Sites {
			if s.Ignored {
				continue
			}
			fwN := s.Framework
			fw2, ok2 := config.GetFrameworkForDir(fwN, s.Path)
			if !ok2 || fw2.Workers == nil {
				continue
			}
			services = append(services, frameworkWorkerServicesForSite(s, fw2, frameworkUnitStatus)...)
		}
	}
	return services
}

// frameworkUnitStatus is the production status lookup; tests swap this with a
// fake to drive the framework worker enumeration without systemd.
var frameworkUnitStatus = podman.UnitStatus

// frameworkWorkerServicesForSite enumerates active framework workers for one
// site, excluding queue/schedule/reverb which surface through their own loops.
// statusFn is injected for tests.
func frameworkWorkerServicesForSite(
	s config.Site,
	fw *config.Framework,
	statusFn func(string) (string, error),
) []ServiceResponse {
	if fw == nil || fw.Workers == nil {
		return nil
	}
	var out []ServiceResponse
	for wname, w := range fw.Workers {
		switch wname {
		case "queue", "schedule", "reverb":
			continue
		}
		label := w.Label
		if label == "" {
			label = wname
		}
		unitName := "servlo-" + wname + "-" + s.Name
		unitStatus, _ := statusFn(unitName)
		if unitStatus != "active" {
			continue
		}
		out = append(out, ServiceResponse{
			Name:        wname + "-" + s.Name,
			Status:      "active",
			EnvVars:     map[string]string{},
			WorkerSite:  s.Name,
			WorkerName:  wname,
			WorkerLabel: label,
		})
	}
	return out
}

// PresetResponse describes a bundled service preset for the web UI.
type PresetResponse struct {
	Name           string                 `json:"name"`
	Description    string                 `json:"description"`
	Image          string                 `json:"image,omitempty"`
	Dashboard      string                 `json:"dashboard,omitempty"`
	DependsOn      []string               `json:"depends_on,omitempty"`
	MissingDeps    []string               `json:"missing_deps,omitempty"`
	Installed      bool                   `json:"installed"`
	Versions       []config.PresetVersion `json:"versions,omitempty"`
	DefaultVersion string                 `json:"default_version,omitempty"`
	InstalledTags  []string               `json:"installed_tags,omitempty"`
	Category       string                 `json:"category,omitempty"`
	Icon           string                 `json:"icon,omitempty"`
	AdminFor       []string               `json:"admin_for,omitempty"`
}

// handleServicePresets returns the list of bundled presets and whether each is
// already installed as a custom service.
func handleServicePresets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	presets, err := cli.ListInstallablePresets()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]PresetResponse, 0, len(presets))
	for _, p := range presets {
		var missing []string
		var resolvedSvc *config.CustomService
		if loaded, err := config.LoadPreset(p.Name); err == nil {
			if svc, rerr := loaded.Resolve(""); rerr == nil {
				resolvedSvc = svc
				missing = cli.MissingPresetDependencies(svc)
			}
		}
		// For single-version presets installed reflects "is the canonical
		// service installed". For multi-version presets it reflects "are any
		// instances installed" (canonical at the bare preset name OR alternates
		// at the suffixed name), and InstalledTags lists them.
		installed := false
		var installedTags []string
		if len(p.Versions) == 0 {
			if serviceops.ServiceInstalled(p.Name) {
				installed = true
			}
		} else {
			for _, v := range p.Versions {
				if serviceops.ServiceInstalled(config.PresetVersionServiceName(p.Name, v)) {
					installed = true
					installedTags = append(installedTags, v.Tag)
				}
			}
		}
		image := p.Image
		if resolvedSvc != nil && len(p.Versions) == 0 {
			image = resolvedSvc.Image
		}
		out = append(out, PresetResponse{
			Name:           p.Name,
			Description:    p.Description,
			Image:          image,
			Dashboard:      p.Dashboard,
			DependsOn:      p.DependsOn,
			MissingDeps:    missing,
			Installed:      installed,
			Versions:       p.Versions,
			DefaultVersion: p.DefaultVersion,
			InstalledTags:  installedTags,
			Category:       p.Category,
			Icon:           p.Icon,
			AdminFor:       p.AdminFor,
		})
	}
	writeJSON(w, out)
}

// handleServicePresetInstall installs a bundled preset and streams per-phase
// progress as NDJSON so the UI can show what step is active and surface the
// podman pull output instead of one opaque spinner.
func handleServicePresetInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/services/presets/")
	name = strings.Trim(name, "/")
	if name == "" || strings.Contains(name, "/") {
		http.NotFound(w, r)
		return
	}
	version := r.URL.Query().Get("version")

	writeLine, _ := startNDJSONStream(w, r)
	start := time.Now()

	svc, err := serviceops.InstallPresetStreaming(name, version, func(ev serviceops.PhaseEvent) {
		writeLine(ev)
	})
	if err != nil {
		writeLine(map[string]any{"phase": "error", "error": err.Error()})
		dispatchNotification(notificationForServiceOp("install", name, start, err))
		return
	}
	writeLine(map[string]any{
		"phase":      "done",
		"name":       svc.Name,
		"dashboard":  svc.Dashboard,
		"depends_on": svc.DependsOn,
	})
	dispatchNotification(notificationForServiceOp("install", svc.Name, start, nil))
}

// startNDJSONStream writes the streaming-response headers and returns a
// writeLine that stops after the first failed write or when the client
// disconnects, so a refreshed browser tab can't drive the server to keep
// writing into a broken connection.
func startNDJSONStream(w http.ResponseWriter, r *http.Request) (writeLine func(payload any), alive func() bool) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	dead := false
	ctx := r.Context()
	writeLine = func(payload any) {
		if dead {
			return
		}
		if ctx.Err() != nil {
			dead = true
			return
		}
		data, err := json.Marshal(payload)
		if err != nil {
			dead = true
			return
		}
		if _, err := w.Write(append(data, '\n')); err != nil {
			dead = true
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	alive = func() bool { return !dead && ctx.Err() == nil }
	return writeLine, alive
}

// ServiceActionResponse wraps the service state plus any error details.
type ServiceActionResponse struct {
	ServiceResponse
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Logs  string `json:"logs,omitempty"`
}

// ServiceTuningReadResponse is the JSON returned by GET /api/services/{name}/config.
// Exists distinguishes a real saved override from the seeded template the
// handler hands back when the file is missing — the frontend uses this
// to hide the "back up the current file first" checkbox on first save
// since there's nothing on disk yet to protect.
type ServiceTuningReadResponse struct {
	Supported bool   `json:"supported"`
	Target    string `json:"target"`
	Content   string `json:"content"`
	Exists    bool   `json:"exists"`
}

// ServiceTuningWriteRequest is the JSON body for POST /api/services/{name}/config.
type ServiceTuningWriteRequest struct {
	Content string `json:"content"`
	Backup  bool   `json:"backup"`
}

// ServiceTuningWriteResponse mirrors SiteNginxWriteResponse so the
// frontend can share refresh logic between the two editors. Content +
// Exists round-trip the canonical post-write state (whether or not
// the restart succeeded) so the client can refresh its baseline even
// on the auto-rollback path.
type ServiceTuningWriteResponse struct {
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
	BackupName string `json:"backup_name,omitempty"`
	Content    string `json:"content,omitempty"`
	Exists     bool   `json:"exists,omitempty"`
	// RolledBack is true when the restart failed and the handler
	// successfully restored the previous bytes; the editor uses this
	// to refresh `original` back to the rolled-back state instead of
	// staying perpetually-dirty against bytes that never landed.
	RolledBack bool `json:"rolled_back,omitempty"`
}

// ServiceTuningRestoreRequest carries the exact backup name the frontend
// previewed in the diff modal. Empty means "newest" for tooling that has
// no preview UI.
type ServiceTuningRestoreRequest struct {
	Name string `json:"name"`
}

// ServiceTuningRestoreResponse is the JSON response for POST /api/services/{name}/config/restore.
// RolledBack is true when the restored bytes themselves crashed the
// service and the handler auto-reverted to the pre-restore content;
// the modal uses this to distinguish "restore succeeded" from
// "restore reverted, service is back on its prior config".
type ServiceTuningRestoreResponse struct {
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
	Restored   string `json:"restored,omitempty"`
	Content    string `json:"content,omitempty"`
	RolledBack bool   `json:"rolled_back,omitempty"`
}

// ServiceTuningResetResponse is the JSON returned by POST /api/services/{name}/config/reset.
// AutoBackupName surfaces the implicit recovery snapshot Reset always
// stages of the pre-reset content (separate from the user-opted-in
// backup flag on Save); the modal can show "your previous config is
// kept as <name>, restore it any time" so users don't fear the action.
type ServiceTuningResetResponse struct {
	OK             bool   `json:"ok"`
	Error          string `json:"error,omitempty"`
	RolledBack     bool   `json:"rolled_back,omitempty"`
	AutoBackupName string `json:"auto_backup_name,omitempty"`
	Content        string `json:"content,omitempty"`
	Exists         bool   `json:"exists,omitempty"`
}

// handleServiceTuning reads (GET) or saves (POST) a service's user
// tuning override. The save path optionally stages a timestamped
// backup, writes the new bytes, regenerates the quadlet so the mount
// is present, restarts the unit, and waits for the service to come
// ready — if it doesn't, the previous bytes are restored and the
// service is restarted again so the user only loses their unsaved
// edits, not the running service.
func handleServiceTuning(w http.ResponseWriter, r *http.Request, name string) {
	if !serviceops.ServiceInstalled(name) {
		http.Error(w, "service is not installed", http.StatusNotFound)
		return
	}
	svc, err := config.ResolveServiceForTuning(name)
	if err != nil {
		http.Error(w, "service not installed", http.StatusNotFound)
		return
	}
	target, ok := config.ServiceTuningMount(svc)
	if !ok {
		http.Error(w, "service does not support tuning", http.StatusBadRequest)
		return
	}
	if err := config.MaterializeServiceTuning(svc); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	path := config.ServiceTuningFile(name)
	if r.Method == http.MethodGet {
		body, err := os.ReadFile(path)
		exists := err == nil
		if err != nil && !os.IsNotExist(err) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, ServiceTuningReadResponse{Supported: true, Target: target, Content: string(body), Exists: exists})
		return
	}
	var req ServiceTuningWriteRequest
	// Cap the POST body so a multi-gigabyte payload can't be streamed
	// straight to disk via os.WriteFile. 64 KiB matches the
	// nginx endpoints in this file.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeJSON(w, ServiceTuningWriteResponse{OK: false, Error: "invalid body: " + err.Error()})
		return
	}
	saveRes, err := serviceops.SaveTuningOverride(name, req.Content, req.Backup)
	if err != nil {
		// Auto-rollback path: the file is back to its pre-save bytes
		// and the service was restarted against those bytes. We still
		// return ok:false so the modal stays open with the error, but
		// the response carries the rolled-back content/exists so the
		// editor can update its baseline.
		switch {
		case errors.Is(err, serviceops.ErrTuningServiceNotInstalled):
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		case errors.Is(err, serviceops.ErrTuningFamilyUnsupported):
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, ServiceTuningWriteResponse{
			OK:         false,
			Error:      err.Error(),
			BackupName: saveRes.BackupName,
			Content:    saveRes.ContentOnDisk,
			Exists:     saveRes.Exists,
			RolledBack: saveRes.RolledBack,
		})
		return
	}
	writeJSON(w, ServiceTuningWriteResponse{
		OK:         true,
		BackupName: saveRes.BackupName,
		Content:    saveRes.ContentOnDisk,
		Exists:     saveRes.Exists,
	})
}

func handleServiceTuningBackups(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !serviceops.ServiceInstalled(name) {
		http.NotFound(w, r)
		return
	}
	list, err := serviceops.ListTuningBackups(name)
	if err != nil {
		http.Error(w, "listing backups: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []serviceops.TuningBackup{}
	}
	writeJSON(w, list)
}

func handleServiceTuningBackupContent(w http.ResponseWriter, r *http.Request, name, backupName string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !serviceops.ServiceInstalled(name) {
		http.NotFound(w, r)
		return
	}
	data, err := serviceops.ReadTuningBackupContent(name, backupName)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "reading backup: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func handleServiceTuningRestore(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !serviceops.ServiceInstalled(name) {
		http.NotFound(w, r)
		return
	}
	var req ServiceTuningRestoreRequest
	// Always attempt decode regardless of ContentLength. Chunked
	// Transfer-Encoding sets ContentLength to -1, so the previous
	// `> 0` guard silently dropped the backup name and served the
	// newest backup instead of the one the user previewed. An empty
	// body still parses as the zero value via io.EOF, which we
	// accept as "no name, restore newest".
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	if err := dec.Decode(&req); err != nil && err != io.EOF {
		writeJSON(w, ServiceTuningRestoreResponse{OK: false, Error: "invalid body: " + err.Error()})
		return
	}
	list, err := serviceops.ListTuningBackups(name)
	if err != nil {
		writeJSON(w, ServiceTuningRestoreResponse{OK: false, Error: err.Error()})
		return
	}
	if len(list) == 0 {
		writeJSON(w, ServiceTuningRestoreResponse{OK: false, Error: "no backup available"})
		return
	}
	backupName := req.Name
	if backupName == "" {
		backupName = list[0].Name
	} else {
		found := false
		for _, b := range list {
			if b.Name == backupName {
				found = true
				break
			}
		}
		if !found {
			writeJSON(w, ServiceTuningRestoreResponse{OK: false, Error: "backup not found: " + backupName})
			return
		}
	}
	res, err := serviceops.RestoreTuningFromBackup(name, backupName)
	if err != nil {
		// On failure RestoreTuningFromBackup may have auto-rolled
		// back to the pre-restore bytes; the response surfaces both
		// the canonical on-disk content (so the editor refreshes its
		// baseline) and the RolledBack flag so the modal can render
		// a recovery-aware message.
		writeJSON(w, ServiceTuningRestoreResponse{
			OK:         false,
			Error:      err.Error(),
			Restored:   backupName,
			Content:    res.ContentOnDisk,
			RolledBack: res.RolledBack,
		})
		return
	}
	writeJSON(w, ServiceTuningRestoreResponse{
		OK:       true,
		Restored: backupName,
		Content:  res.Content,
	})
}

func handleServiceTuningReset(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	res, err := serviceops.ResetTuningOverride(name)
	if err != nil {
		switch {
		case errors.Is(err, serviceops.ErrTuningServiceNotInstalled):
			http.Error(w, err.Error(), http.StatusNotFound)
		case errors.Is(err, serviceops.ErrTuningFamilyUnsupported):
			http.Error(w, err.Error(), http.StatusBadRequest)
		default:
			writeJSON(w, ServiceTuningResetResponse{
				OK:             false,
				Error:          err.Error(),
				RolledBack:     res.RolledBack,
				AutoBackupName: res.AutoBackupName,
				Content:        res.ContentOnDisk,
				Exists:         res.Exists,
			})
		}
		return
	}
	writeJSON(w, ServiceTuningResetResponse{
		OK:             true,
		AutoBackupName: res.AutoBackupName,
		Content:        res.ContentOnDisk,
		Exists:         res.Exists,
	})
}

func handleServiceAction(w http.ResponseWriter, r *http.Request) {
	// path: /api/services/{name}/start or /api/services/{name}/stop
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/services/"), "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}

	// /config subroutes (backups, restore, reset) sit alongside the
	// existing GET/POST on /config. Domain validation inside each
	// handler keeps the {name} segment from leaking path traversal.
	if len(parts) >= 3 && parts[1] == "config" {
		name := parts[0]
		switch parts[2] {
		case "backups":
			if len(parts) == 3 {
				handleServiceTuningBackups(w, r, name)
				return
			}
			if len(parts) == 4 {
				handleServiceTuningBackupContent(w, r, name, parts[3])
				return
			}
		case "restore":
			if len(parts) == 3 {
				handleServiceTuningRestore(w, r, name)
				return
			}
		case "reset":
			if len(parts) == 3 {
				handleServiceTuningReset(w, r, name)
				return
			}
		}
		http.NotFound(w, r)
		return
	}

	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	name, action := parts[0], parts[1]

	// Allow GET for logs sub-resource
	if action == "logs" {
		writeJSON(w, map[string]string{"logs": serviceRecentLogs("servlo-" + name)})
		return
	}

	// Read-only update-availability check. POST forces a fresh registry
	// fetch (used by the manual "Check for updates" button); GET uses the
	// cached value so snapshot rebuilds stay cheap.
	if action == "updates" {
		var (
			avail *serviceops.UpdateAvailability
			err   error
		)
		if r.Method == http.MethodPost {
			avail, err = serviceops.RefreshUpdateAvailability(name)
		} else {
			avail, err = serviceops.CheckUpdateAvailable(name)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, avail)
		return
	}

	// Streaming migration: dump current data, swap data dir, start new image,
	// restore dump. Backups land in ~/.local/share/servlo/backups.
	if action == "migrate" && r.Method == http.MethodPost {
		targetTag := r.URL.Query().Get("tag")
		if targetTag == "" {
			http.Error(w, "tag query parameter required", http.StatusBadRequest)
			return
		}
		avail, err := serviceops.CheckUpdateAvailable(name)
		if err != nil || avail.CurrentImage == "" {
			http.Error(w, "could not resolve current image", http.StatusBadRequest)
			return
		}
		targetImage, err := serviceops.ResolveMigrateTarget(name, avail.CurrentImage, targetTag)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeLine, _ := startNDJSONStream(w, r)
		start := time.Now()
		err = serviceops.MigrateService(name, targetImage, func(ev serviceops.PhaseEvent) { writeLine(ev) })
		if err != nil {
			writeLine(map[string]any{"phase": "error", "error": err.Error()})
		}
		dispatchNotification(notificationForServiceOp("migrate", name, start, err))
		return
	}

	// Streaming rollback: pull the previously-running image and restart.
	if action == "rollback" && r.Method == http.MethodPost {
		writeLine, _ := startNDJSONStream(w, r)
		start := time.Now()
		err := serviceops.RollbackService(name, func(ev serviceops.PhaseEvent) { writeLine(ev) })
		if err != nil {
			writeLine(map[string]any{"phase": "error", "error": err.Error()})
		}
		dispatchNotification(notificationForServiceOp("rollback", name, start, err))
		return
	}

	if action == "reinstall" && r.Method == http.MethodPost {
		resetData := r.URL.Query().Get("resetData") == "true"
		writeLine, _ := startNDJSONStream(w, r)
		start := time.Now()
		err := serviceops.ReinstallService(name, resetData, func(ev serviceops.PhaseEvent) { writeLine(ev) })
		if err != nil {
			writeLine(map[string]any{"phase": "error", "error": err.Error()})
		}
		dispatchNotification(notificationForServiceOp("reinstall", name, start, err))
		return
	}

	if action == "ports" && r.Method == http.MethodPost {
		handleServicePorts(w, r, name)
		return
	}

	if action == "shims" && r.Method == http.MethodPost {
		handleServiceShims(w, r, name)
		return
	}

	if action == "update" && r.Method == http.MethodPost {
		targetTag := r.URL.Query().Get("tag")
		var targetImage string
		if targetTag != "" {
			if avail, err := serviceops.CheckUpdateAvailable(name); err == nil && avail.CurrentImage != "" {
				if at := strings.LastIndex(avail.CurrentImage, ":"); at > 0 {
					targetImage = avail.CurrentImage[:at] + ":" + targetTag
				} else {
					targetImage = avail.CurrentImage + ":" + targetTag
				}
			}
		}
		writeLine, _ := startNDJSONStream(w, r)
		start := time.Now()
		err := serviceops.UpdateServiceStreaming(name, targetImage, func(ev serviceops.PhaseEvent) {
			writeLine(ev)
		})
		if err != nil {
			writeLine(map[string]any{"phase": "error", "error": err.Error()})
		}
		dispatchNotification(notificationForServiceOp("update", name, start, err))
		return
	}

	// Tuning override: GET reads the user-editable config file (seeding it on
	// first access), POST saves it and restarts the service so it re-reads.
	if action == "config" && (r.Method == http.MethodGet || r.Method == http.MethodPost) {
		handleServiceTuning(w, r, name)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// If the name matches a registered custom service, skip the prefix-based
	// per-site routes below — otherwise a custom service named e.g. "stripe-mock"
	// would be routed as a per-site stripe listener and fail with
	// "unsupported action for stripe listener".
	_, customLoadErr := config.LoadCustomService(name)
	isCustom := customLoadErr == nil

	// Handle queue worker services (queue-{sitename})
	if !isCustom && strings.HasPrefix(name, "queue-") {
		siteName := strings.TrimPrefix(name, "queue-")
		if action == "stop" {
			opErr := podman.StopUnit("servlo-queue-" + siteName)
			resp := ServiceActionResponse{
				ServiceResponse: ServiceResponse{Name: name, Status: "inactive", EnvVars: map[string]string{}, QueueSite: siteName},
				OK:              opErr == nil,
			}
			if opErr != nil {
				resp.Error = opErr.Error()
				resp.Status = "active"
			}
			writeJSON(w, resp)
		} else {
			http.Error(w, "unsupported action for queue worker", http.StatusBadRequest)
		}
		return
	}

	// Handle stripe listener services (stripe-{sitename})
	if !isCustom && strings.HasPrefix(name, "stripe-") {
		siteName := strings.TrimPrefix(name, "stripe-")
		if action == "stop" {
			opErr := cli.StripeStopForSite(siteName)
			resp := ServiceActionResponse{
				ServiceResponse: ServiceResponse{Name: name, Status: "inactive", EnvVars: map[string]string{}, StripeListenerSite: siteName},
				OK:              opErr == nil,
			}
			if opErr != nil {
				resp.Error = opErr.Error()
				resp.Status = "active"
			}
			writeJSON(w, resp)
		} else {
			writeJSON(w, ServiceActionResponse{OK: false, Error: "unsupported action for stripe listener"})
		}
		return
	}

	// Handle schedule worker services (schedule-{sitename})
	if !isCustom && strings.HasPrefix(name, "schedule-") {
		siteName := strings.TrimPrefix(name, "schedule-")
		if action == "stop" {
			opErr := cli.ScheduleStopForSite(siteName)
			resp := ServiceActionResponse{
				ServiceResponse: ServiceResponse{Name: name, Status: "inactive", EnvVars: map[string]string{}, ScheduleWorkerSite: siteName},
				OK:              opErr == nil,
			}
			if opErr != nil {
				resp.Error = opErr.Error()
				resp.Status = "active"
			}
			writeJSON(w, resp)
		} else {
			writeJSON(w, ServiceActionResponse{OK: false, Error: "unsupported action for schedule worker"})
		}
		return
	}

	// Handle horizon worker services (horizon-{sitename})
	if !isCustom && strings.HasPrefix(name, "horizon-") {
		siteName := strings.TrimPrefix(name, "horizon-")
		if action == "stop" {
			opErr := cli.HorizonStopForSite(siteName)
			resp := ServiceActionResponse{
				ServiceResponse: ServiceResponse{Name: name, Status: "inactive", EnvVars: map[string]string{}, HorizonSite: siteName},
				OK:              opErr == nil,
			}
			if opErr != nil {
				resp.Error = opErr.Error()
				resp.Status = "active"
			}
			writeJSON(w, resp)
		} else {
			writeJSON(w, ServiceActionResponse{OK: false, Error: "unsupported action for horizon worker"})
		}
		return
	}

	// Handle reverb server services (reverb-{sitename})
	if !isCustom && strings.HasPrefix(name, "reverb-") {
		siteName := strings.TrimPrefix(name, "reverb-")
		if action == "stop" {
			opErr := cli.ReverbStopForSite(siteName)
			resp := ServiceActionResponse{
				ServiceResponse: ServiceResponse{Name: name, Status: "inactive", EnvVars: map[string]string{}, ReverbSite: siteName},
				OK:              opErr == nil,
			}
			if opErr != nil {
				resp.Error = opErr.Error()
				resp.Status = "active"
			}
			writeJSON(w, resp)
		} else {
			writeJSON(w, ServiceActionResponse{OK: false, Error: "unsupported action for reverb server"})
		}
		return
	}

	// Custom framework workers: {workerName}-{siteName}.
	if action == "stop" {
		if reg3, err3 := config.LoadSites(); err3 == nil {
			for _, s := range reg3.Sites {
				if s.Ignored {
					continue
				}
				fwN3 := s.Framework
				fw3, ok3 := config.GetFrameworkForDir(fwN3, s.Path)
				if !ok3 || fw3.Workers == nil {
					continue
				}
				for wname := range fw3.Workers {
					switch wname {
					case "queue", "schedule", "reverb":
						continue
					}
					prefix := wname + "-" + s.Name
					if name == prefix {
						opErr := cli.WorkerStopForSite(s.Name, s.Path, wname)
						resp := ServiceActionResponse{
							ServiceResponse: ServiceResponse{Name: name, Status: "inactive", EnvVars: map[string]string{}, WorkerSite: s.Name, WorkerName: wname},
							OK:              opErr == nil,
						}
						if opErr != nil {
							resp.Error = opErr.Error()
							resp.Status = "active"
						}
						writeJSON(w, resp)
						return
					}
				}
			}
		}
	}

	// Validate service name — built-in or custom
	if !config.IsDefaultPreset(name) {
		if _, loadErr := config.LoadCustomService(name); loadErr != nil {
			http.Error(w, "unknown service", http.StatusNotFound)
			return
		}
	}

	unit := "servlo-" + name
	var opErr error

	switch action {
	case "start":
		opErr = serviceops.StartService(name)
	case "stop":
		opErr = serviceops.StopService(name)
	case "restart":
		opErr = serviceops.RestartService(name)
	case "remove":
		removeData := r.URL.Query().Get("removeData") == "true"
		if err := serviceops.RemoveService(name, serviceops.RemoveOptions{RemoveData: removeData}, nil); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
		return
	case "pin":
		if opErr = config.SetServicePinned(name, true); opErr == nil {
			status, _ := podman.UnitStatus(unit)
			if status != "active" {
				opErr = serviceops.StartService(name)
			}
		}
	case "unpin":
		opErr = config.SetServicePinned(name, false)
	default:
		http.NotFound(w, r)
		return
	}

	if opErr != nil {
		writeJSON(w, ServiceActionResponse{
			ServiceResponse: buildServiceResponse(name),
			OK:              false,
			Error:           opErr.Error(),
			Logs:            serviceRecentLogs(unit),
		})
		return
	}

	writeJSON(w, ServiceActionResponse{
		ServiceResponse: buildServiceResponse(name),
		OK:              true,
	})
}

// handleServicePorts sets a service's published host port and (for built-in
// services) its extra published ports in one request, routing both through the
// shared serviceops layer so the Web UI enforces the same validation, guard and
// host-proxy refresh as the CLI. A nil/zero published_port resets to the
// preset default.
// servicePortsMu serializes port-modal saves so their snapshot/apply/restore
// sequences can't interleave across concurrent requests.
var servicePortsMu sync.Mutex

func handleServicePorts(w http.ResponseWriter, r *http.Request, name string) {
	var body struct {
		PublishedPort  *int           `json:"published_port"`
		PublishedPorts map[string]int `json:"published_ports"`
		ExtraPorts     []string       `json:"extra_ports"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	fail := func(err error) {
		writeJSON(w, ServiceActionResponse{
			ServiceResponse: buildServiceResponse(name),
			OK:              false,
			Error:           err.Error(),
		})
	}
	port := 0
	if body.PublishedPort != nil {
		port = *body.PublishedPort
	}
	apply := func() error {
		if _, err := serviceops.SetPublishedPort(name, port); err != nil {
			return err
		}
		// Secondary published ports keyed by container-internal port (a multi-port
		// service's UI/console mapping). Each moves through the same shared gate.
		for cs, hp := range body.PublishedPorts {
			c, err := strconv.Atoi(cs)
			if err != nil {
				continue
			}
			if _, err := serviceops.SetPublishedPortFor(name, c, hp); err != nil {
				return err
			}
		}
		// Extra ports apply to any preset servlo ships; genuinely custom services
		// declare their ports in their own YAML.
		if config.PresetExists(name) {
			if err := serviceops.SetExtraPorts(name, body.ExtraPorts); err != nil {
				return err
			}
		}
		return nil
	}
	// A modal save applies the primary, secondaries, and extras in sequence.
	// Snapshot first so a failure partway through rolls the whole save back to a
	// consistent state. The lock spans snapshot, apply and restore so two
	// concurrent saves can't interleave and restore each other's wrong baseline.
	servicePortsMu.Lock()
	snapshot, canRestore := serviceops.SnapshotPublishedPorts(name)
	err := apply()
	if err != nil && canRestore {
		_ = serviceops.RestorePublishedPorts(name, snapshot)
	}
	servicePortsMu.Unlock()
	if err != nil {
		fail(err)
		return
	}
	writeJSON(w, ServiceActionResponse{
		ServiceResponse: buildServiceResponse(name),
		OK:              true,
	})
}

// handleServiceShims records the user's decision for one client-tool shim and
// reconciles the shim dir so it takes effect immediately. Mirrors the ports
// handler's request/response shape so the frontend refreshes off the returned
// service state.
func handleServiceShims(w http.ResponseWriter, r *http.Request, name string) {
	var body struct {
		Tool    string `json:"tool"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Tool == "" {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	// A shared same-family tool is managed only from its owning service, so a
	// toggle from a non-owner is rejected (its toggle is disabled in the UI).
	if owner := shims.ToolOwner(body.Tool); owner != "" && owner != name {
		writeJSON(w, ServiceActionResponse{
			ServiceResponse: buildServiceResponse(name),
			OK:              false,
			Error:           fmt.Sprintf("%s is provided by %s; manage it there", body.Tool, owner),
		})
		return
	}
	if err := shims.Set(body.Tool, body.Enabled); err != nil {
		writeJSON(w, ServiceActionResponse{
			ServiceResponse: buildServiceResponse(name),
			OK:              false,
			Error:           err.Error(),
		})
		return
	}
	writeJSON(w, ServiceActionResponse{
		ServiceResponse: buildServiceResponse(name),
		OK:              true,
	})
}

// ensureServiceQuadlet writes the unit file for a default-preset service.
// Delegates to serviceops so install + runtime + all generate the same
// quadlet (and re-materialise file mounts like mysql's servlo.cnf).
func ensureServiceQuadlet(name string) error {
	return serviceops.EnsureDefaultPresetQuadlet(name)
}

// ensureCustomServiceQuadlet writes the quadlet for a custom service and reloads systemd.
func ensureCustomServiceQuadlet(svc *config.CustomService) error {
	return serviceops.EnsureCustomServiceQuadlet(svc)
}

// countSitesUsingService counts how many active site .env files reference servlo-{name}.
func countSitesUsingService(name string) int {
	return config.CountSitesUsingService(name)
}

// sitesUsingService returns the domains of active sites that use the named service.
// Checks both .servlo.yaml services list and .env file references.
func sitesUsingService(name string) []string {
	reg, err := config.LoadSites()
	if err != nil {
		return nil
	}
	needle := "servlo-" + name
	var domains []string
	for _, s := range reg.Sites {
		if s.Ignored || s.Paused {
			continue
		}
		// Check .servlo.yaml services list first.
		if proj, pErr := config.LoadProjectConfig(s.Path); pErr == nil {
			found := false
			for _, svc := range proj.Services {
				if svc.Name == name {
					found = true
					break
				}
			}
			if found {
				domains = append(domains, s.PrimaryDomain())
				continue
			}
		}
		// Fall back to .env scanning.
		data, err := os.ReadFile(filepath.Join(s.Path, ".env"))
		if err != nil {
			continue
		}
		if strings.Contains(string(data), needle) {
			domains = append(domains, s.PrimaryDomain())
		}
	}
	return domains
}

// serviceRecentLogs is implemented in logs_linux.go.

// VersionResponse is the response for GET /api/version.
type VersionResponse struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	HasUpdate bool   `json:"has_update"`
	Changelog string `json:"changelog,omitempty"`
}

func handleVersion(w http.ResponseWriter, r *http.Request, currentVersion string) {
	check := servloUpdate.CachedUpdateCheck
	if r.URL.Query().Get("refresh") != "" {
		// An explicit user-initiated check bypasses the 24h cache for a live answer.
		check = servloUpdate.ForceUpdateCheck
	}
	info, _ := check(currentVersion)
	writeJSON(w, buildVersionResponse(currentVersion, info))
}

// buildVersionResponse builds the wire payload for /api/version. The
// dashboard banner template already prepends "v" so the Latest field
// must be stripped of any leading v from the GitHub tag, otherwise
// users see "vv1.20.0" in the banner.
func buildVersionResponse(currentVersion string, info *servloUpdate.UpdateInfo) VersionResponse {
	resp := VersionResponse{Current: currentVersion}
	if info != nil {
		resp.Latest = servloUpdate.StripV(info.LatestVersion)
		resp.HasUpdate = true
		resp.Changelog = info.Changelog
	}
	return resp
}

func handlePHPVersions(w http.ResponseWriter, _ *http.Request) {
	versions, _ := phpPkg.ListInstalled()
	if versions == nil {
		versions = []string{}
	}
	writeJSON(w, versions)
}

func handleNodeVersions(w http.ResponseWriter, _ *http.Request) {
	versions := servloNode.ListInstalled()
	if versions == nil {
		versions = []string{}
	}
	writeJSON(w, versions)
}

func handleSiteFavicon(w http.ResponseWriter, r *http.Request) {
	// path: /api/sites/{domain}/favicon
	domain := strings.TrimPrefix(r.URL.Path, "/api/sites/")
	domain = strings.TrimSuffix(domain, "/favicon")

	site, err := config.FindSiteByDomain(domain)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	path := siteinfo.DetectFavicon(site.Path, site.PublicDir, site.Framework, nil, false)
	if path == "" {
		http.NotFound(w, r)
		return
	}

	http.ServeFile(w, r, path)
}

// handleSiteEnv dispatches the per-site .env endpoint. GET returns the raw
// contents (or empty body for missing files), PUT replaces them with an
// optional pre-overwrite backup. POST and other methods are rejected so
// future shared dispatch does not accidentally widen the contract.
//
//	GET /api/sites/{domain}/env
//	PUT /api/sites/{domain}/env
func handleSiteEnv(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleSiteEnvRead(w, r)
	case http.MethodPut:
		handleSiteEnvWrite(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleSiteEnvRead(w http.ResponseWriter, r *http.Request) {
	domain := strings.TrimPrefix(r.URL.Path, "/api/sites/")
	domain = strings.TrimSuffix(domain, "/env")

	site, err := config.FindSiteByDomain(domain)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	dir, envFile, ok := resolveEnvTarget(w, r, site)
	if !ok {
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	data, err := os.ReadFile(filepath.Join(dir, envFile))
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		http.Error(w, "reading "+envFile+": "+err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(data)
}

// SiteEnvWriteRequest is the JSON body for PUT /api/sites/{domain}/env.
type SiteEnvWriteRequest struct {
	Content string `json:"content"`
	Backup  bool   `json:"backup"`
}

// SiteEnvWriteResponse is the JSON response for PUT /api/sites/{domain}/env.
type SiteEnvWriteResponse struct {
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
	BackupPath string `json:"backup_path,omitempty"`
}

func handleSiteEnvWrite(w http.ResponseWriter, r *http.Request) {
	domain := strings.TrimPrefix(r.URL.Path, "/api/sites/")
	domain = strings.TrimSuffix(domain, "/env")

	site, err := config.FindSiteByDomain(domain)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	dir, envFile, ok := resolveEnvTarget(w, r, site)
	if !ok {
		return
	}

	var body SiteEnvWriteRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&body); err != nil {
		writeJSON(w, SiteEnvWriteResponse{OK: false, Error: "invalid body: " + err.Error()})
		return
	}

	res, err := envCfgFile(dir, envFile).Save(body.Content, cfgedit.SaveOpts{Backup: body.Backup})
	if err != nil {
		writeJSON(w, SiteEnvWriteResponse{OK: false, Error: err.Error()})
		return
	}
	if !res.OK {
		writeJSON(w, SiteEnvWriteResponse{OK: false, Error: res.Error})
		return
	}
	writeJSON(w, SiteEnvWriteResponse{OK: true, BackupPath: res.BackupName})
}

// envCfgFile builds the cfgedit.File for one of a site's env files. Env files
// are not behind any include glob, so backups and write-staging share the
// project dir; backups are named "{envFile}.bkp.{ts}".
func envCfgFile(dir, envFile string) cfgedit.File {
	// envFile may be nested (CakePHP config/.env), so the backup dir is the
	// file's own dir and the name is its base. Keying BkpDir/BkpName off the
	// joined path keeps backups next to the file and listable; a root .env
	// still resolves to BkpDir=dir, BkpName=.env as before.
	full := filepath.Join(dir, envFile)
	return cfgedit.File{
		Path:    full,
		BkpDir:  filepath.Dir(full),
		BkpName: filepath.Base(full),
	}
}

// envFileRe matches the names of env files the UI is willing to expose for
// editing. The user-facing dropdown also runs filenames through this regex.
// Backup files like ".env.20260528-103045" never match because the suffix
// must start with a letter, and servlo's own ".env.before_servlo" is explicitly
// excluded so it stays out of the editor.
var envFileRe = regexp.MustCompile(`^\.env(\.[A-Za-z][A-Za-z0-9_-]*)?$`)

var envExcludedFiles = map[string]bool{
	".env.before_servlo": true,
}

// envFileFromQuery extracts the ?file= parameter and validates it. An empty
// ?file= defaults to the framework's declared env file. That file is always
// allowed even when it lives in a subdirectory (CakePHP config/.env), which
// envFileRe rejects; every other name must be a root dotenv variant.
func envFileFromQuery(r *http.Request, defaultFile string) (string, bool) {
	f := r.URL.Query().Get("file")
	if f == "" {
		return defaultFile, true
	}
	if f == defaultFile {
		return f, true
	}
	if envExcludedFiles[f] || !envFileRe.MatchString(f) {
		return "", false
	}
	return f, true
}

// resolveEnvTarget resolves the target env file for a site env request, shared
// by all five /env endpoints so they agree on the file set. It writes the error
// and returns ok=false when the framework has no editable dotenv or the file is
// invalid (400).
func resolveEnvTarget(w http.ResponseWriter, r *http.Request, site *config.Site) (dir, envFile string, ok bool) {
	dir = site.Path
	def, has := frameworkEnvFile(site.Framework, dir)
	if !has {
		http.Error(w, "invalid file", http.StatusBadRequest)
		return "", "", false
	}
	envFile, ok = envFileFromQuery(r, def)
	if !ok {
		http.Error(w, "invalid file", http.StatusBadRequest)
		return "", "", false
	}
	return dir, envFile, true
}

// listEnvFiles enumerates the project's editable env files in dir. The
// framework's declared file appears first; the rest are alphabetical.
func listEnvFiles(frameworkName, dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := []string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if envExcludedFiles[name] || !envFileRe.MatchString(name) {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)

	primary, ok := frameworkEnvFile(frameworkName, dir)
	if !ok {
		// Framework has no editable dotenv (WordPress, Magento): read/write/
		// backup all 400, so /env/files must not list root dotenvs either.
		return nil, nil
	}
	// The root scan only sees names envFileRe accepts, so it misses a declared
	// file that is nested (CakePHP config/.env) or simply named something else.
	// Surface it whenever it is on disk, then hoist it: the file the framework
	// actually reads is the one to pre-select, and the rest stay alphabetical.
	scanned := false
	for i, n := range out {
		if n == primary {
			out = append(out[:i], out[i+1:]...)
			scanned = true
			break
		}
	}
	if !scanned {
		if info, statErr := os.Stat(filepath.Join(dir, primary)); statErr != nil || info.IsDir() {
			return out, nil
		}
	}
	return append([]string{primary}, out...), nil
}

// SiteEnvBackup is one row in the GET /api/sites/{domain}/env/backups list.
// It aliases cfgedit.Backup so the env editor shares the edit service's shape.
type SiteEnvBackup = cfgedit.Backup

func handleSiteEnvBackupContent(w http.ResponseWriter, r *http.Request, site *config.Site, name string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	dir, envFile, ok := resolveEnvTarget(w, r, site)
	if !ok {
		return
	}
	data, err := envCfgFile(dir, envFile).ReadBackup(name)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "reading backup: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func handleSiteEnvBackups(w http.ResponseWriter, r *http.Request, site *config.Site) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	dir, envFile, ok := resolveEnvTarget(w, r, site)
	if !ok {
		return
	}
	list, err := envCfgFile(dir, envFile).ListBackups()
	if err != nil {
		http.Error(w, "listing backups: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []cfgedit.Backup{}
	}
	writeJSON(w, list)
}

func handleSiteEnvFiles(w http.ResponseWriter, r *http.Request, site *config.Site) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	dir := site.Path
	files, err := listEnvFiles(site.Framework, dir)
	if err != nil {
		http.Error(w, "listing env files: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if files == nil {
		files = []string{}
	}
	writeJSON(w, files)
}

// SiteEnvProposeResponse previews inserting the .env.example keys a site's env
// file is missing. Current and Merged feed a diff editor; Added lists the keys
// the merge inserts, Required/Optional the doctor's classification so the UI can
// offer to also pull in the optional (has-a-code-default) keys. Every slice is
// non-nil so the client can render without null guards.
type SiteEnvProposeResponse struct {
	File       string                `json:"file"`
	Current    string                `json:"current"`
	Merged     string                `json:"merged"`
	Added      []string              `json:"added"`
	AddedLines []int                 `json:"addedLines"`
	Required   []string              `json:"required"`
	Optional   []string              `json:"optional"`
	Entries    []SiteEnvProposeEntry `json:"entries"`
}

// SiteEnvProposeEntry is one missing example key with the value that would be
// written for it (verbatim from .env.example) and whether the app requires it,
// so the UI can list every candidate key with its value for the user to pick.
type SiteEnvProposeEntry struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Required bool   `json:"required"`
}

// handleSiteEnvPropose returns a proposed .env that inserts the framework env
// file's missing example keys next to their neighbours. It targets the
// framework-resolved env file (the same one the env_drift check inspects), so
// ?file is not honoured; ?optional=1 also pulls in the keys the app reads with
// a code default.
func handleSiteEnvPropose(w http.ResponseWriter, r *http.Request, site *config.Site) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	dir := site.Path

	// GetFrameworkForDir, like the Env tab's own resolver: GetFramework returns
	// the Go built-in and ignores the versioned store yaml, so it would propose
	// against a different file than the one the tab has open and the banner
	// (gated on the two agreeing) could never appear.
	var fw *config.Framework
	if f, ok := config.GetFrameworkForDir(site.Framework, dir); ok {
		fw = f
	}
	prop, ok := sitedoctor.ProposeEnvMerge(dir, fw)
	if !ok {
		writeJSON(w, SiteEnvProposeResponse{File: ".env", Added: []string{}, AddedLines: []int{}, Required: []string{}, Optional: []string{}, Entries: []SiteEnvProposeEntry{}})
		return
	}

	values := envfile.ExampleValues(prop.ExampleContent)
	entries := make([]SiteEnvProposeEntry, 0, len(prop.Required)+len(prop.Optional))
	for _, k := range prop.Required {
		entries = append(entries, SiteEnvProposeEntry{Key: k, Value: values[k], Required: true})
	}
	for _, k := range prop.Optional {
		entries = append(entries, SiteEnvProposeEntry{Key: k, Value: values[k], Required: false})
	}

	// ?keys=A,B stages exactly the picked keys (intersected with the missing
	// set so the caller can't inject arbitrary example lines); without it we
	// fall back to all required keys, plus the optional ones when ?optional=1.
	include := make(map[string]bool, len(prop.Required)+len(prop.Optional))
	if sel := r.URL.Query().Get("keys"); sel != "" {
		missing := make(map[string]bool, len(entries))
		for _, e := range entries {
			missing[e.Key] = true
		}
		for _, k := range strings.Split(sel, ",") {
			if k = strings.TrimSpace(k); missing[k] {
				include[k] = true
			}
		}
	} else {
		for _, k := range prop.Required {
			include[k] = true
		}
		if r.URL.Query().Get("optional") == "1" {
			for _, k := range prop.Optional {
				include[k] = true
			}
		}
	}
	res := envfile.MergeMissing(prop.ExampleContent, prop.EnvContent, include)

	addedLines := res.AddedLines
	if addedLines == nil {
		addedLines = []int{}
	}
	writeJSON(w, SiteEnvProposeResponse{
		File:       prop.EnvFile,
		Current:    prop.EnvContent,
		Merged:     res.Merged,
		Added:      nonNilStrings(res.Added),
		AddedLines: addedLines,
		Required:   nonNilStrings(prop.Required),
		Optional:   nonNilStrings(prop.Optional),
		Entries:    entries,
	})
}

// nonNilStrings returns s unchanged unless it is nil, in which case it returns
// an empty slice so JSON encodes [] rather than null.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// SiteEnvRestoreRequest carries the previewed backup name so the restore
// applies the exact bytes the user saw, not whatever is newest at accept time.
type SiteEnvRestoreRequest struct {
	Name string `json:"name"`
}

// SiteEnvRestoreResponse is the JSON body returned by POST /env/restore.
type SiteEnvRestoreResponse struct {
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	Restored string `json:"restored,omitempty"`
	Content  string `json:"content,omitempty"`
}

func handleSiteEnvRestore(w http.ResponseWriter, r *http.Request, site *config.Site) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	dir, envFile, ok := resolveEnvTarget(w, r, site)
	if !ok {
		return
	}
	// Always attempt the decode: an empty body parses as the zero value via
	// io.EOF, which Restore treats as "restore newest". A previewed name is
	// validated against the live backup list inside Restore.
	var req SiteEnvRestoreRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	if err := dec.Decode(&req); err != nil && err != io.EOF {
		writeJSON(w, SiteEnvRestoreResponse{OK: false, Error: "invalid body: " + err.Error()})
		return
	}
	res, err := envCfgFile(dir, envFile).Restore(req.Name, nil)
	if err != nil {
		writeJSON(w, SiteEnvRestoreResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, SiteEnvRestoreResponse(res))
}

// handleDashboardQR serves a QR code PNG encoding the dashboard's own LAN URL
// (https://<lan-ip>:7073) so a phone can scan straight into the remote
// dashboard. Only meaningful while LAN exposure is on; 404 otherwise.
func handleDashboardQR(w http.ResponseWriter, r *http.Request) {
	cfg, _ := config.LoadGlobal()
	if cfg == nil || !cfg.LAN.Exposed {
		http.NotFound(w, r)
		return
	}
	ip := uiPrimaryLANIP()
	if ip == "" {
		http.NotFound(w, r)
		return
	}
	png, err := qrcode.Encode("https://"+ip+":7073", qrcode.Medium, 160)
	if err != nil {
		http.Error(w, "qr encode: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "qr.png", time.Time{}, bytes.NewReader(png))
}

// SiteNginxBackup is the backup metadata the frontend's restore dropdown
// consumes. It aliases cfgedit.Backup so the site and global-nginx
// editors all surface the same shape from the shared edit service.
type SiteNginxBackup = cfgedit.Backup

// SiteNginxReadResponse is the JSON returned by GET /api/sites/{domain}/nginx.
// Exists distinguishes a real saved override from the seeded template the
// handler hands back when the file is missing; the frontend uses this to
// hide the "back up the current file first" checkbox on first save since
// there's nothing on disk yet to protect.
type SiteNginxReadResponse struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Exists  bool   `json:"exists"`
}

// SiteNginxWriteRequest is the JSON body for POST /api/sites/{domain}/nginx.
type SiteNginxWriteRequest struct {
	Content string `json:"content"`
	Backup  bool   `json:"backup"`
}

// SiteNginxWriteResponse is the JSON response for POST /api/sites/{domain}/nginx.
// ValidationOutput carries the captured `nginx -t` stdout+stderr when the
// pre-flight validation step ran (whether it passed or failed) so the modal
// can show the user exactly which directive / line nginx complained about.
// Content/Exists round-trip the canonical post-write state so the client
// can refresh its `original` baseline even on the reload-failure path
// (file already landed on disk, just the runtime reload step failed).
type SiteNginxWriteResponse struct {
	OK               bool   `json:"ok"`
	Error            string `json:"error,omitempty"`
	BackupName       string `json:"backup_name,omitempty"`
	ValidationOutput string `json:"validation_output,omitempty"`
	Content          string `json:"content,omitempty"`
	Exists           bool   `json:"exists,omitempty"`
}

// SiteNginxRestoreRequest is the JSON body for POST /api/sites/{d}/nginx/restore.
// The frontend always loads a specific backup and previews its diff before
// the user accepts, so we require the caller to name the backup it
// rendered; otherwise a concurrent save creating a NEWER backup between
// modal-open and accept would silently swap the live file with bytes the
// user never saw and consume the wrong file. Empty name means "newest"
// for tooling that doesn't have a preview UI.
type SiteNginxRestoreRequest struct {
	Name string `json:"name"`
}

// SiteNginxRestoreResponse is the JSON response for POST /api/sites/{domain}/nginx/restore.
type SiteNginxRestoreResponse struct {
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	Restored string `json:"restored,omitempty"`
	Content  string `json:"content,omitempty"`
}

// handleSiteNginx reads (GET) or saves (POST) a site's custom.d nginx override.
// The override is bind-mounted into servlo-nginx and included at the end of the
// site's server block; saving reloads nginx so the change takes effect. The
// domain is validated against the registered sites, which also blocks any path
// traversal via the {domain} segment.
func handleSiteNginx(w http.ResponseWriter, r *http.Request, domain string) {
	if _, err := config.FindSiteByDomain(domain); err != nil {
		http.Error(w, "site not found", http.StatusNotFound)
		return
	}
	if r.Method == http.MethodGet {
		got, err := siteops.ReadCustomNginx(domain)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, SiteNginxReadResponse{Path: got.Path, Content: got.Body, Exists: got.Exists})
		return
	}
	var req SiteNginxWriteRequest
	// Cap the POST body so a multi-gigabyte payload can't stream straight to
	// disk. 64 KiB matches the global nginx endpoints.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeJSON(w, SiteNginxWriteResponse{OK: false, Error: "invalid body: " + err.Error()})
		return
	}
	res, err := siteops.SaveCustomNginx(domain, req.Content, req.Backup)
	if err != nil {
		writeJSON(w, SiteNginxWriteResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, SiteNginxWriteResponse(res))
}

// handleSiteNginxBackups lists the per-site nginx override backups for the
// domain, newest first. Mirrors handleSiteEnvBackups.
func handleSiteNginxBackups(w http.ResponseWriter, r *http.Request, domain string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, err := config.FindSiteByDomain(domain); err != nil {
		http.NotFound(w, r)
		return
	}
	list, err := siteops.ListCustomNginxBackups(domain)
	if err != nil {
		http.Error(w, "listing backups: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []SiteNginxBackup{}
	}
	writeJSON(w, list)
}

// handleSiteNginxBackupContent serves the raw bytes of a single backup so the
// restore modal can show a diff before the user accepts. Mirrors the env
// backup-content handler.
func handleSiteNginxBackupContent(w http.ResponseWriter, r *http.Request, domain, name string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, err := config.FindSiteByDomain(domain); err != nil {
		http.NotFound(w, r)
		return
	}
	data, err := siteops.ReadCustomNginxBackup(domain, name)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "reading backup: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

// SiteNginxResetResponse is the JSON returned by POST /api/sites/{d}/nginx/reset.
type SiteNginxResetResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// handleSiteNginxReset deletes the per-site nginx override file so the
// generated vhost falls back to the bundled defaults (the include glob
// silently expands to nothing when no file matches). Backups are
// intentionally preserved in custom.d.bkp/ so a Restore can recover from
// an accidental reset. Skips the nginx reload when the file was already
// missing because there is genuinely nothing for nginx to re-read in
// that case and the round-trip via podman exec is wasted.
func handleSiteNginxReset(w http.ResponseWriter, r *http.Request, domain string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, err := config.FindSiteByDomain(domain); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := siteops.ResetCustomNginx(domain); err != nil {
		writeJSON(w, SiteNginxResetResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, SiteNginxResetResponse{OK: true})
}

// handleSiteNginxRestore restores a specific backup over the current
// override, reloads nginx, and only then removes the backup. Taking the
// backup name from the request body (rather than picking newest server-
// side) eliminates the preview-vs-action race: the frontend always
// loaded a specific backup's bytes and showed a diff for THAT one, so
// the server must restore the same file the user just inspected.
// Deferring the backup deletion until AFTER the reload succeeds means a
// failed reload leaves the recovery copy intact for the user to retry.
func handleSiteNginxRestore(w http.ResponseWriter, r *http.Request, domain string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, err := config.FindSiteByDomain(domain); err != nil {
		http.NotFound(w, r)
		return
	}
	var req SiteNginxRestoreRequest
	// Body is optional (empty name means newest); only a malformed envelope
	// is refused.
	if r.ContentLength > 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
			writeJSON(w, SiteNginxRestoreResponse{OK: false, Error: "invalid body: " + err.Error()})
			return
		}
	}
	res, err := siteops.RestoreCustomNginx(domain, req.Name)
	if err != nil {
		writeJSON(w, SiteNginxRestoreResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, SiteNginxRestoreResponse(res))
}

// nginxHttpTemplate seeds the global http-level override editor when no file
// exists yet. Loaded inside http{}; a servlo default of the same name is
// commented out of nginx.conf on save so nginx sees no duplicate.
const nginxHttpTemplate = `# Servlo global nginx http-level overrides.
#
# Loaded inside the http { } block. Anything you set here replaces servlo's own
# default for that directive. Servlo never overwrites this file; saving reloads
# nginx. Note client_max_body_size already defaults to 0 (unlimited).

# client_max_body_size 100m;
# gzip on;
# gzip_types text/plain application/json application/javascript text/css;
# proxy_buffers 8 16k;
# proxy_buffer_size 32k;
`

// SiteActionResponse is returned by POST /api/sites/{domain}/secure|unsecure.
type SiteActionResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	// Warning carries a change the user should know about but that did not fail
	// the action: a PHP version clamped to the framework's range, or a target
	// image that never built part of the declared extension set.
	Warning string `json:"warning,omitempty"`
}

// handlePHPExtensions reports what a PHP version's image actually carries: its
// own `php -m`, plus the declared extension/package sets measured against it.
// Reading php -m starts a container, so it is cached against the image ID in
// phpsets and only paid for when someone opens the tab.
func handlePHPExtensions(w http.ResponseWriter, r *http.Request, version string) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	cfg, err := config.LoadGlobal()
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	report, err := phpsets.ModulesReport(cfg, version)
	if err != nil {
		// The sets are still worth reporting when only php -m failed.
		writeJSON(w, map[string]any{"ok": true, "report": report, "modules_error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "report": report})
}

// phpSwitchWarning renders what the dashboard should tell the user about a PHP
// switch that succeeded but did not do exactly what they asked, so the dropdown
// never silently lands a site on an image missing its extensions.
func phpSwitchWarning(res siteops.PHPVersionResult) string {
	var parts []string
	if res.Clamped {
		parts = append(parts, fmt.Sprintf("PHP %s is outside the range this framework supports, so %s was used instead.", res.Requested, res.Version))
	}
	if res.Demoted {
		parts = append(parts, fmt.Sprintf("FrankenPHP has no image for PHP %s, so the site was switched to FPM.", res.Version))
	}
	switch {
	case res.NotInstalled:
		parts = append(parts, fmt.Sprintf("PHP %s has no image yet. Run 'servlo php:rebuild %s' to build it.", res.Version, res.Version))
	case res.Stale:
		parts = append(parts, fmt.Sprintf("The PHP %s image predates your custom extensions and packages. Run 'servlo php:rebuild %s' to bring it up to date.", res.Version, res.Version))
	case len(res.Missing) > 0:
		parts = append(parts, fmt.Sprintf("PHP %s cannot load: %s. They did not build on this version, and a rebuild will not change that.",
			res.Version, strings.Join(res.Missing, ", ")))
	}
	return strings.Join(parts, " ")
}

func handleSiteAction(w http.ResponseWriter, r *http.Request) {
	// path: /api/sites/{domain}/secure or /api/sites/{domain}/unsecure
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/sites/"), "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	domain := parts[0]
	// Commands subroutes have more than two segments
	// (/api/sites/{d}/commands and /api/sites/{d}/commands/{name}/run).
	if commandRoute(w, r, domain, parts[1:]) {
		return
	}
	if doctorRoute(w, r, domain, parts[1:]) {
		return
	}
	if tlsRoute(w, r, domain, parts[1:]) {
		return
	}
	if statsRoute(w, r, domain, parts[1:]) {
		return
	}
	if dbUserRoute(w, r, domain, parts[1:]) {
		return
	}
	if analyticsRoute(w, r, domain, parts[1:]) {
		return
	}
	if filesRoute(w, r, domain, parts[1:]) {
		return
	}
	if backupRoute(w, r, domain, parts[1:]) {
		return
	}
	if cronRoute(w, r, domain, parts[1:]) {
		return
	}
	if smtpRoute(w, r, domain, parts[1:]) {
		return
	}
	// /nginx subroutes (backups, restore) sit alongside the GET/POST on
	// /nginx. The domain validation inside each handler closes the path
	// traversal vector that the {domain} segment would otherwise open.
	if len(parts) >= 3 && parts[1] == "nginx" {
		switch parts[2] {
		case "backups":
			if len(parts) == 3 {
				handleSiteNginxBackups(w, r, domain)
				return
			}
			if len(parts) == 4 {
				handleSiteNginxBackupContent(w, r, domain, parts[3])
				return
			}
		case "restore":
			if len(parts) == 3 {
				handleSiteNginxRestore(w, r, domain)
				return
			}
		case "reset":
			if len(parts) == 3 {
				handleSiteNginxReset(w, r, domain)
				return
			}
		}
		http.NotFound(w, r)
		return
	}
	// /env subroutes (backups, restore) sit alongside the GET/PUT on /env.
	if len(parts) >= 3 && parts[1] == "env" {
		site, err := config.FindSiteByDomain(domain)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		switch parts[2] {
		case "files":
			if len(parts) == 3 {
				handleSiteEnvFiles(w, r, site)
				return
			}
		case "propose":
			if len(parts) == 3 {
				handleSiteEnvPropose(w, r, site)
				return
			}
		case "backups":
			if len(parts) == 3 {
				handleSiteEnvBackups(w, r, site)
				return
			}
			if len(parts) == 4 {
				handleSiteEnvBackupContent(w, r, site, parts[3])
				return
			}
		case "restore":
			if len(parts) == 3 {
				handleSiteEnvRestore(w, r, site)
				return
			}
		}
		http.NotFound(w, r)
		return
	}
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	action := parts[1]

	// Favicon is a GET endpoint served separately.
	if action == "favicon" {
		handleSiteFavicon(w, r)
		return
	}

	// .env file viewer is a GET endpoint served separately.
	if action == "env" {
		handleSiteEnv(w, r)
		return
	}

	// Per-site nginx override editor (GET reads, POST saves + reloads).
	if action == "nginx" && (r.Method == http.MethodGet || r.Method == http.MethodPost) {
		handleSiteNginx(w, r, domain)
		return
	}

	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}

	site, err := config.FindSiteByDomain(domain)
	if err != nil {
		writeJSON(w, SiteActionResponse{Error: "site not found: " + domain})
		return
	}

	needsReload := false
	switch action {
	case "secure", "unsecure":
		// Funnel through the shared helper so cert + .env + .servlo.yaml +
		// nginx reload + Stripe restart + LAN share refresh all stay in
		// sync with the CLI paths. SetSecured posts to this same
		// daemon's stripe:refresh / lan:refresh endpoints for the
		// dependent listeners, so the in-process Stripe and share handlers
		// run via the existing case handlers below.
		if err := siteops.SetSecured(site, action == "secure"); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "php":
		version := r.URL.Query().Get("version")
		if version == "" {
			writeJSON(w, SiteActionResponse{Error: "version parameter required"})
			return
		}
		// Funnel through the shared helper so the clamp, the .php-version and
		// .servlo.yaml pins, the FrankenPHP fallback, the quadlet and the vhost all
		// stay in sync with the CLI paths. It reloads nginx itself.
		res, err := siteops.SetSitePHPVersion(site, version)
		if err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true, Warning: phpSwitchWarning(res)})
		return
	case "php-settings":
		handleSitePHPSettings(w, r, site)
		return
	case "nginx-settings":
		handleSiteNginxSettings(w, r, site)
		return
	case "deploy":
		handleSiteDeploy(w, r, site)
		return
	case "deploy-script":
		handleSiteDeployScript(w, r, site)
		return
	case "deploy-exclude":
		handleSiteDeployExclude(w, r, site)
		return
	case "redeploy":
		handleSiteRedeploy(w, r, site)
		return
	case "deploy-history":
		handleSiteDeployHistory(w, r, site)
		return
	case "pseudo-cron":
		handleSitePseudoCron(w, r, site)
		return
	case "webhook":
		handleSiteWebhook(w, r, site)
		return
	case "node":
		version := r.URL.Query().Get("version")
		if version == "" {
			writeJSON(w, SiteActionResponse{Error: "version parameter required"})
			return
		}
		// "bun" is a JS-runtime toggle, not a Node version: pin js_runtime in
		// .servlo.yaml (preserving node_version) and re-sync host workers so the
		// dev/Vite worker switches to bun.
		if version == "bun" {
			if err := config.SetProjectJSRuntime(site.Path, "bun"); err != nil {
				writeJSON(w, SiteActionResponse{Error: "setting js_runtime: " + err.Error()})
				return
			}
			cli.RegenerateHostWorkersForSite(*site)
			writeJSON(w, SiteActionResponse{OK: true})
			return
		}
		if err := os.WriteFile(filepath.Join(site.Path, ".node-version"), []byte(version+"\n"), 0644); err != nil {
			writeJSON(w, SiteActionResponse{Error: "writing .node-version: " + err.Error()})
			return
		}
		site.NodeVersion = version
		// Picking a Node version while pinned to bun means switch back to Node,
		// so the chosen version actually runs.
		if projectJSRuntime(site.Path) == "bun" {
			_ = config.SetProjectJSRuntime(site.Path, "node")
			cli.RegenerateHostWorkersForSite(*site)
		}
	case "unlink":
		if err := cli.UnlinkSite(site.Name); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "pause":
		if err := cli.PauseSite(site.Name); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "unpause":
		if err := cli.UnpauseSite(site.Name); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "pin":
		if err := config.SetSitePinned(site.Name, true); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "unpin":
		if err := config.SetSitePinned(site.Name, false); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "restart":
		if err := cli.RestartSite(site.Name); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "rebuild":
		if err := cli.RebuildSite(site.Name); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "horizon:start":
		phpVersion := site.PHPVersion
		if detected, err := phpPkg.DetectVersion(site.Path); err == nil && detected != "" {
			phpVersion = detected
		}
		go cli.HorizonStartForSite(site.Name, site.Path, phpVersion) //nolint:errcheck
		go syncServloYAMLWorkersDelayed(site)
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "horizon:stop":
		if err := cli.HorizonStopForSite(site.Name); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		if !site.Paused {
			_ = config.SetProjectWorkers(site.Path, cli.CollectRunningWorkerNames(site))
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "horizon:reload":
		enabled := r.URL.Query().Get("enabled") == "true"
		phpVersion := site.PHPVersion
		if detected, err := phpPkg.DetectVersion(site.Path); err == nil && detected != "" {
			phpVersion = detected
		}
		if err := cli.ApplyHorizonReload(site.Name, site.Path, phpVersion, enabled); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "horizon:install-watcher":
		if err := cli.InstallChokidar(site.Path); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "octane:reload":
		enabled := r.URL.Query().Get("enabled") == "true"
		phpVersion := site.PHPVersion
		if detected, err := phpPkg.DetectVersion(site.Path); err == nil && detected != "" {
			phpVersion = detected
		}
		if err := cli.ApplyOctaneReload(site.Name, site.Path, phpVersion, enabled); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "octane:install-watcher":
		if err := cli.InstallChokidar(site.Path); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "queue:start":
		phpVersion := site.PHPVersion
		if detected, err := phpPkg.DetectVersion(site.Path); err == nil && detected != "" {
			phpVersion = detected
		}
		go cli.QueueStartForSite(site.Name, site.Path, phpVersion) //nolint:errcheck
		go syncServloYAMLWorkersDelayed(site)
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "queue:stop":
		if err := cli.QueueStopForSite(site.Name); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		if !site.Paused {
			_ = config.SetProjectWorkers(site.Path, cli.CollectRunningWorkerNames(site))
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "stripe:start":
		scheme := "http"
		if site.Secured {
			scheme = "https"
		}
		go cli.StripeStartForSite(site.Name, site.Path, scheme+"://"+site.PrimaryDomain()) //nolint:errcheck
		go syncServloYAMLWorkersDelayed(site)
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "stripe:stop":
		if err := cli.StripeStopForSite(site.Name); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		if !site.Paused {
			_ = config.SetProjectWorkers(site.Path, cli.CollectRunningWorkerNames(site))
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "stripe:config":
		path := r.URL.Query().Get("path")
		secretEnvKey := r.URL.Query().Get("secret_env_key")
		if err := config.SetProjectStripe(site.Path, path, secretEnvKey); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		// Re-forward to the new route immediately when a listener is already
		// running; no-op otherwise.
		cli.RestartStripeIfActive(site)
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "schedule:start":
		phpVersion := site.PHPVersion
		if detected, err := phpPkg.DetectVersion(site.Path); err == nil && detected != "" {
			phpVersion = detected
		}
		go cli.ScheduleStartForSite(site.Name, site.Path, phpVersion) //nolint:errcheck
		go syncServloYAMLWorkersDelayed(site)
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "schedule:stop":
		if err := cli.ScheduleStopForSite(site.Name); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		if !site.Paused {
			_ = config.SetProjectWorkers(site.Path, cli.CollectRunningWorkerNames(site))
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "reverb:start":
		phpVersion := site.PHPVersion
		if detected, err := phpPkg.DetectVersion(site.Path); err == nil && detected != "" {
			phpVersion = detected
		}
		go cli.ReverbStartForSite(site.Name, site.Path, phpVersion) //nolint:errcheck
		go syncServloYAMLWorkersDelayed(site)
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "reverb:stop":
		if err := cli.ReverbStopForSite(site.Name); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		if !site.Paused {
			_ = config.SetProjectWorkers(site.Path, cli.CollectRunningWorkerNames(site))
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "stripe:refresh":
		// Restart the Stripe listener with the current scheme/host so its
		// --forward-to flag matches reality. Used by callers that
		// can't run the systemd commands inline.
		cli.RestartStripeIfActive(site)
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "domain:add", "domain:edit", "domain:remove":
		handleSiteDomainAction(w, r, site, action)
		return
	case "group:assign":
		secondaryDomain := r.URL.Query().Get("secondary")
		label := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("label")))
		if secondaryDomain == "" || label == "" {
			writeJSON(w, SiteActionResponse{Error: "secondary and label parameters required"})
			return
		}
		secondary, secErr := config.FindSiteByDomain(secondaryDomain)
		if secErr != nil {
			writeJSON(w, SiteActionResponse{Error: "secondary site not found: " + secondaryDomain})
			return
		}
		shareDB := r.URL.Query().Get("share_db") == "1"
		if err := grouping.AssignSecondary(site, secondary, label, shareDB); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "group:set-db":
		share := r.URL.Query().Get("share") == "1"
		if err := grouping.SetSecondarySharedDB(site, share); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "group:unassign":
		if err := grouping.UnassignSecondary(site); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "group:set-label":
		label := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("label")))
		if label == "" {
			writeJSON(w, SiteActionResponse{Error: "label parameter required"})
			return
		}
		if err := grouping.SetSecondaryLabel(site, label); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	case "group:remove":
		if site.Group == "" {
			writeJSON(w, SiteActionResponse{Error: "site is not part of a group"})
			return
		}
		if err := grouping.DissolveGroup(site.Group); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		writeJSON(w, SiteActionResponse{OK: true})
		return
	default:
		// worker:{name}:start|stop.
		if strings.HasPrefix(action, "worker:") {
			parts := strings.SplitN(action, ":", 3)
			if len(parts) == 3 && (parts[2] == "start" || parts[2] == "stop") {
				workerName := parts[1]
				// A host-proxy site's dev server (the "app" worker) IS the site:
				// nothing runs behind the proxy vhost but the dev command itself.
				// Stopping just its unit leaves the vhost proxying to a now-dead
				// port, so every request 502s. Route the parent app worker's
				// start/stop through pause/unpause, which swap the vhost to the
				// paused page (and back) and keep registry state consistent so the
				// paused page's Resume button works.
				if lifecycle, ok := hostProxyAppLifecycleOp(site.IsHostProxy(), workerName, parts[2]); ok {
					var opErr error
					if lifecycle == "pause" {
						opErr = cli.PauseSite(site.Name)
					} else {
						opErr = cli.UnpauseSite(site.Name)
					}
					if opErr != nil {
						writeJSON(w, SiteActionResponse{Error: opErr.Error()})
						return
					}
					writeJSON(w, SiteActionResponse{OK: true})
					return
				}
				targetPath := site.Path
				if parts[2] == "stop" {
					// Stops orphans without a framework definition too.
					if err := cli.WorkerStopForSite(site.Name, targetPath, workerName); err != nil {
						writeJSON(w, SiteActionResponse{Error: err.Error()})
						return
					}
					if !site.Paused {
						_ = config.SetProjectWorkers(site.Path, cli.CollectRunningWorkerNames(site))
					}
				} else {
					fwN := site.Framework
					fw, ok := config.GetFrameworkForDir(fwN, targetPath)
					if !ok || fw.Workers == nil {
						writeJSON(w, SiteActionResponse{Error: "framework has no workers defined"})
						return
					}
					worker, ok := fw.Workers[workerName]
					if !ok {
						writeJSON(w, SiteActionResponse{Error: "worker " + workerName + " not defined for this framework"})
						return
					}
					phpVersion := site.PHPVersion
					if detected, err := phpPkg.DetectVersion(targetPath); err == nil && detected != "" {
						phpVersion = detected
					}
					go cli.WorkerStartForSite(site.Name, targetPath, phpVersion, workerName, worker, true) //nolint:errcheck
					go syncServloYAMLWorkersDelayed(site)
				}
				writeJSON(w, SiteActionResponse{OK: true})
				return
			}
		}
		http.NotFound(w, r)
		return
	}

	if err := config.AddSite(*site); err != nil {
		writeJSON(w, SiteActionResponse{Error: "updating site registry: " + err.Error()})
		return
	}
	if needsReload {
		if err := nginx.Reload(); err != nil {
			writeJSON(w, SiteActionResponse{Error: "reloading nginx: " + err.Error()})
			return
		}
	}
	writeJSON(w, SiteActionResponse{OK: true})
}

// hostProxyAppLifecycleOp maps a worker start/stop on a host-proxy site's parent
// dev-server worker ("app") to the site-level lifecycle op that also swaps the
// proxy vhost: "stop" -> "pause", "start" -> "unpause". It returns ok=false for
// anything that must take the normal per-worker path — a different worker or a
// non-host-proxy site. Without this, stopping the app worker leaves the proxy
// vhost pointing at the dead dev-server port and every request to the site 502s.
func hostProxyAppLifecycleOp(isHostProxy bool, workerName, op string) (string, bool) {
	if !isHostProxy || workerName != config.HostProxyWorkerName {
		return "", false
	}
	switch op {
	case "stop":
		return "pause", true
	case "start":
		return "unpause", true
	}
	return "", false
}

func handlePHPVersionAction(w http.ResponseWriter, r *http.Request) {
	// path: /api/php-versions/{version}/{remove|set-default|config|...}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/php-versions/"), "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	version, action := parts[0], parts[1]
	if !validVersion.MatchString(version) {
		http.NotFound(w, r)
		return
	}

	// A read, so it sits above the POST-only gate below.
	if action == "extensions" && len(parts) == 2 {
		handlePHPExtensions(w, r, version)
		return
	}

	// Base-image freshness. GET answers from the digest cache so a snapshot
	// rebuild never waits on the registry; POST is the manual "check for
	// updates" and reads through to it. null means nothing to report: no
	// recorded base, or a registry that could not answer.
	if action == "updates" && len(parts) == 2 {
		if r.Method == http.MethodPost {
			writeJSON(w, podman.RefreshBaseImageFreshness(version))
			return
		}
		writeJSON(w, podman.BaseImageFreshness(version))
		return
	}

	// Streaming rebuild: same work as `servlo php:rebuild <version>`, so the
	// update badge has an action behind it.
	if action == "rebuild" && len(parts) == 2 {
		handlePHPRebuild(w, r, version)
		return
	}

	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	switch action {
	case "set-default":
		cfg, err := config.LoadGlobal()
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		cfg.PHP.DefaultVersion = version
		if err := config.SaveGlobal(cfg); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "php_default": version})
	case "start":
		short := strings.ReplaceAll(version, ".", "")
		unit := "servlo-php" + short + "-fpm"
		if err := podman.StartUnit(unit); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	case "stop":
		short := strings.ReplaceAll(version, ".", "")
		unit := "servlo-php" + short + "-fpm"
		if err := podman.StopUnit(unit); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	case "remove":
		if err := teardownPHPFPM(version); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	case "ports":
		var body struct {
			Ports []string `json:"ports"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		resolved, err := podman.SetFPMPorts(version, body.Ports)
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "ports": resolved})
	default:
		http.NotFound(w, r)
	}
}

// handlePHPInstallable answers GET /api/php-installable with the supported PHP
// versions (7.4 .. 8.5) minus the ones already installed, so the UI can offer
// them in a dropdown.
func handlePHPInstallable(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, installablePHPVersions(cli.SupportedPHPVersions, fullyInstalledPHPVersions()))
}

// fullyInstalledPHPVersions returns the versions that are both registered (a
// quadlet or container exists) and have their FPM image built. A version left
// half-registered by an interrupted build (quadlet written, image missing) is
// excluded so it stays installable for repair; orphaned :local images for
// versions with no quadlet are ignored so they don't hide installable versions.
func fullyInstalledPHPVersions() []string {
	installed, _ := phpPkg.ListInstalled()
	out := []string{}
	for _, v := range installed {
		if podman.ImageExists(podman.FPMImageName(v)) {
			out = append(out, v)
		}
	}
	return out
}

// teardownPHPFPM stops and removes a PHP-FPM version's unit, quadlet and
// container, then refreshes the cache so the version list reflects the removal
// immediately. Used by the remove action and to roll back a failed install.
func teardownPHPFPM(version string) error {
	short := strings.ReplaceAll(version, ".", "")
	unit := "servlo-php" + short + "-fpm"
	_ = podman.StopUnit(unit)
	if err := podman.RemoveQuadlet(unit); err != nil {
		return err
	}
	_ = podman.DaemonReloadFn()
	// Force-remove any lingering (stopped) container and refresh the cache so the
	// follow-up version list no longer reports this version. ListInstalled reads
	// podman ps -a, so a stale snapshot would keep the tab around.
	podman.RemoveContainer(unit)
	podman.Cache.PollNow()
	return nil
}

// phpBuildInFlight guards against concurrent installs or rebuilds of the same
// version racing on the same image build and quadlet file.
var phpBuildInFlight sync.Map

// installablePHPVersions returns the supported versions that are not present in
// installed, preserving the supported order. Always a non-nil slice.
func installablePHPVersions(supported, installed []string) []string {
	have := make(map[string]bool, len(installed))
	for _, v := range installed {
		have[v] = true
	}
	out := []string{}
	for _, v := range supported {
		if !have[v] {
			out = append(out, v)
		}
	}
	return out
}

// startPHPBuildStream prepares the SSE response an image build streams over,
// returning the writer that ships build output and the emitter for the final
// `event: done` payload. ok is false when the client cannot be streamed to.
func startPHPBuildStream(w http.ResponseWriter) (*sseLineWriter, func(map[string]any), bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, nil, false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	done := func(payload map[string]any) {
		fmt.Fprintf(w, "event: done\ndata: %s\n\n", mustJSON(payload))
		flusher.Flush()
	}
	return &sseLineWriter{w: w, f: flusher}, done, true
}

// handlePHPInstall answers POST /api/php-versions/install?version=8.3 by
// building the FPM image for that version, streaming the build log as SSE and
// finishing with an `event: done` payload.
func handlePHPInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	sw, done, ok := startPHPBuildStream(w)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	version := strings.TrimSpace(r.URL.Query().Get("version"))
	if !cli.IsSupportedPHPVersion(version) {
		done(map[string]any{"ok": false, "error": "unsupported PHP version"})
		return
	}
	// Reject a second concurrent install of the same version so two clients can't
	// race on the same image build and quadlet file.
	if _, busy := phpBuildInFlight.LoadOrStore(version, struct{}{}); busy {
		done(map[string]any{"ok": false, "error": "PHP " + version + " is already installing"})
		return
	}
	defer phpBuildInFlight.Delete(version)
	// Block only when fully installed (registered with a built image); a version
	// left half-registered by an interrupted build must stay re-installable.
	if slices.Contains(fullyInstalledPHPVersions(), version) {
		done(map[string]any{"ok": false, "error": "PHP " + version + " is already installed"})
		return
	}

	start := time.Now()
	err := cli.InstallPHPVersion(version, sw)
	sw.flushTail()
	// Notify regardless of whether the client is still connected, so a user who
	// closed the modal still learns the build finished or failed.
	dispatchNotification(notificationForPHPInstall(version, start, err))
	if err != nil {
		// Roll back a half-registered version (quadlet written before the build
		// failed) so it doesn't linger as a broken, stopped tab in the UI.
		if !podman.ImageExists(podman.FPMImageName(version)) {
			_ = teardownPHPFPM(version)
		}
		done(map[string]any{"ok": false, "error": err.Error(), "version": version})
		return
	}
	// Refresh the container cache before signalling done so the client's
	// follow-up status load (and the publishAfter broadcast) report the
	// freshly-started FPM as running instead of a stale not-running snapshot.
	podman.Cache.PollNow()
	done(map[string]any{"ok": true, "version": version})
}

// handlePHPRebuild answers POST /api/php-versions/{version}/rebuild by
// force-rebuilding that version's image against the current prebuilt base and
// restarting everything running on it, streaming the build log as SSE. This is
// the action behind the update badge a republished base raises.
func handlePHPRebuild(w http.ResponseWriter, r *http.Request, version string) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	sw, done, ok := startPHPBuildStream(w)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	if !slices.Contains(fullyInstalledPHPVersions(), version) {
		done(map[string]any{"ok": false, "error": "PHP " + version + " is not installed"})
		return
	}
	// Shares the in-flight set with install: both build the same image.
	if _, busy := phpBuildInFlight.LoadOrStore(version, struct{}{}); busy {
		done(map[string]any{"ok": false, "error": "PHP " + version + " is already building"})
		return
	}
	defer phpBuildInFlight.Delete(version)

	start := time.Now()
	err := cli.RebuildPHPVersion(version, sw)
	sw.flushTail()
	dispatchNotification(notificationForPHPRebuild(version, start, err))
	if err != nil {
		done(map[string]any{"ok": false, "error": err.Error(), "version": version})
		return
	}
	podman.Cache.PollNow()
	done(map[string]any{"ok": true, "version": version})
}

func handleNodeVersionAction(w http.ResponseWriter, r *http.Request) {
	// path: /api/node-versions/{version}/{remove|set-default}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/node-versions/"), "/")
	if len(parts) != 2 || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	if !servloNode.Managed() {
		writeJSON(w, map[string]any{"ok": false, "error": "servlo is not managing Node.js"})
		return
	}
	version, action := parts[0], parts[1]
	if !validVersion.MatchString(version) {
		http.NotFound(w, r)
		return
	}
	switch action {
	case "set-default":
		if err := servloNode.Active().SetDefault(version); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		cfg, err := config.LoadGlobal()
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		cfg.Node.DefaultVersion = version
		if err := config.SaveGlobal(cfg); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "node_default": version})
	case "remove":
		// version is a major; Uninstall removes every installed full version
		// under it via the active manager.
		if err := servloNode.Active().Uninstall(version); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	default:
		http.NotFound(w, r)
	}
}

// handleNodeManage / handleNodeUnmanage opt the host into or out of
// servlo-managed Node by shelling out to the servlo binary, reusing the CLI's shim
// + worker-regeneration logic rather than duplicating it here. Synchronous:
// these can take a few seconds (Node install, worker restarts), so the UI shows
// a loading state.
func handleNodeManage(w http.ResponseWriter, r *http.Request) { runNodeMgmtCmd(w, r, "node:manage") }
func handleNodeUnmanage(w http.ResponseWriter, r *http.Request) {
	runNodeMgmtCmd(w, r, "node:unmanage")
}

// handleNodeSetManager switches the Node version manager servlo drives (fnm/nvm)
// by shelling out to `servlo node:manager <manager>`, reusing the CLI's shim +
// worker-regeneration logic rather than duplicating it here.
func handleNodeSetManager(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Manager string `json:"manager"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Manager != "fnm" && req.Manager != "nvm") {
		writeJSON(w, map[string]any{"ok": false, "error": "invalid manager"})
		return
	}
	runNodeMgmtCmd(w, r, "node:manager", req.Manager)
}

func runNodeMgmtCmd(w http.ResponseWriter, r *http.Request, sub string, extra ...string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	self, err := os.Executable()
	if err != nil || self == "" {
		self = "servlo"
	}
	if out, err := exec.Command(self, append([]string{sub}, extra...)...).CombinedOutput(); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": strings.TrimSpace(string(out))})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

var validVersion = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*$`)

func handleInstallNodeVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !servloNode.Managed() {
		writeJSON(w, map[string]any{"ok": false, "error": "servlo is not managing Node.js"})
		return
	}
	var req struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Version == "" || !validVersion.MatchString(req.Version) {
		writeJSON(w, map[string]any{"ok": false, "error": "invalid version"})
		return
	}
	version := req.Version
	major := strings.SplitN(version, ".", 2)[0]
	if err := servloNode.Active().Install(major); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// allowedContainer validates that a container name is a known servlo container.
var allowedContainer = regexp.MustCompile(`^servlo-[a-z0-9-]+$`)

func handleLogs(w http.ResponseWriter, r *http.Request) {
	container := strings.TrimPrefix(r.URL.Path, "/api/logs/")
	if !allowedContainer.MatchString(container) {
		http.Error(w, "unknown container", http.StatusNotFound)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // tell nginx not to buffer

	// Flush headers immediately so the EventSource client fires `onopen` even
	// when the container is idle and writing nothing. Without
	// this, scanner.Scan below blocks before any bytes hit the wire and the
	// browser's "live" indicator never turns on.
	_, _ = io.WriteString(w, ": connected\n\n")
	flusher.Flush()

	// If no container exists for this unit, route to the platform log stream
	// (file tail for native services) or report not-running for container units.
	if exists, _ := podman.ContainerExists(container); !exists {
		if isContainerUnit(container) {
			fmt.Fprintf(w, "data: container %s is not running\n\n", container)
			flusher.Flush()
			return
		}
		// Native service (dns, watcher, ui) — stream from log file.
		streamUnitLogs(w, r, container)
		return
	}

	tail := "100"
	if r.Header.Get("Last-Event-ID") != "" {
		tail = "0"
	}

	// Wrap r.Context() in a cancel so the worker-mode migration can kill
	// this stream pre-emptively. Otherwise its `podman logs -f` child holds
	// a gvproxy slot and races the migration's `podman rm -f` against the
	// same container, jamming the podman API socket.
	streamCtx, streamCancel := context.WithCancel(r.Context())
	defer streamCancel()
	if isFrameworkWorkerUnit(container) {
		defer logStreams.Register(container, streamCancel)()
	}

	pr, pw := io.Pipe()
	cmd := podman.CmdContext(streamCtx, "logs", "-f", "--tail", tail, container)
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(w, "data: error starting logs: %s\n\n", err.Error())
		flusher.Flush()
		return
	}

	go func() {
		cmd.Wait() //nolint:errcheck
		pw.Close()
	}()

	var lineID int
	scanner := bufio.NewScanner(pr)
	for scanner.Scan() {
		line := scanner.Text()
		// Escape backslashes and encode as a single SSE data line.
		escaped := strings.ReplaceAll(line, "\\", "\\\\")
		lineID++
		fmt.Fprintf(w, "id: %d\ndata: %s\n\n", lineID, escaped)
		flusher.Flush()
		if r.Context().Err() != nil {
			break
		}
	}
	if cmd.Process != nil {
		cmd.Process.Kill() //nolint:errcheck
	}
}

var allowedQueueUnit = regexp.MustCompile(`^[a-z0-9-]+$`)

// SettingsResponse is the response for GET /api/settings.
type SettingsResponse struct {
	AutostartOnLogin  bool   `json:"autostart_on_login"`
	WorkerExecMode    string `json:"worker_exec_mode"`
	WorkerModeApplies bool   `json:"worker_mode_applies"` // true on macOS only
}

func handleSettings(w http.ResponseWriter, _ *http.Request) {
	cfg, _ := config.LoadGlobal()
	mode := config.WorkerExecModeExec
	if cfg != nil {
		mode = cfg.WorkerExecMode()
	}
	writeJSON(w, SettingsResponse{
		AutostartOnLogin:  servloSystemd.IsAutostartEnabled(),
		WorkerExecMode:    mode,
		WorkerModeApplies: false,
	})
}

func handleSettingsWorkerMode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if body.Mode != config.WorkerExecModeExec && body.Mode != config.WorkerExecModeContainer {
		writeJSON(w, map[string]any{"ok": false, "error": "unknown mode"})
		return
	}
	// NDJSON: stream phase events so the dashboard modal can show live
	// per-worker progress instead of a 30-60s blank spinner. Each line is
	// a cli.WorkerModePhaseEvent; the client treats {"phase":"done"} as
	// success and {"phase":"error"} as failure.
	writeLine, _ := startNDJSONStream(w, r)
	if err := cli.ApplyWorkersModeStreaming(body.Mode, func(evt cli.WorkerModePhaseEvent) {
		writeLine(evt)
	}); err != nil {
		writeLine(cli.WorkerModePhaseEvent{Phase: "error", Error: err.Error()})
	}
}

// handleWorkersHealth reports every worker unit currently in the systemd
// "failed" state, grouped per site. Reads the existing batched unit-state
// cache so polling stays cheap (no extra subprocess per request); each
// entry is enriched with the last journal line so the dashboard can show
// "why did this fail?" without a drill-down.
func handleWorkersHealth(w http.ResponseWriter, _ *http.Request) {
	unhealthy, err := cli.DetectUnhealthyWorkers()
	if err != nil {
		writeJSON(w, map[string]any{"unhealthy": []cli.UnhealthyWorker{}, "error": err.Error()})
		return
	}
	if unhealthy == nil {
		unhealthy = []cli.UnhealthyWorker{}
	}
	unhealthy = workerheal.Enrich(unhealthy)
	writeJSON(w, map[string]any{"unhealthy": unhealthy})
}

// handleWorkersHeal streams NDJSON heal events to the dashboard so the
// banner can show real per-unit progress. Heal is intentionally narrow:
// reset-failed + start; no .servlo.yaml or unit-file writes.
func handleWorkersHeal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeLine, _ := startNDJSONStream(w, r)
	if _, err := cli.HealWorkers(func(evt cli.HealEvent) {
		writeLine(evt)
	}); err != nil {
		writeLine(cli.HealEvent{Phase: "failed", Error: err.Error()})
	}
}

func handleSettingsAutostart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	if err := cli.ApplyAutostart(!body.Enabled); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "autostart_on_login": body.Enabled})
}

func handleServloStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := cli.RunStart(); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func handleServloStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := cli.RunStop(); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func handleServloQuit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Respond before quitting so the browser receives the response.
	writeJSON(w, map[string]any{"ok": true})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go cli.RunQuit() //nolint:errcheck
}

// appleScriptStr returns an AppleScript string expression for s.
// AppleScript has no escape sequences; double quotes are spliced in via & quote &.
func appleScriptStr(s string) string {
	parts := strings.Split(s, `"`)
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = `"` + p + `"`
	}
	return strings.Join(quoted, " & quote & ")
}

func handleWatcherStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := servloSystemd.StartService("servlo-watcher"); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// laravelAppName labels a sites-dashboard tile by its Laravel APP_NAME instead
// of just the URL. Thin wrapper over siteinfo.LaravelAppName so the web and the
// TUI share one implementation; see there for the gating rules.
func laravelAppName(frameworkName, sitePath string) string {
	return siteinfo.LaravelAppName(frameworkName, sitePath)
}

// siteHasEnv reports whether the site root contains a .env file. Cheap,
// stat-only check used to decide whether to surface the Env tab in the UI.
// projectJSRuntime returns the .servlo.yaml js_runtime pin ("bun"/"node") for a
// site path, or "" when unset. LoadProjectConfig is cached so this is cheap.
func projectJSRuntime(sitePath string) string {
	if sitePath == "" {
		return ""
	}
	if proj, err := config.LoadProjectConfig(sitePath); err == nil && proj != nil {
		return proj.JSRuntime
	}
	return ""
}

// frameworkEnvFile resolves the dotenv file a framework actually reads, relative
// to dir (Laravel ".env", CakePHP "config/.env", Symfony ".env.local"). ok is
// false for frameworks whose env is PHP source (WordPress wp-config.php, etc.):
// those are out of scope for the flat key=value editor and get no Env tab. With
// no known framework it defaults to ".env".
func frameworkEnvFile(frameworkName, dir string) (file string, ok bool) {
	// GetFrameworkForDir (not GetFramework) so the tab resolves against the same
	// version-aware store definition the env wiring and doctor use; GetFramework
	// returns the Go built-in and ignores the per-version store yaml.
	fw, found := config.GetFrameworkForDir(frameworkName, dir)
	if !found {
		return ".env", true
	}
	file, format := fw.Env.Resolve(dir)
	if format != "dotenv" {
		return "", false
	}
	// The declared path comes from framework yaml and is joined onto dir for
	// read/write/backup; reject anything that escapes the site (absolute or
	// ../.. traversal) so a bad declaration can't reach ../../.ssh/config.
	// Symlinks are deliberately followed: a project sharing one .env through a
	// symlink is a real layout, and it is the same file the CLI, the doctor and
	// the service wiring already read.
	if !filepath.IsLocal(file) {
		return "", false
	}
	return file, true
}

func siteHasEnv(frameworkName, sitePath string) bool {
	if sitePath == "" {
		return false
	}
	file, ok := frameworkEnvFile(frameworkName, sitePath)
	if !ok {
		return false
	}
	info, err := os.Stat(filepath.Join(sitePath, file))
	return err == nil && !info.IsDir()
}

// siteHasEnvOverrides reports whether the project declares env_overrides in its
// .servlo.yaml, which servlo uses for per-tenant subdomain templating.
// The UI warns when grouping a secondary under such a main, since the chosen
// subdomain is carved out of the main's wildcard tenant space.
func siteHasEnvOverrides(sitePath string) bool {
	if sitePath == "" {
		return false
	}
	cfg, err := config.LoadProjectConfig(sitePath)
	return err == nil && cfg != nil && len(cfg.EnvOverrides) > 0
}

// handleAppLogs serves application-level log files (e.g. Laravel's storage/logs/*.log).
//
//	GET /api/app-logs/{domain}            → list available log files
//	GET /api/app-logs/{domain}/{filename} → parsed log entries
func handleAppLogs(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/app-logs/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	domain := parts[0]
	site, err := config.FindSiteByDomain(domain)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	basePath := site.Path
	if basePath == "" {
		http.NotFound(w, r)
		return
	}

	fwName := site.Framework
	if fwName == "" {
		fwName, _ = config.DetectFrameworkForDir(basePath)
	}
	fw, hasFw := config.GetFramework(fwName)
	if !hasFw || len(fw.Logs) == 0 {
		writeJSON(w, map[string]any{"files": []any{}, "entries": []any{}})
		return
	}

	// POST /api/app-logs/{domain}/clear deletes the matched log files to reclaim
	// disk. It requires dashboard-control authority.
	if len(parts) == 2 && parts[1] == "clear" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handleAppLogsClear(w, basePath, fw.Logs)
		return
	}

	if len(parts) == 1 {
		files, _ := applog.DiscoverLogFiles(basePath, fw.Logs)
		if files == nil {
			files = []applog.LogFile{}
		}
		writeJSON(w, map[string]any{"files": files})
		return
	}

	filename := parts[1]
	for _, c := range filename {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-') {
			http.NotFound(w, r)
			return
		}
	}

	fullPath := applog.ResolveLogFilePath(basePath, fw.Logs, filename)
	if fullPath == "" {
		http.NotFound(w, r)
		return
	}

	maxEntries := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if n, err := fmt.Sscanf(limitStr, "%d", &maxEntries); err != nil || n != 1 {
			maxEntries = 100
		}
		if maxEntries <= 0 {
			maxEntries = 0 // 0 means unlimited
		}
	}

	format := applog.FormatForFile(fw.Logs, filename)
	entries, err := applog.ParseFile(fullPath, format, maxEntries)
	if err != nil {
		writeJSON(w, map[string]any{"entries": []any{}, "error": err.Error()})
		return
	}
	if entries == nil {
		entries = []applog.LogEntry{}
	}
	writeJSON(w, map[string]any{"entries": entries})
}

// handleBrowse returns a listing of directories for the file browser.
func handleBrowse(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = home
	}
	dir = filepath.Clean(dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}

	type dirEntry struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	var dirs []dirEntry
	// Always include parent
	parent := filepath.Dir(dir)
	if parent != dir {
		dirs = append(dirs, dirEntry{Name: "..", Path: parent})
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dirs = append(dirs, dirEntry{Name: e.Name(), Path: filepath.Join(dir, e.Name())})
	}
	writeJSON(w, map[string]any{"current": dir, "dirs": dirs})
}

// SiteReorderRequest is the JSON body for POST /api/sites/reorder.
type SiteReorderRequest struct {
	Order []string `json:"order"` // site names in the desired display order
}

func handleSiteReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}

	var req SiteReorderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, SiteActionResponse{Error: "invalid request body"})
		return
	}
	if err := config.ReorderSites(req.Order); err != nil {
		writeJSON(w, SiteActionResponse{Error: err.Error()})
		return
	}
	writeJSON(w, SiteActionResponse{OK: true})
}

// sseLineWriter buffers writes into newline-delimited SSE `data:` frames so
// arbitrary git/composer output streams cleanly to the browser.
type sseLineWriter struct {
	w   http.ResponseWriter
	f   http.Flusher
	buf []byte
}

func (s *sseLineWriter) Write(p []byte) (int, error) {
	s.buf = append(s.buf, p...)
	for {
		i := bytes.IndexByte(s.buf, '\n')
		if i < 0 {
			break
		}
		s.emit(string(s.buf[:i]))
		s.buf = s.buf[i+1:]
	}
	return len(p), nil
}

func (s *sseLineWriter) emit(line string) {
	// SSE data lines are delimited by newlines only, so backslashes need no
	// escaping; the frontend consumers pass the payload through verbatim.
	fmt.Fprintf(s.w, "data: %s\n\n", strings.TrimRight(line, "\r"))
	s.f.Flush()
}

func (s *sseLineWriter) flushTail() {
	if len(s.buf) > 0 {
		s.emit(string(s.buf))
		s.buf = nil
	}
}

// syncServloYAMLWorkersDelayed waits briefly for the worker unit to start, then syncs.
func syncServloYAMLWorkersDelayed(site *config.Site) {
	time.Sleep(2 * time.Second)
	if !site.Paused {
		_ = config.SetProjectWorkers(site.Path, cli.CollectRunningWorkerNames(site))
	}
}
