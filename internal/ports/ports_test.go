package ports

import (
	"strings"
	"testing"
)

func TestPublish_SysctlBindsTheRealPorts(t *testing.T) {
	http, https := Publish(Sysctl)
	if http != 80 || https != 443 {
		t.Errorf("Publish(Sysctl) = %d, %d; want 80, 443", http, https)
	}
}

// Under the fallback nginx cannot hold 80/443 itself, so it takes the high
// ports the DNAT rules redirect into.
func TestPublish_NFTablesBindsTheHighPorts(t *testing.T) {
	http, https := Publish(NFTables)
	if http != 8080 || https != 8443 {
		t.Errorf("Publish(NFTables) = %d, %d; want 8080, 8443", http, https)
	}
}

// A host already below the threshold needs nothing run at all. Emitting the
// commands anyway would train the operator to paste sudo lines that change
// nothing, which is how a real one gets skipped later.
func TestPlanFor_SysctlAlreadySatisfiedAsksForNothing(t *testing.T) {
	p := PlanFor(80, true)
	if p.Strategy != Sysctl {
		t.Errorf("strategy = %q, want %q", p.Strategy, Sysctl)
	}
	if len(p.Commands) != 0 {
		t.Errorf("commands = %v, want none", p.Commands)
	}
	if !p.Satisfied {
		t.Error("Satisfied = false on a host that already allows the bind")
	}
}

func TestPlanFor_SysctlNeedsLoweringEmitsBothLiveAndPersistentCommands(t *testing.T) {
	p := PlanFor(1024, true)
	if p.Strategy != Sysctl {
		t.Fatalf("strategy = %q, want %q", p.Strategy, Sysctl)
	}
	if p.Satisfied {
		t.Error("Satisfied = true while the sysctl still blocks the bind")
	}
	if p.HTTPPort != 80 || p.HTTPSPort != 443 {
		t.Errorf("ports = %d, %d; want 80, 443", p.HTTPPort, p.HTTPSPort)
	}
	joined := strings.Join(p.Commands, "\n")
	// Live and persistent are separate steps: sysctl -w alone is lost at reboot,
	// and the drop-in alone does not take effect until one.
	if !strings.Contains(joined, "sysctl -w net.ipv4.ip_unprivileged_port_start=80") {
		t.Errorf("no live sysctl in:\n%s", joined)
	}
	if !strings.Contains(joined, dropInPath) {
		t.Errorf("no persistent drop-in in:\n%s", joined)
	}
}

// A kernel that does not expose the sysctl cannot be talked into it, so the
// plan switches rather than emitting a command that would fail.
func TestPlanFor_NoSysctlFallsBackToNFTables(t *testing.T) {
	p := PlanFor(0, false)
	if p.Strategy != NFTables {
		t.Fatalf("strategy = %q, want %q", p.Strategy, NFTables)
	}
	if p.HTTPPort != 8080 || p.HTTPSPort != 8443 {
		t.Errorf("ports = %d, %d; want 8080, 8443", p.HTTPPort, p.HTTPSPort)
	}
	joined := strings.Join(p.Commands, "\n")
	for _, want := range []string{"80", "8080", "443", "8443", "redirect to"} {
		if !strings.Contains(joined, want) {
			t.Errorf("nftables plan does not mention %q:\n%s", want, joined)
		}
	}
}

// A rule that vanishes at reboot is the failure this whole story exists to
// avoid: the droplet comes back and serves nothing on 80.
func TestPlanFor_NFTablesPersistsAcrossReboot(t *testing.T) {
	joined := strings.Join(PlanFor(0, false).Commands, "\n")
	if !strings.Contains(joined, nftRulesPath) {
		t.Errorf("rules are not written to a file that survives a reboot:\n%s", joined)
	}
	if !strings.Contains(joined, "systemctl enable") {
		t.Errorf("nothing re-loads the rules at boot:\n%s", joined)
	}
}

// CLAUDE.md 3.2: servlo never runs sudo itself. Every privileged step is
// emitted for a human to read and run, which only works if it is a complete
// command rather than a description of one.
func TestPlanFor_EveryCommandIsRunnableAndAsksForPrivilegeExplicitly(t *testing.T) {
	for _, p := range []Plan{PlanFor(1024, true), PlanFor(0, false)} {
		if len(p.Commands) == 0 {
			t.Fatalf("%s plan emits no commands", p.Strategy)
		}
		for _, c := range p.Commands {
			if !strings.HasPrefix(c, "sudo ") {
				t.Errorf("%s: command does not name the privilege it needs: %q", p.Strategy, c)
			}
			if strings.TrimSpace(c) != c {
				t.Errorf("%s: command has stray whitespace: %q", p.Strategy, c)
			}
		}
	}
}

func TestParseStrategy(t *testing.T) {
	cases := map[string]Strategy{
		"sysctl":   Sysctl,
		"nftables": NFTables,
		"":         Sysctl,
		"nonsense": Sysctl,
	}
	for in, want := range cases {
		if got := ParseStrategy(in); got != want {
			t.Errorf("ParseStrategy(%q) = %q, want %q", in, got, want)
		}
	}
}
