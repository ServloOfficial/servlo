package cli

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/feedback"
	"github.com/realrashid/servlo/internal/freeport"
	"github.com/realrashid/servlo/internal/linker"
	"github.com/realrashid/servlo/internal/nginx"
	"github.com/realrashid/servlo/internal/podman"
	"github.com/realrashid/servlo/internal/siteops"
)

// RegenerateHostProxyVhostsOnGatewayChange rewrites every host-proxy site's
// nginx vhost so the host-gateway IP baked into proxy_pass (Linux only) tracks a
// network change, then reloads nginx. Wired to the host-gateway watcher from
// main. No-op on macOS, where the upstream is the gvproxy-resolved
// host.containers.internal hostname rather than a literal IP.
func RegenerateHostProxyVhostsOnGatewayChange() {
	reg, err := config.LoadSites()
	if err != nil {
		return
	}
	regenerated := false
	for i := range reg.Sites {
		s := reg.Sites[i]
		if !s.IsHostProxy() {
			continue
		}
		if err := siteops.RegenerateSiteVhost(&s, s.PrimaryDomain()); err != nil {
			feedback.Warn("regenerating host-proxy vhost for %s: %v", s.Name, err)
			continue
		}
		proxy := parentProxyConfig(s)
		if proxy != nil {
			if w, ok := hostProxyWorker(proxy); ok {
				rebindHostProxyDevServer(proxy, s.Name, s.Path, w)
			}
		}
		regenerated = true
	}
	if regenerated {
		_ = nginx.Reload()
	}
}

// rebindHostProxyDevServer rewrites a host-proxy dev server's unit with the
// current host-gateway bind address and restarts it so a network change can't
// strand it on a stale IP. It only touches a server that is already running and
// whose bind servlo injects (inject_host:false servers manage their own bind), and
// it restarts only when the rewritten unit actually changed, so an unchanged bind
// never churns a running server. This is a rewrite-and-restart rather than the
// full start path, which would tear down conflicts and emit a spurious "starting".
func rebindHostProxyDevServer(proxy *config.ProxyConfig, siteName, sitePath string, w config.FrameworkWorker) {
	if !hostProxyShouldBind(proxy) {
		return
	}
	unitName, unitSiteName := workerNames(siteName, sitePath, hostProxyWorkerName)
	if !isServiceActiveOrRestarting(unitName) {
		return
	}
	label := w.Label
	if label == "" {
		label = hostProxyWorkerName
	}
	restart := resolveWorkerRestart(w.Restart)
	command := resolveWorkerCommand(sitePath, hostProxyWorkerName, w)
	fpmUnit := resolveWorkerFPMUnit(siteName, "")
	changed, err := writeWorkerUnitFile(unitName, label, unitSiteName, sitePath, "", command, restart, w.Schedule, fpmUnit, w.Host)
	if err != nil {
		feedback.Warn("rebinding dev server for %s: %v", siteName, err)
		return
	}
	if !changed {
		return // bind unchanged — leave the running server alone
	}
	if err := podman.DaemonReloadFn(); err != nil {
		feedback.Warn("daemon-reload for %s: %v", siteName, err)
	}
	if err := restartDevServer(unitName, proxy.Port, hostProxyRebindTimeout); err != nil {
		feedback.Warn("restarting dev server for %s: %v", siteName, err)
	}
}

// hostProxyWorkerName is the stable worker name for a host-proxy site's
// supervised dev server. Aliases the shared config constant so the unit name
// has a single source of truth (see config.HostProxyWorkerUnit).
const hostProxyWorkerName = config.HostProxyWorkerName

// hostProxyPortEnvKey returns the environment variable the port is injected
// as, defaulting to PORT (honoured by NestJS, Next, Nuxt, and most Node
// servers).
func hostProxyPortEnvKey(proxy *config.ProxyConfig) string {
	if proxy.PortEnvKey != "" {
		return proxy.PortEnvKey
	}
	return "PORT"
}

