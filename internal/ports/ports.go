// Package ports decides how nginx reaches 80 and 443 on a host where Servlo
// never runs as root.
//
// Rootless podman cannot publish a privileged port, and Servlo will not keep a
// root process around to do it (PRD 6). Two ways out, in preference order:
// lower net.ipv4.ip_unprivileged_port_start so the bind is simply allowed, or
// leave nginx on high ports and have nftables redirect the privileged ones into
// them. The first is one line and touches nothing else; the second exists for
// kernels that do not expose the sysctl at all.
//
// Nothing here executes anything. The plan is a list of commands for a human to
// read and run, because a tool that silently acquires privilege is the thing
// this design is avoiding.
package ports

import (
	"fmt"
	"os"
	"strings"
)

// Strategy names how the privileged ports are obtained.
type Strategy string

const (
	Sysctl   Strategy = "sysctl"
	NFTables Strategy = "nftables"
)

// unprivStart is the value the sysctl is lowered to. 80 rather than 0: it is
// the lowest value that admits both 80 and 443, and it leaves ssh, smtp and the
// rest of the low range privileged, which 0 would not.
const unprivStart = 80

const (
	dropInPath   = "/etc/sysctl.d/99-servlo-ports.conf"
	nftRulesPath = "/etc/nftables.d/servlo-ports.conf"
	nftMainConf  = "/etc/nftables.conf"
)

// Publish returns the host ports nginx binds under a strategy. Under the
// sysctl these are the real ones; under nftables they are the high ports the
// redirect lands on.
func Publish(s Strategy) (http, https int) {
	if s == NFTables {
		return 8080, 8443
	}
	return 80, 443
}

// ParseStrategy reads a recorded strategy back, defaulting to the sysctl for
// anything unrecognised: it is the strategy a host without a recorded choice
// was already using.
func ParseStrategy(s string) Strategy {
	if Strategy(s) == NFTables {
		return NFTables
	}
	return Sysctl
}

// Plan is the chosen strategy, the ports it implies, and exactly what a human
// has to run to put it in place. Satisfied means the host already does what the
// strategy needs and Commands is empty.
type Plan struct {
	Strategy  Strategy
	HTTPPort  int
	HTTPSPort int
	Satisfied bool
	Commands  []string
	// Reason explains the choice in one line, for the operator who is being
	// asked to run the commands.
	Reason string
}

// PlanFor chooses a strategy from what the kernel offers. current is the value
// of ip_unprivileged_port_start and available reports whether the kernel
// exposes it at all.
func PlanFor(current int, available bool) Plan {
	if !available {
		return nftablesPlan()
	}
	return sysctlPlan(current)
}

// Detect reads the live kernel and plans from it.
func Detect() Plan {
	current, available := unprivilegedPortStart()
	return PlanFor(current, available)
}

// unprivilegedPortStart reads the sysctl gating rootless binds of 80 and 443.
// ok is false on a kernel that does not expose it, where there is nothing to
// set and the fallback is the only way through.
func unprivilegedPortStart() (int, bool) {
	data, err := os.ReadFile("/proc/sys/net/ipv4/ip_unprivileged_port_start")
	if err != nil {
		return 0, false
	}
	val := 1024
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &val); err != nil {
		return 0, false
	}
	return val, true
}

func sysctlPlan(current int) Plan {
	http, https := Publish(Sysctl)
	p := Plan{
		Strategy:  Sysctl,
		HTTPPort:  http,
		HTTPSPort: https,
		Reason:    fmt.Sprintf("lowering ip_unprivileged_port_start to %d lets rootless nginx bind %d and %d directly", unprivStart, http, https),
	}
	if current <= unprivStart {
		p.Satisfied = true
		p.Reason = fmt.Sprintf("ip_unprivileged_port_start is already %d, so rootless nginx can bind %d and %d", current, http, https)
		return p
	}
	setting := fmt.Sprintf("net.ipv4.ip_unprivileged_port_start=%d", unprivStart)
	// Two steps on purpose: -w takes effect now but is lost at reboot, and the
	// drop-in survives a reboot but does nothing until one.
	p.Commands = []string{
		"sudo sysctl -w " + setting,
		fmt.Sprintf("sudo sh -c 'echo %s > %s'", setting, dropInPath),
	}
	return p
}

func nftablesPlan() Plan {
	http, https := Publish(NFTables)
	// The output chain matters as much as prerouting: traffic the droplet
	// originates to its own site never passes prerouting, so without it a local
	// curl to port 80 reaches nothing.
	rules := fmt.Sprintf(`table inet servlo {
  chain prerouting {
    type nat hook prerouting priority dstnat; policy accept;
    tcp dport 80 redirect to :%d
    tcp dport 443 redirect to :%d
  }
  chain output {
    type nat hook output priority -100; policy accept;
    ip daddr 127.0.0.1 tcp dport 80 redirect to :%d
    ip daddr 127.0.0.1 tcp dport 443 redirect to :%d
  }
}`, http, https, http, https)

	return Plan{
		Strategy:  NFTables,
		HTTPPort:  http,
		HTTPSPort: https,
		Reason:    fmt.Sprintf("this kernel does not expose ip_unprivileged_port_start, so nginx stays on %d and %d and nftables redirects 80 and 443 into them", http, https),
		Commands: []string{
			fmt.Sprintf("sudo mkdir -p %s", dirOf(nftRulesPath)),
			fmt.Sprintf("sudo tee %s >/dev/null <<'SERVLO'\n%s\nSERVLO", nftRulesPath, rules),
			// The include is what makes the rules come back at boot; nftables.service
			// only ever reads the main file.
			fmt.Sprintf("sudo sh -c 'grep -q %s %s || echo '\\''include \"%s\"'\\'' >> %s'", nftRulesPath, nftMainConf, nftRulesPath, nftMainConf),
			"sudo systemctl enable --now nftables",
			fmt.Sprintf("sudo nft -f %s", nftRulesPath),
		},
	}
}

func dirOf(path string) string {
	if i := strings.LastIndex(path, "/"); i > 0 {
		return path[:i]
	}
	return path
}
