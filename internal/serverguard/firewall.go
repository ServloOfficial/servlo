package serverguard

import (
	"fmt"
	"slices"
	"strconv"
)

// FirewallWant is what this server should be reachable on.
type FirewallWant struct {
	// SSHPort is where sshd listens. Opened first and always, because a
	// firewall enabled before SSH is allowed locks the operator out of the
	// machine they are configuring, from a session that is about to end.
	SSHPort int
	HTTP    bool
	HTTPS   bool
}

// DefaultWant is a servlo server: SSH, and the two ports the sites are served
// on. Nothing else needs to face the internet, because every service binds to
// the container network.
func DefaultWant(sshPort int) FirewallWant {
	if sshPort == 0 {
		sshPort = 22
	}
	return FirewallWant{SSHPort: sshPort, HTTP: true, HTTPS: true}
}

// Plan is a set of commands for a person to run, and why.
type Plan struct {
	Why      string
	Commands []string
}

// FirewallPlan is the ufw configuration a servlo server wants.
//
// Printed, never run. ufw needs root and servlo does not have it and does not
// ask for it, so what servlo produces is the exact text to paste, in an order
// that is safe to paste: SSH before enable, every time.
func FirewallPlan(want FirewallWant) Plan {
	ssh := want.SSHPort
	if ssh == 0 {
		ssh = 22
	}
	cmds := []string{
		// Order matters and this is the whole reason the plan is generated
		// rather than left to be typed: allow SSH, then set the defaults, then
		// enable. Any other order can end the session that is running it.
		"sudo ufw allow " + strconv.Itoa(ssh) + "/tcp comment 'SSH'",
	}
	if want.HTTP {
		// Open even on a server with no certificate yet: this is where the
		// HTTP-01 challenge is answered, and closing it makes the first Get SSL
		// fail with nothing to point at.
		cmds = append(cmds, "sudo ufw allow 80/tcp comment 'HTTP and ACME challenges'")
	}
	if want.HTTPS {
		cmds = append(cmds, "sudo ufw allow 443/tcp comment 'HTTPS'")
	}
	cmds = append(cmds,
		"sudo ufw default deny incoming",
		"sudo ufw default allow outgoing",
		"sudo ufw --force enable",
		"sudo ufw status verbose",
	)
	return Plan{
		Why: fmt.Sprintf("Open SSH on %d, HTTP and HTTPS, and refuse everything else. "+
			"Every servlo service binds to the container network, so nothing else needs to face the internet.", ssh),
		Commands: cmds,
	}
}

// UnexpectedlyOpen is every listening port the plan does not account for.
//
// This is the finding worth surfacing. A firewall rule is a claim about what
// should be reachable; an open socket is what is. The gap between them is where
// a forgotten published port or a service bound to the wrong address lives.
func UnexpectedlyOpen(want FirewallWant, listening []int) []int {
	expected := []int{want.SSHPort}
	if want.SSHPort == 0 {
		expected = []int{22}
	}
	if want.HTTP {
		expected = append(expected, 80)
	}
	if want.HTTPS {
		expected = append(expected, 443)
	}
	var extra []int
	for _, p := range listening {
		if !slices.Contains(expected, p) {
			extra = append(extra, p)
		}
	}
	return extra
}
