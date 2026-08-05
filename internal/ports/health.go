package ports

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Probes the health check makes, as vars so tests can drive a host that does
// not exist. Each is deliberately something an unprivileged user can read:
// doctor runs as the operator, not as root, so "nft list table" is not
// available to it and the persistence checks read configuration instead.
var (
	liveUnprivStart = unprivilegedPortStart
	readTextFile    = os.ReadFile
	systemdUnitIs   = defaultSystemdUnitIs
)

func defaultSystemdUnitIs(unit, verb string) bool {
	out, err := exec.Command("systemctl", verb, unit).Output()
	if err != nil {
		// is-enabled and is-active both exit non-zero for the negative answer,
		// so the status alone cannot distinguish "no" from "systemctl missing".
		// The word it printed can, and an empty one reads as no.
		return strings.TrimSpace(string(out)) == "enabled" || strings.TrimSpace(string(out)) == "active"
	}
	word := strings.TrimSpace(string(out))
	return word == "enabled" || word == "active"
}

// Health is the state of a strategy on this host, split into the two halves
// that fail independently.
//
// Live is whether nginx can bind its ports right now. Persistent is whether it
// will still be able to after a reboot. They come apart in both directions, and
// the dangerous one is Live without Persistent: a `sysctl -w` with no drop-in,
// or nftables rules loaded by hand with no include, serves perfectly until the
// machine restarts and then silently stops. That is the state this check exists
// to catch while someone is still looking.
type Health struct {
	Strategy   Strategy
	Live       bool
	Persistent bool
	Detail     string
	Fix        []string
}

// Healthy reports whether the strategy holds both now and across a reboot.
func (h Health) Healthy() bool { return h.Live && h.Persistent }

// CheckHealth inspects the host against the strategy recorded at install.
func CheckHealth(s Strategy) Health {
	if s == NFTables {
		return nftablesHealth()
	}
	return sysctlHealth()
}

func sysctlHealth() Health {
	h := Health{Strategy: Sysctl}

	current, available := liveUnprivStart()
	h.Live = available && current <= unprivStart

	if body, err := readTextFile(dropInPath); err == nil {
		h.Persistent = dropInAllows(string(body))
	}

	setting := fmt.Sprintf("net.ipv4.ip_unprivileged_port_start=%d", unprivStart)
	switch {
	case h.Healthy():
		h.Detail = fmt.Sprintf("ip_unprivileged_port_start is %d live and pinned in %s", current, dropInPath)
	case h.Live && !h.Persistent:
		h.Detail = fmt.Sprintf("set live but not pinned in %s, so nginx loses ports 80 and 443 at the next reboot", dropInPath)
		h.Fix = []string{fmt.Sprintf("sudo sh -c 'echo %s > %s'", setting, dropInPath)}
	case !h.Live && h.Persistent:
		h.Detail = fmt.Sprintf("pinned in %s but the running kernel is still at %d, so nginx cannot bind until it is applied", dropInPath, current)
		h.Fix = []string{"sudo sysctl -w " + setting}
	case !available:
		h.Detail = "this kernel does not expose ip_unprivileged_port_start, so the sysctl strategy cannot work here"
		h.Fix = nftablesPlan().Commands
	default:
		h.Detail = fmt.Sprintf("ip_unprivileged_port_start is %d, so rootless nginx cannot bind 80 or 443", current)
		h.Fix = sysctlPlan(current).Commands
	}
	return h
}

// dropInAllows reads the pinned value back rather than looking for the literal
// line servlo writes. A drop-in edited to a value that no longer admits 443 is
// worse than a missing one, because it looks present.
func dropInAllows(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "net.ipv4.ip_unprivileged_port_start") {
			continue
		}
		_, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			continue
		}
		return n <= unprivStart
	}
	return false
}

func nftablesHealth() Health {
	h := Health{Strategy: NFTables}

	_, rulesErr := readTextFile(nftRulesPath)
	rulesPresent := rulesErr == nil

	included := false
	if body, err := readTextFile(nftMainConf); err == nil {
		included = strings.Contains(string(body), nftRulesPath)
	}
	enabled := systemdUnitIs("nftables", "is-enabled")

	// Live is the unit actually running with rules on disk. Reading the loaded
	// ruleset back would need root, which doctor does not have.
	h.Live = rulesPresent && systemdUnitIs("nftables", "is-active")
	h.Persistent = rulesPresent && included && enabled

	switch {
	case h.Healthy():
		http, https := Publish(NFTables)
		h.Detail = fmt.Sprintf("80 and 443 redirect to %d and %d, restored at boot from %s", http, https, nftRulesPath)
	case !rulesPresent:
		h.Detail = fmt.Sprintf("%s is missing, so nothing redirects 80 and 443", nftRulesPath)
		h.Fix = nftablesPlan().Commands
	case !included:
		h.Detail = fmt.Sprintf("%s does not include %s, so the redirect is lost at the next reboot", nftMainConf, nftRulesPath)
		h.Fix = nftablesPlan().Commands[2:]
	case !enabled:
		h.Detail = "nftables.service is disabled, so the redirect is lost at the next reboot"
		h.Fix = []string{"sudo systemctl enable --now nftables"}
	default:
		h.Detail = "nftables.service is not running, so the redirect is not in effect"
		h.Fix = []string{"sudo systemctl start nftables", fmt.Sprintf("sudo nft -f %s", nftRulesPath)}
	}
	return h
}
