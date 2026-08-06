package serverbasics

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ── Timezone ──────────────────────────────────────────────────────────────────

// utcNames are the spellings that all mean UTC. Some images report Etc/UTC and
// some Universal; treating either as a mismatch would have doctor nagging
// forever about a machine that is already correct.
var utcNames = map[string]bool{"UTC": true, "Etc/UTC": true, "Universal": true, "Etc/Universal": true}

// planTimezone puts the machine on UTC.
//
// UTC is right for a server even when its operator is not in it. Logs, cron
// schedules, backup timestamps and certificate expiry all get compared across
// machines and against a certificate authority, and a local timezone turns every
// one of those comparisons into a conversion somebody has to remember to do.
// It also removes daylight saving, which otherwise makes one hour a year happen
// twice and another never, to whatever runs in it.
func planTimezone(current string) Plan {
	p := Plan{Name: "timezone"}
	if utcNames[current] {
		p.Satisfied = true
		p.Detail = "already UTC"
		return p
	}
	if current == "" {
		p.Detail = "could not read the system timezone; setting UTC anyway"
	} else {
		p.Detail = fmt.Sprintf("currently %s; a server is easier to reason about on UTC", current)
	}
	p.Commands = []string{"timedatectl set-timezone UTC"}
	return p
}

// TimezonePlan reads the live timezone and plans from it.
func TimezonePlan() Plan { return planTimezone(currentTimezone()) }

// currentTimezone reads the configured zone. /etc/timezone is the Debian family
// file and is a plain read, unlike timedatectl which needs a working dbus.
var currentTimezone = func() string {
	if data, err := os.ReadFile("/etc/timezone"); err == nil {
		return strings.TrimSpace(string(data))
	}
	// The symlink is the systemd-native answer and survives an image that has
	// no /etc/timezone.
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if _, zone, ok := strings.Cut(target, "zoneinfo/"); ok {
			return zone
		}
	}
	return ""
}

// ── Unattended security upgrades ──────────────────────────────────────────────

const autoUpgradesPath = "/etc/apt/apt.conf.d/20auto-upgrades"

// planUnattendedUpgrades turns on automatic security updates.
//
// Security updates only, and no automatic reboot. Pulling every update means a
// package changing behaviour under a running site with nobody watching, which is
// a worse failure than being a week behind on a non-security release. And a site
// going down at 3am because a kernel landed is not an improvement over a pending
// reboot somebody applies deliberately.
func planUnattendedUpgrades(configured bool) Plan {
	p := Plan{Name: "unattended security updates"}
	if configured {
		p.Satisfied = true
		p.Detail = "already enabled"
		return p
	}
	p.Detail = "not enabled; security updates would have to be applied by hand"
	p.Commands = []string{
		"apt-get install -y unattended-upgrades",
		fmt.Sprintf("printf 'APT::Periodic::Update-Package-Lists \"1\";\\nAPT::Periodic::Unattended-Upgrade \"1\";\\n' > %s", autoUpgradesPath),
	}
	return p
}

// UnattendedUpgradesPlan reads the host and plans from it.
func UnattendedUpgradesPlan() Plan { return planUnattendedUpgrades(unattendedUpgradesConfigured()) }

// unattendedUpgradesConfigured reports whether the periodic unattended upgrade
// is switched on. The drop-in existing is not enough: Ubuntu ships one with the
// value set to "0" on some images, which reads as configured and does nothing.
var unattendedUpgradesConfigured = func() bool {
	data, err := os.ReadFile(autoUpgradesPath)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "APT::Periodic::Unattended-Upgrade") {
			return strings.Contains(line, `"1"`)
		}
	}
	return false
}

// ── fail2ban ──────────────────────────────────────────────────────────────────

const fail2banJailPath = "/etc/fail2ban/jail.d/servlo-sshd.conf"

// planFail2ban puts fail2ban in front of SSH.
//
// This is the whole of servlo's SSH hardening, deliberately. It bans an address
// that fails repeatedly and leaves every legitimate login route exactly as the
// operator set it up. Password authentication is never touched: a droplet whose
// owner has not added a key yet would be locked out of their own machine by the
// very step meant to protect it.
func planFail2ban(installed, running bool) Plan {
	p := Plan{Name: "fail2ban for SSH"}
	switch {
	case installed && running:
		p.Satisfied = true
		p.Detail = "installed and running"
		return p
	case installed:
		p.Detail = "installed but not running, so nothing is being banned"
	default:
		p.Detail = "not installed; repeated SSH login attempts are unthrottled"
	}

	jail := "[sshd]\\nenabled = true\\nmaxretry = 5\\nbantime = 1h\\nfindtime = 10m\\n"
	p.Commands = []string{
		"apt-get install -y fail2ban",
		fmt.Sprintf("mkdir -p /etc/fail2ban/jail.d && printf '%s' > %s", jail, fail2banJailPath),
		"systemctl enable --now fail2ban",
	}
	return p
}

// Fail2banPlan reads the host and plans from it.
func Fail2banPlan() Plan { return planFail2ban(fail2banInstalled(), fail2banRunning()) }

var fail2banInstalled = func() bool {
	_, err := exec.LookPath("fail2ban-server")
	return err == nil
}

var fail2banRunning = func() bool {
	return exec.Command("systemctl", "is-active", "--quiet", "fail2ban").Run() == nil
}

// ── The set ───────────────────────────────────────────────────────────────────

// All returns every basic measured against this machine, in the order the
// install and doctor report them.
func All() []Plan {
	return []Plan{SwapPlan(), TimezonePlan(), UnattendedUpgradesPlan(), Fail2banPlan()}
}

// Outstanding returns only the basics that still need work.
func Outstanding() []Plan {
	var out []Plan
	for _, p := range All() {
		if !p.Satisfied && len(p.Commands) > 0 {
			out = append(out, p)
		}
	}
	return out
}