// hostProxyHostEnvKey returns the environment variable the bind address is
// injected as, defaulting to HOST (honoured by Nuxt, NestJS, and most Node
// servers). Set host_env_key to e.g. HOSTNAME for a Next.js standalone server.
func hostProxyHostEnvKey(proxy *config.ProxyConfig) string {
	if proxy.HostEnvKey != "" {
		return proxy.HostEnvKey
	}
	return "HOST"
}

// hostProxyShouldBind reports whether servlo injects the bind-address env
// (e.g. HOST=0.0.0.0). Defaults to true; a project sets `inject_host: false` to
// opt out entirely, for a dev server that reads HOST for something else or
// manages its own bind. The port injection is unaffected.
func hostProxyShouldBind(proxy *config.ProxyConfig) bool {
	return proxy.InjectHost == nil || *proxy.InjectHost
}

// hostGatewayBindIP resolves the host-gateway IP a host-proxy dev server should
// bind. It is the same address nginx proxies to, so binding it keeps the dev
// server reachable from the container while off every other interface. A package
// var so tests can pin it.
var hostGatewayBindIP = podman.ReadHostGatewayFromFile

// hostIPIsLocal reports whether an IP is assigned to a local interface (so a dev
// server can actually bind it). A package var so tests can pin it.
var hostIPIsLocal = isLocalInterfaceIP

// hostProxyBindAddr is the address the dev server must bind so the in-container
// nginx can reach it. On Linux that is the routable host-gateway IP, which keeps
// the dev server off other interfaces; but only when that IP is a local interface
// we can bind. A gateway that isn't local (slirp4netns 10.0.2.2, the 169.254.1.2
// fallback), an unknown gateway, or macOS (gvproxy) all fall back to 0.0.0.0,
// which always binds. Env-only: pure Vite reads --host, not this var.
func hostProxyBindAddr() string {
	if ip := hostGatewayBindIP(); ip != "" && hostIPIsLocal(ip) {
		return ip
	}
	return "0.0.0.0"
}

// isLocalInterfaceIP reports whether ip is assigned to one of the host's network
// interfaces, so binding it won't fail with "cannot assign requested address".
func isLocalInterfaceIP(ip string) bool {
	target := net.ParseIP(ip)
	if target == nil {
		return false
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.Equal(target) {
			return true
		}
	}
	return false
}

// buildHostProxyCommandPort prefixes the dev command with `env PORT=port
// HOST=<bind>` so the app binds the port nginx proxies to, on the interface the
// proxy container reaches it by. The `env` utility (not a bare `KEY=value`
// assignment) is used because host workers exec the command both through a
// shell (macOS) and directly via `fnm exec --` (Linux); `env` is a real
// executable that works in both. A HOST the user sets later in their own
// command still wins (env evaluates left to right). The bind injection is
// suppressed when `inject_host: false`, leaving only the port. Returns "" in
// proxy-only mode (no command).
func buildHostProxyCommandPort(proxy *config.ProxyConfig, port int) string {
	if proxy.Command == "" {
		return ""
	}
	if !hostProxyShouldBind(proxy) {
		return fmt.Sprintf("env %s=%d %s",
			hostProxyPortEnvKey(proxy), port, proxy.Command)
	}
	return fmt.Sprintf("env %s=%d %s=%s %s",
		hostProxyPortEnvKey(proxy), port,
		hostProxyHostEnvKey(proxy), hostProxyBindAddr(),
		proxy.Command)
}

func buildHostProxyCommand(proxy *config.ProxyConfig) string {
	return buildHostProxyCommandPort(proxy, proxy.Port)
}

// hostProxyWorker builds the supervised dev-server worker for a host-proxy
// site on its configured port. ok is false in proxy-only mode (no command).
func hostProxyWorker(proxy *config.ProxyConfig) (config.FrameworkWorker, bool) {
	command := buildHostProxyCommand(proxy)
	if command == "" {
		return config.FrameworkWorker{}, false
	}
	return config.FrameworkWorker{
		Label:   "Dev Server",
		Command: command,
		Restart: "always",
		Host:    true,
	}, true
}

// hostProxyWorkerUnit returns the worker unit name for a host-proxy site.
func hostProxyWorkerUnit(siteName string) string {
	return config.HostProxyWorkerUnit(siteName)
}

