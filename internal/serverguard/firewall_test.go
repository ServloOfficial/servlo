package serverguard

import (
	"strings"
	"testing"
)

// The plan opens exactly what a servlo server serves and closes the rest. SSH
// is first and non-negotiable: a firewall enabled before SSH is allowed locks
// the operator out of the machine they are configuring.
func TestFirewallPlan_AllowsSSHBeforeItEnablesAnything(t *testing.T) {
	plan := FirewallPlan(FirewallWant{SSHPort: 22, HTTP: true, HTTPS: true})

	var sshAt, enableAt = -1, -1
	for i, cmd := range plan.Commands {
		if strings.Contains(cmd, "allow 22/tcp") && sshAt < 0 {
			sshAt = i
		}
		if strings.Contains(cmd, "enable") {
			enableAt = i
		}
	}
	if sshAt < 0 {
		t.Fatalf("the plan never allows SSH:\n%s", strings.Join(plan.Commands, "\n"))
	}
	if enableAt < 0 {
		t.Fatalf("the plan never enables the firewall:\n%s", strings.Join(plan.Commands, "\n"))
	}
	if sshAt > enableAt {
		t.Errorf("the firewall is enabled before SSH is allowed, which locks the operator out:\n%s",
			strings.Join(plan.Commands, "\n"))
	}
}

// A non-standard SSH port has to be the one that is opened. Opening 22 on a
// server whose sshd is on 2222 is the same lockout with extra steps.
func TestFirewallPlan_OpensTheSSHPortInUse(t *testing.T) {
	plan := FirewallPlan(FirewallWant{SSHPort: 2222, HTTP: true, HTTPS: true})
	joined := strings.Join(plan.Commands, "\n")
	if !strings.Contains(joined, "allow 2222/tcp") {
		t.Errorf("the plan does not open the port sshd is on:\n%s", joined)
	}
	if strings.Contains(joined, "allow 22/tcp") {
		t.Errorf("the plan opens 22 as well, which is not where sshd is:\n%s", joined)
	}
}

// Every command is one a person runs themselves. Servlo never runs these.
func TestFirewallPlan_IsSomethingAPersonRuns(t *testing.T) {
	plan := FirewallPlan(FirewallWant{SSHPort: 22, HTTP: true, HTTPS: true})
	for _, cmd := range plan.Commands {
		if !strings.HasPrefix(cmd, "sudo ") {
			t.Errorf("%q is not written as a command to hand to a person", cmd)
		}
	}
}

// A port that is listening on a public address and is not in the plan is the
// finding worth surfacing: a firewall rule is a claim, an open socket is the
// truth, and the gap between them is where the surprises are.
func TestUnexpectedlyOpen_FindsWhatThePlanDoesNotCover(t *testing.T) {
	want := FirewallWant{SSHPort: 22, HTTP: true, HTTPS: true}

	extra := UnexpectedlyOpen(want, []int{22, 80, 443, 3306, 9000})
	if len(extra) != 2 || extra[0] != 3306 || extra[1] != 9000 {
		t.Errorf("unexpected = %v, want the database and the object store", extra)
	}
	if len(UnexpectedlyOpen(want, []int{22, 80, 443})) != 0 {
		t.Error("a server serving exactly what it should reported a finding")
	}
}

// A server with no HTTPS yet still needs 80 open, because that is where the
// certificate challenge is answered. Closing it would make the first Get SSL
// fail with nothing to point at.
func TestFirewallPlan_KeepsPort80ForTheCertificateChallenge(t *testing.T) {
	plan := FirewallPlan(FirewallWant{SSHPort: 22, HTTP: true})
	if !strings.Contains(strings.Join(plan.Commands, "\n"), "allow 80/tcp") {
		t.Error("the plan closes the port the HTTP-01 challenge is answered on")
	}
}
