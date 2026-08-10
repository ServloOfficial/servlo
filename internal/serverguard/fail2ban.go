package serverguard

import (
	"fmt"
	"net"
	"os"
	"strings"
)

// fail2ban, and the honest limit of what servlo can do about it.
//
// Reading a jail's status and unbanning an address both go through
// fail2ban-client, which talks to a root-owned socket. Servlo does not run as
// root and never runs sudo on somebody's behalf (CLAUDE.md §3.2), so it cannot
// list bans and cannot lift one. Saying otherwise, or quietly granting itself
// the privilege to make it possible, would trade the whole privilege model for
// a convenience.
//
// So the panel does the two things it honestly can: say whether fail2ban is
// running at all, which is the thing that is actually often wrong, and build
// the exact command to unban an address, validated so that what gets pasted
// into a root shell is a command and not an injection.

// SocketPaths are where fail2ban's control socket lives, depending on how the
// distribution lays out /run.
var SocketPaths = []string{"/var/run/fail2ban/fail2ban.sock", "/run/fail2ban/fail2ban.sock"}

// DefaultJail is the jail an operator means when they say fail2ban. Servlo's
// bootstrap configures this one, because SSH password authentication stays
// enabled by design and this is what stands in front of it.
const DefaultJail = "sshd"

// Fail2ban is what servlo can say about it.
type Fail2ban struct {
	// Running is whether the control socket is there. It is the only signal
	// available without root, and it is the one that matters: a fail2ban that
	// was never installed, or that failed to start after an upgrade, is the
	// common failure, not a misconfigured jail.
	Running bool `json:"running"`
	// StatusCommand shows the current bans.
	StatusCommand string `json:"status_command"`
	// InstallCommands bring it up when it is not running.
	InstallCommands []string `json:"install_commands,omitempty"`
	// Why explains the position servlo is in, so nobody reads the absence of a
	// ban list as servlo saying there are no bans.
	Why string `json:"why"`
}

// socketExists is the seam, so a test does not depend on whether the machine it
// runs on happens to have fail2ban.
var socketExists = func() bool {
	for _, p := range SocketPaths {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// Fail2banStatus is what servlo knows about fail2ban.
func Fail2banStatus(jail string) Fail2ban {
	jail = normalizeJail(jail)
	out := Fail2ban{
		Running:       socketExists(),
		StatusCommand: "sudo fail2ban-client status " + jail,
		Why: "Reading the bans and lifting one both need root, which servlo does not have and does not " +
			"ask for, so these are commands to run rather than buttons. Servlo can see whether fail2ban " +
			"is running, which is the part that is usually wrong.",
	}
	if !out.Running {
		out.InstallCommands = []string{
			"sudo apt-get install -y fail2ban",
			"sudo systemctl enable --now fail2ban",
		}
		out.Why = "Servlo keeps SSH password authentication enabled by design, which makes fail2ban the " +
			"thing standing between this server and an automated password guesser."
	}
	return out
}

// UnbanCommand is what to run to lift a ban on one address.
//
// The address is parsed rather than pattern-matched, and the jail is checked
// against what a jail name can be. This text is built to be pasted into a root
// shell, so anything that is not plainly an address does not get to be part of
// it: a "host" of `1.2.3.4; rm -rf /` would otherwise become a second command.
func UnbanCommand(jail, addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if net.ParseIP(addr) == nil {
		return "", fmt.Errorf("%q is not an IP address. fail2ban bans addresses, so that is what it unbans", addr)
	}
	jail = normalizeJail(jail)
	if !validJail(jail) {
		return "", fmt.Errorf("%q is not a jail name", jail)
	}
	return fmt.Sprintf("sudo fail2ban-client set %s unbanip %s", jail, addr), nil
}

func normalizeJail(jail string) string {
	if jail = strings.TrimSpace(jail); jail == "" {
		return DefaultJail
	}
	return jail
}

// validJail is the character set fail2ban allows in a jail name. Anything else
// would be a way to put arbitrary text into a command a person is about to run
// as root.
func validJail(jail string) bool {
	if len(jail) > 64 {
		return false
	}
	for _, r := range jail {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return jail != ""
}