// devScriptCandidates are the package.json scripts a host-proxy site might run
// as its dev server, in the order the wizard prefers them.
var devScriptCandidates = []string{"start:dev", "dev", "serve", "start"}

// packageManifest is the slice of package.json the host-proxy wizard reads.
type packageManifest struct {
	Scripts map[string]string `json:"scripts"`
}

// defaultDevServerPort is where host-port allocation starts when the command
// doesn't name a port; the allocator walks up from here to the first free port.
const defaultDevServerPort = 3000

// readPackageManifest parses package.json once; nil if absent or invalid. The
// methods below are nil-safe so callers don't have to branch.
func readPackageManifest(cwd string) *packageManifest {
	data, err := os.ReadFile(filepath.Join(cwd, "package.json"))
	if err != nil {
		return nil
	}
	var m packageManifest
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	return &m
}

// devScripts returns the present dev-server scripts in preference order, each
// rendered as "npm run <name>".
func (m *packageManifest) devScripts() []string {
	if m == nil {
		return nil
	}
	var out []string
	for _, c := range devScriptCandidates {
		if _, ok := m.Scripts[c]; ok {
			out = append(out, "npm run "+c)
		}
	}
	return out
}

// AvailableDevScripts returns the dev-server scripts present in the project's
// package.json, in preference order, each rendered as "npm run <name>".
func AvailableDevScripts(cwd string) []string {
	return readPackageManifest(cwd).devScripts()
}

// runsVite reports whether the wizard's chosen dev command launches Vite,
// resolving "npm run <script>" against package.json so the allowedHosts note
// only prints for Vite projects. A custom command naming vite counts too.
func (m *packageManifest) runsVite(command string) bool {
	if strings.Contains(command, "vite") {
		return true
	}
	if name, ok := strings.CutPrefix(command, "npm run "); ok && m != nil {
		return strings.Contains(m.Scripts[name], "vite")
	}
	return false
}

var portFlagRe = regexp.MustCompile(`(?:--port[ =]|PORT=)(\d+)`)

// portFromCommand extracts an explicit port from a command string, or 0 if none.
// A dev command that already names a port keeps it; otherwise the port is
// auto-assigned and injected via the PORT env var.
func portFromCommand(command string) int {
	m := portFlagRe.FindStringSubmatch(command)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// isNodeProject reports whether dir is a Node project, used to decide whether a
// host worker's command runs through fnm (Node) or directly (host-proxy sites in
// Python, Ruby, Go, and other languages). A package.json, .nvmrc, or
// .node-version all count; none of these exist in a non-Node project.
func isNodeProject(dir string) bool {
	for _, f := range []string{"package.json", ".nvmrc", ".node-version"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return true
		}
	}
	return false
}

// reservedHostPorts returns host ports already claimed by other host-proxy
// sites in the registry, so two sites never get assigned the same port even
// when the other site's dev server isn't currently running. exceptSite is
// skipped so re-running init on a site keeps its own port.
func reservedHostPorts(exceptSite string) map[int]bool {
	out := map[int]bool{}
	exceptPort := 0
	if reg, err := config.LoadSites(); err == nil {
		for _, s := range reg.Sites {
			if s.Name == exceptSite {
				exceptPort = s.HostPort
				continue
			}
			if s.HostPort != 0 {
				out[s.HostPort] = true
			}
		}
	}
	// Reserve host ports servlo services publish (e.g. gotenberg on 3000) even when
	// the container is stopped: a stopped service still owns its published port
	// and would collide the moment it starts, which a bind probe can't foresee.
	for p := range servloServiceHostPorts() {
		out[p] = true
	}
	// Honour exceptSite across the whole set: re-running init on a site keeps its
	// own already-assigned port even when it coincides with a service's port, so
	// allocation stays idempotent instead of silently bumping the site.
	if exceptPort > 0 {
		delete(out, exceptPort)
	}
	return out
}

