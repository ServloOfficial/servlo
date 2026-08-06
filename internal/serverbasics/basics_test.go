package serverbasics

import (
	"strings"
	"testing"
)

func allCommands(plans []Plan) string {
	var b strings.Builder
	for _, p := range plans {
		b.WriteString(p.Name + ":\n" + strings.Join(p.Commands, "\n") + "\n")
		b.WriteString(p.Detail + "\n")
	}
	return b.String()
}

// The hard rule, and the one most worth a test rather than a comment. Locking
// an operator out of their own droplet is not a service, and it is exactly what
// "hardening" scripts do by turning off password login on a machine whose owner
// has not added a key yet. Servlo puts fail2ban in front of SSH and leaves the
// login method alone.
func TestNothingTouchesSSHPasswordAuthentication(t *testing.T) {
	plans := []Plan{
		planSwap(2*gib, 0),
		planTimezone("Europe/London"),
		planUnattendedUpgrades(false),
		planFail2ban(false, false),
	}
	body := allCommands(plans)

	for _, forbidden := range []string{
		"PasswordAuthentication",
		"PermitRootLogin",
		"ChallengeResponseAuthentication",
		"sshd_config",
		"KbdInteractiveAuthentication",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("a server basic touches %q, which could lock the operator out:\n%s", forbidden, body)
		}
	}
}

// Nothing here restarts or reloads sshd either. Even a config-preserving
// restart is a chance to drop the session the operator is running the install
// from.
func TestNothingRestartsSSH(t *testing.T) {
	body := allCommands([]Plan{
		planTimezone("UTC"),
		planUnattendedUpgrades(false),
		planFail2ban(false, false),
	})
	for _, forbidden := range []string{"restart ssh", "reload ssh", "restart sshd", "reload sshd"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("a server basic restarts SSH:\n%s", body)
		}
	}
}

// ── Timezone ──────────────────────────────────────────────────────────────────

// UTC is the right default for a server even when the operator is not in it.
// Logs, cron schedules, backup timestamps and certificate expiry all get
// compared across machines, and a local timezone makes every one of those
// comparisons a conversion someone has to remember to do.
func TestPlanTimezone_SetsUTCWhenSomethingElseIsConfigured(t *testing.T) {
	plan := planTimezone("America/New_York")
	if plan.Satisfied {
		t.Fatal("a machine on local time was reported satisfied")
	}
	joined := strings.Join(plan.Commands, "\n")
	if !strings.Contains(joined, "timedatectl set-timezone UTC") {
		t.Errorf("the timezone commands do not set UTC:\n%s", joined)
	}
	if !strings.Contains(plan.Detail, "America/New_York") {
		t.Errorf("detail %q does not say what the timezone currently is", plan.Detail)
	}
}

func TestPlanTimezone_AlreadyUTCIsSatisfied(t *testing.T) {
	if plan := planTimezone("UTC"); !plan.Satisfied || len(plan.Commands) != 0 {
		t.Errorf("a machine already on UTC was planned for again: %+v", plan)
	}
}

// Etc/UTC is what some images report and means the same thing. Treating it as a
// mismatch would have doctor nagging forever about a machine that is correct.
func TestPlanTimezone_AcceptsTheEquivalentSpellings(t *testing.T) {
	for _, tz := range []string{"UTC", "Etc/UTC", "Universal"} {
		if !planTimezone(tz).Satisfied {
			t.Errorf("%q was not recognised as UTC", tz)
		}
	}
}

// ── Unattended upgrades ───────────────────────────────────────────────────────

func TestPlanUnattendedUpgrades_InstallsAndEnables(t *testing.T) {
	plan := planUnattendedUpgrades(false)
	if plan.Satisfied {
		t.Fatal("a machine without unattended upgrades was reported satisfied")
	}
	joined := strings.Join(plan.Commands, "\n")
	for _, want := range []string{"unattended-upgrades", "20auto-upgrades"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the commands do not mention %q:\n%s", want, joined)
		}
	}
}

// Security updates only. Pulling every update automatically means a package
// changing behaviour under a running site with nobody watching, which is a
// worse failure than being a week behind on a non-security release.
func TestPlanUnattendedUpgrades_LimitsItselfToSecurity(t *testing.T) {
	joined := strings.Join(planUnattendedUpgrades(false).Commands, "\n")
	if strings.Contains(joined, "Unattended-Upgrade::Allowed-Origins") && !strings.Contains(joined, "security") {
		t.Errorf("the origins are widened past security:\n%s", joined)
	}
	// And it must never reboot on its own: a site going down at 3am because a
	// kernel landed is not an improvement over a pending reboot.
	if strings.Contains(joined, "Automatic-Reboot \"true\"") {
		t.Errorf("unattended upgrades would reboot the machine unattended:\n%s", joined)
	}
}

func TestPlanUnattendedUpgrades_AlreadyOnIsSatisfied(t *testing.T) {
	if plan := planUnattendedUpgrades(true); !plan.Satisfied || len(plan.Commands) != 0 {
		t.Errorf("an already-configured machine was planned for again: %+v", plan)
	}
}

// ── fail2ban ──────────────────────────────────────────────────────────────────

func TestPlanFail2ban_InstallsEnablesAndJailsSSH(t *testing.T) {
	plan := planFail2ban(false, false)
	if plan.Satisfied {
		t.Fatal("a machine without fail2ban was reported satisfied")
	}
	joined := strings.Join(plan.Commands, "\n")
	for _, want := range []string{"apt-get install", "fail2ban", "jail.d", "systemctl enable"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the commands do not mention %q:\n%s", want, joined)
		}
	}
}

// Installed but not running is the state that reads as protected and is not.
func TestPlanFail2ban_InstalledButStoppedStillNeedsWork(t *testing.T) {
	plan := planFail2ban(true, false)
	if plan.Satisfied {
		t.Fatal("fail2ban installed but not running was reported satisfied")
	}
	if !strings.Contains(plan.Detail, "not running") {
		t.Errorf("detail %q does not say it is installed but stopped", plan.Detail)
	}
}

func TestPlanFail2ban_RunningIsSatisfied(t *testing.T) {
	if plan := planFail2ban(true, true); !plan.Satisfied || len(plan.Commands) != 0 {
		t.Errorf("a running fail2ban was planned for again: %+v", plan)
	}
}

// ── The set ───────────────────────────────────────────────────────────────────

// Every basic has a name doctor can print and a detail explaining its state,
// including the satisfied ones. A check that says nothing when it passes leaves
// an operator unable to tell "checked and fine" from "not checked".
func TestEveryPlanExplainsItself(t *testing.T) {
	for _, p := range []Plan{
		planSwap(2*gib, 4*gib),
		planSwap(2*gib, 0),
		planTimezone("UTC"),
		planTimezone("Asia/Kolkata"),
		planUnattendedUpgrades(true),
		planUnattendedUpgrades(false),
		planFail2ban(true, true),
		planFail2ban(false, false),
	} {
		if p.Name == "" {
			t.Errorf("a plan has no name: %+v", p)
		}
		if p.Detail == "" {
			t.Errorf("%s has no detail explaining its state", p.Name)
		}
		if p.Satisfied && len(p.Commands) > 0 {
			t.Errorf("%s is satisfied but still asks for commands: %v", p.Name, p.Commands)
		}
		if !p.Satisfied && len(p.Commands) == 0 && p.Name != "swap" {
			t.Errorf("%s is unsatisfied but offers no way to fix it", p.Name)
		}
	}
}
