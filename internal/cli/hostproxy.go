package cli

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/geodro/lerd/internal/config"
)

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

// buildHostProxyCommand prefixes the dev command with `env KEY=port` so the app
// binds the port nginx proxies to. The `env` utility (not a bare `KEY=value`
// assignment) is used because host workers exec the command both through a
// shell (macOS) and directly via `fnm exec --` (Linux); `env` is a real
// executable that works in both. Returns "" in proxy-only mode (no command).
func buildHostProxyCommand(proxy *config.ProxyConfig) string {
	if proxy.Command == "" {
		return ""
	}
	return fmt.Sprintf("env %s=%d %s", hostProxyPortEnvKey(proxy), proxy.Port, proxy.Command)
}

// hostProxyWorker builds the supervised dev-server worker for a host-proxy
// site. ok is false in proxy-only mode (no command), in which case lerd
// supervises nothing and only wires the proxy.
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

// firstFreePort returns the first port at or above start for which isTaken is
// false. Pure (isTaken is injected) so the search logic is unit-testable
// without binding real sockets. Falls back to start if nothing is free.
func firstFreePort(start int, isTaken func(int) bool) int {
	if start < 1 {
		start = 1
	}
	for p := start; p <= 65535; p++ {
		if !isTaken(p) {
			return p
		}
	}
	return start
}

// portBoundOnHost reports whether something is already listening on the host's
// loopback at port p. Used as the live half of host-port allocation so a dev
// server isn't assigned a port a lerd service (or any process) already holds.
// Both IPv4 and IPv6 loopback are probed so a service bound only to [::1] (as
// some lerd quadlets are) is still detected as taken.
func portBoundOnHost(p int) bool {
	for _, host := range []string{"127.0.0.1", "::1"} {
		ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(p)))
		if err != nil {
			return true
		}
		_ = ln.Close()
	}
	return false
}

// reservedHostPorts returns host ports already claimed by other host-proxy
// sites in the registry, so two sites never get assigned the same port even
// when the other site's dev server isn't currently running. exceptSite is
// skipped so re-running init on a site keeps its own port.
func reservedHostPorts(exceptSite string) map[int]bool {
	out := map[int]bool{}
	reg, err := config.LoadSites()
	if err != nil {
		return out
	}
	for _, s := range reg.Sites {
		if s.Name == exceptSite || s.HostPort == 0 {
			continue
		}
		out[s.HostPort] = true
	}
	return out
}

// allocateHostPort picks a free host port for a dev server, starting from the
// tool's conventional default and walking up past anything another host-proxy
// site reserves or any process currently binds (e.g. lerd-gotenberg on 3000).
func allocateHostPort(start int, exceptSite string) int {
	reserved := reservedHostPorts(exceptSite)
	return firstFreePort(start, func(p int) bool {
		return reserved[p] || portBoundOnHost(p)
	})
}

// startHostProxyWorker supervises the dev command for a host-proxy site as a
// host-mode worker (launchd/fnm on macOS), reusing the standard worker
// machinery for auto-restart, logs, and health. No-op in proxy-only mode.
func startHostProxyWorker(site config.Site, proxy *config.ProxyConfig) {
	w, ok := hostProxyWorker(proxy)
	if !ok {
		return
	}
	if err := WorkerStartForSite(site.Name, site.Path, "", hostProxyWorkerName, w, false); err != nil {
		fmt.Printf("[WARN] starting dev server: %v\n", err)
	}
}