// servloServiceHostPorts returns every host port a servlo service may publish:
// installed/default services (with their resolved ports), all bundled presets
// (including optional ones like gotenberg that aren't in the default set), and
// installed custom services. It delegates to config.ReservedHostPorts, the single
// shared definition the serviceops port-ownership guard consumes too, so a
// host-proxy dev server is never assigned a port a service will reclaim and the
// two reserved sets can't drift.
func servloServiceHostPorts() map[int]bool {
	return config.ReservedHostPorts()
}

// allocateHostPort picks a free host port for a dev server, starting from the
// tool's conventional default and walking up past anything another host-proxy
// site reserves or any process currently binds (e.g. servlo-gotenberg on 3000).
func allocateHostPort(start int, exceptSite string) int {
	reserved := reservedHostPorts(exceptSite)
	// freeport.FirstFree returns 0 when nothing in range is free; preserve this
	// allocator's long-standing fall-back-to-start behaviour in that case.
	if p := freeport.FirstFree(start, func(p int) bool {
		return reserved[p] || !freeport.Bindable(p)
	}); p > 0 {
		return p
	}
	return start
}

// parentProxyConfig returns a host-proxy site's proxy config. It prefers the
// committed .servlo.yaml proxy block, falling back to the fields persisted on
// the registered Site.
func parentProxyConfig(site config.Site) *config.ProxyConfig {
	if proj, err := config.LoadProjectConfig(site.Path); err == nil && proj.Proxy != nil {
		return proj.Proxy
	}
	if site.HostCommand == "" && site.HostPort == 0 {
		return nil
	}
	return &config.ProxyConfig{Command: site.HostCommand, Port: site.HostPort, SSL: site.HostSSL}
}

// startHostProxyWorker supervises the dev command for a host-proxy site as a
// host-mode worker, reusing the standard worker
// machinery for auto-restart, logs, and health. No-op in proxy-only mode.
func startHostProxyWorker(site config.Site, proxy *config.ProxyConfig) {
	w, ok := hostProxyWorker(proxy)
	if !ok {
		return
	}
	if err := gateHostProxyAutostart(site, proxy.Command); err != nil {
		feedback.Warn("dev server not started: %v", err)
		return
	}
	if err := WorkerStartForSite(site.Name, site.Path, "", hostProxyWorkerName, w, false); err != nil {
		feedback.Warn("starting dev server: %v", err)
	}
}

// gateHostProxyAutostart authorises auto-(re)starting a host-proxy dev command
// on a non-link path (watcher, unpause): the command must match the one approved
// at link and host_proxy.disabled must be off, else it is refused unattended.
func gateHostProxyAutostart(site config.Site, command string) error {
	approved := command != "" && command == site.HostCommand
	return approveHostProxyCommand(site.Name, command, approved)
}

// hostProxyPreApproved lets the init wizard mark the command the user just chose
// as approved, so the link it triggers doesn't prompt again for the same command.
var hostProxyPreApproved bool

// hostProxyApproved folds the init wizard's pre-approval into the caller's
// approved flag, so the link the wizard triggers doesn't ask again about the
// command the user just chose.
func hostProxyApproved(approved bool) bool { return approved || hostProxyPreApproved }

// approveHostProxyCommand enforces the consent gates before servlo supervises a
// dev-server command on the host: the global disable switch and an interactive
// confirmation of the exact command. approved short-circuits the prompt when the
// command already matches the registry-approved one or the caller passed --yes.
func approveHostProxyCommand(siteName, command string, approved bool) error {
	gcfg, _ := config.LoadGlobal()
	proceed, prompt, reason := linker.HostProxyGate(command, gcfg.HostProxy.Disabled, gcfg.HostProxy.SkipConfirmation, hostProxyApproved(approved), isInteractive())
	if proceed {
		return nil
	}
	if !prompt {
		return fmt.Errorf("host-proxy %s: %s", siteName, reason)
	}
	fmt.Printf("\nservlo supervises this dev-server command on your host, outside any container:\n\n  %s\n", command)
	if !promptConfirm(fmt.Sprintf("Start and auto-restart it for %s?", siteName)) {
		return fmt.Errorf("host-proxy setup declined for %s", siteName)
	}
	return nil
}
