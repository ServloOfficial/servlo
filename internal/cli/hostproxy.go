package cli

import (
	"encoding/json"
	"fmt"
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
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

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

// devTool identifies the dev-server tool from dependencies, so the wizard can
// pick a sensible default port and the right port flag.
func (m *packageManifest) devTool() string {
	if m == nil {
		return ""
	}
	has := func(name string) bool {
		_, a := m.Dependencies[name]
		_, b := m.DevDependencies[name]
		return a || b
	}
	switch {
	case has("@angular/core") || has("@angular/cli"):
		return "angular"
	case has("vite"):
		return "vite"
	case has("@nestjs/core"):
		return "nest"
	}
	return ""
}

// AvailableDevScripts returns the dev-server scripts present in the project's
// package.json, in preference order, each rendered as "npm run <name>".
func AvailableDevScripts(cwd string) []string {
	return readPackageManifest(cwd).devScripts()
}

// detectDevTool inspects package.json dependencies to identify the dev-server
// tool, so the wizard can pick a sensible default port and the right port flag.
func detectDevTool(cwd string) string {
	return readPackageManifest(cwd).devTool()
}

// defaultDevPort returns the conventional dev-server port for a tool.
func defaultDevPort(tool string) int {
	switch tool {
	case "vite":
		return 5173
	case "angular":
		return 4200
	default:
		return 3000
	}
}

var portFlagRe = regexp.MustCompile(`(?:--port[ =]|PORT=)(\d+)`)

// portFromCommand extracts an explicit port from a command string, or 0 if none.
func portFromCommand(command string) int {
	m := portFlagRe.FindStringSubmatch(command)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// appendPortFlag adds the port flag for tools that won't honour the PORT env
// var on their own. Vite needs --strictPort or it silently moves to another
// port and breaks the proxy. A command that already names a port is left alone.
func appendPortFlag(command, tool string, port int) string {
	if portFromCommand(command) != 0 {
		return command
	}
	switch tool {
	case "vite":
		return fmt.Sprintf("%s -- --port %d --strictPort", command, port)
	case "angular":
		return fmt.Sprintf("%s -- --port %d", command, port)
	}
	return command
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
