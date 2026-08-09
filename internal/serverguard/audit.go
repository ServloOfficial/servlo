package serverguard

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/realrashid/servlo/internal/config"
)

// Severity is how much a finding matters.
type Severity string

const (
	// Bad is something an attacker could use today.
	Bad Severity = "bad"
	// Warn is something that will hurt later, or that servlo cannot confirm.
	Warn Severity = "warn"
	// OK is a check that passed, reported so the audit says what it looked at
	// rather than only what it disliked.
	OK Severity = "ok"
)

// Finding is one thing the audit looked at.
type Finding struct {
	Severity Severity `json:"severity"`
	Title    string   `json:"title"`
	Detail   string   `json:"detail,omitempty"`
	// Fix is the command a person runs. Empty when servlo can fix it itself or
	// when there is nothing to run.
	Fix []string `json:"fix,omitempty"`
}

// Audit is what this server looks like from outside, and what is wrong with it.
//
// Nothing here changes anything. Every fix that needs privilege is printed for
// a person to run, which is the same division the rest of servlo keeps: servlo
// never runs sudo on somebody's behalf.
func Audit(want FirewallWant) []Finding {
	var out []Finding
	out = append(out, listeningFindings(want)...)
	out = append(out, fail2banFinding())
	out = append(out, unattendedUpgradesFinding())
	out = append(out, cloudFirewallFinding())
	out = append(out, configModeFindings()...)
	return out
}

func listeningFindings(want FirewallWant) []Finding {
	ports, err := Listening()
	if err != nil {
		return []Finding{{
			Severity: Warn,
			Title:    "Servlo could not read which ports are listening",
			Detail:   err.Error(),
		}}
	}
	extra := UnexpectedlyOpen(want, ports)
	if len(extra) == 0 {
		return []Finding{{
			Severity: OK,
			Title:    "Nothing unexpected is listening on a public address",
			Detail:   "Open: " + joinPorts(ports),
		}}
	}
	return []Finding{{
		Severity: Bad,
		Title:    fmt.Sprintf("%d %s listening on a public address that should not be", len(extra), portWord(len(extra))),
		Detail: "Open and unaccounted for: " + joinPorts(extra) + ". A servlo service binds to the container " +
			"network, so anything here is either a published port that is no longer needed or a service bound " +
			"to the wrong address.",
		Fix: FirewallPlan(want).Commands,
	}}
}

// fail2banFinding reports whether fail2ban is there at all.
//
// Its socket is the honest signal available without root: fail2ban-client
// needs privilege, and asking for it to read a status would be servlo running
// sudo, which it does not do.
func fail2banFinding() Finding {
	status := Fail2banStatus(DefaultJail)
	if status.Running {
		return Finding{
			Severity: OK,
			Title:    "fail2ban is running",
			Detail:   "Banned addresses: " + status.StatusCommand,
		}
	}
	return Finding{
		Severity: Warn,
		Title:    "fail2ban does not appear to be running",
		Detail:   status.Why,
		Fix:      append(status.InstallCommands, status.StatusCommand),
	}
}

func unattendedUpgradesFinding() Finding {
	if _, err := os.Stat("/etc/apt/apt.conf.d/20auto-upgrades"); err == nil {
		return Finding{Severity: OK, Title: "Unattended upgrades are configured"}
	}
	return Finding{
		Severity: Warn,
		Title:    "Security updates are not being installed on their own",
		Fix: []string{
			"sudo apt-get install -y unattended-upgrades",
			"sudo dpkg-reconfigure -plow unattended-upgrades",
		},
	}
}

// cloudFirewallFinding says the thing that is otherwise debugged at the wrong
// layer for an afternoon.
//
// Servlo cannot see a provider's firewall: it sits in front of the machine and
// is invisible from inside it. What servlo can do is say it exists, so a port
// that is open in ufw and still unreachable is looked for in the right place.
func cloudFirewallFinding() Finding {
	p := DetectProvider()
	title := "A provider firewall may also be filtering"
	if p.Name != "" {
		title = p.Name + "'s firewall may also be filtering"
	}
	detail := p.Detail
	if p.FirewallURL != "" {
		detail += " " + p.FirewallURL
	}
	return Finding{Severity: OK, Title: title, Detail: detail}
}

// configModeFindings checks the modes on what servlo keeps, because half of it
// is a credential.
func configModeFindings() []Finding {
	type target struct {
		name string
		path string
	}
	targets := []target{
		{"the backup key", filepath.Join(config.ConfigDir(), "backup.key")},
		{"the database connections", filepath.Join(config.ConfigDir(), "databases.yaml")},
		{"the per-site database accounts", filepath.Join(config.ConfigDir(), "database-users.yaml")},
		{"the backup destinations", filepath.Join(config.ConfigDir(), "backup-destinations.yaml")},
		{"the panel's SMTP settings", filepath.Join(config.ConfigDir(), "smtp.json")},
	}
	var bad []string
	var checked int
	for _, t := range targets {
		info, err := os.Stat(t.path)
		if err != nil {
			continue
		}
		checked++
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			bad = append(bad, fmt.Sprintf("%s is %04o", t.name, perm))
		}
	}
	if checked == 0 {
		return nil
	}
	if len(bad) == 0 {
		return []Finding{{
			Severity: OK,
			Title:    fmt.Sprintf("Every stored credential is private (%d checked)", checked),
		}}
	}
	fix := make([]string, 0, len(bad))
	for _, t := range targets {
		if _, err := os.Stat(t.path); err == nil {
			fix = append(fix, "chmod 600 "+t.path)
		}
	}
	return []Finding{{
		Severity: Bad,
		Title:    "A stored credential is readable by other accounts on this server",
		Detail:   strings.Join(bad, "; "),
		Fix:      fix,
	}}
}

func joinPorts(ports []int) string {
	if len(ports) == 0 {
		return "none"
	}
	out := make([]string, len(ports))
	for i, p := range ports {
		out[i] = strconv.Itoa(p)
	}
	return strings.Join(out, ", ")
}

func portWord(n int) string {
	if n == 1 {
		return "port is"
	}
	return "ports are"
}
