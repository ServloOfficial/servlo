package ports

import (
	"os"
	"strings"
	"testing"
)

// stubHost redirects every probe CheckHealth makes, so the health logic can be
// driven without a real kernel, /etc, or systemd.
func stubHost(t *testing.T, start int, startOK bool, files map[string]string, units map[string]bool) {
	t.Helper()
	origStart, origRead, origUnit := liveUnprivStart, readTextFile, systemdUnitIs
	t.Cleanup(func() { liveUnprivStart, readTextFile, systemdUnitIs = origStart, origRead, origUnit })

	liveUnprivStart = func() (int, bool) { return start, startOK }
	readTextFile = func(path string) ([]byte, error) {
		body, ok := files[path]
		if !ok {
			return nil, os.ErrNotExist
		}
		return []byte(body), nil
	}
	systemdUnitIs = func(unit, verb string) bool { return units[unit+" "+verb] }
}

func TestCheckHealth_SysctlLiveAndPersistedIsHealthy(t *testing.T) {
	stubHost(t, 80, true, map[string]string{
		dropInPath: "net.ipv4.ip_unprivileged_port_start=80\n",
	}, nil)

	h := CheckHealth(Sysctl)
	if !h.Live || !h.Persistent {
		t.Fatalf("live=%v persistent=%v, want both true (%s)", h.Live, h.Persistent, h.Detail)
	}
	if !h.Healthy() {
		t.Error("Healthy() = false with both halves in place")
	}
}

// The whole point of S1.4: `sysctl -w` alone works right now and is gone after
// a reboot, so a host in that state serves happily until it restarts and then
// silently stops. Doctor has to catch it while the machine is still up.
func TestCheckHealth_SysctlLiveButNotPersistedFailsOnTheRebootClause(t *testing.T) {
	stubHost(t, 80, true, nil, nil)

	h := CheckHealth(Sysctl)
	if !h.Live {
		t.Error("Live = false on a kernel that currently allows the bind")
	}
	if h.Persistent {
		t.Fatal("Persistent = true with no drop-in on disk")
	}
	if h.Healthy() {
		t.Error("Healthy() = true for a setting that does not survive a reboot")
	}
	if !strings.Contains(h.Detail, "reboot") {
		t.Errorf("detail does not say what breaks: %q", h.Detail)
	}
	if len(h.Fix) == 0 {
		t.Error("no fix offered for a repairable state")
	}
}

// The reverse: the drop-in is on disk but nobody applied it live, which is what
// a host looks like between running the second command and rebooting.
func TestCheckHealth_SysctlPersistedButNotLive(t *testing.T) {
	stubHost(t, 1024, true, map[string]string{
		dropInPath: "net.ipv4.ip_unprivileged_port_start=80\n",
	}, nil)

	h := CheckHealth(Sysctl)
	if h.Live {
		t.Error("Live = true while the running kernel still blocks the bind")
	}
	if !h.Persistent {
		t.Error("Persistent = false with the drop-in on disk")
	}
	if h.Healthy() {
		t.Error("Healthy() = true while nginx cannot bind right now")
	}
}

// A drop-in someone edited to a value that no longer admits 443 is worse than a
// missing one, because it looks present.
func TestCheckHealth_SysctlDropInWithAWrongValueIsNotPersistent(t *testing.T) {
	stubHost(t, 80, true, map[string]string{
		dropInPath: "net.ipv4.ip_unprivileged_port_start=1024\n",
	}, nil)

	if h := CheckHealth(Sysctl); h.Persistent {
		t.Errorf("accepted a drop-in that does not allow the bind: %+v", h)
	}
}

func TestCheckHealth_NFTablesLoadedAndPersistedIsHealthy(t *testing.T) {
	stubHost(t, 0, false, map[string]string{
		nftRulesPath: "table inet servlo {}",
		nftMainConf:  "include \"" + nftRulesPath + "\"\n",
	}, map[string]bool{"nftables is-active": true, "nftables is-enabled": true})

	h := CheckHealth(NFTables)
	if !h.Healthy() {
		t.Fatalf("not healthy with rules, include and unit all in place: %+v", h)
	}
}

// Rules loaded by hand, never included from the main file: they vanish at the
// next boot exactly like a bare sysctl -w.
func TestCheckHealth_NFTablesWithoutTheIncludeIsNotPersistent(t *testing.T) {
	stubHost(t, 0, false, map[string]string{
		nftRulesPath: "table inet servlo {}",
		nftMainConf:  "# nothing here\n",
	}, map[string]bool{"nftables is-active": true, "nftables is-enabled": true})

	h := CheckHealth(NFTables)
	if h.Persistent {
		t.Error("Persistent = true with no include in the main config")
	}
	if !strings.Contains(h.Detail, "reboot") {
		t.Errorf("detail does not say what breaks: %q", h.Detail)
	}
}

// The include can be there while the unit that reads it is disabled, which
// again survives inspection but not a restart.
func TestCheckHealth_NFTablesWithTheUnitDisabledIsNotPersistent(t *testing.T) {
	stubHost(t, 0, false, map[string]string{
		nftRulesPath: "table inet servlo {}",
		nftMainConf:  "include \"" + nftRulesPath + "\"\n",
	}, map[string]bool{"nftables is-active": true, "nftables is-enabled": false})

	if h := CheckHealth(NFTables); h.Persistent {
		t.Error("Persistent = true with nftables.service disabled")
	}
}

func TestCheckHealth_NFTablesWithNoRulesFileIsNotLive(t *testing.T) {
	stubHost(t, 0, false, nil, map[string]bool{"nftables is-active": true})

	h := CheckHealth(NFTables)
	if h.Live {
		t.Error("Live = true with no rules file at all")
	}
	if len(h.Fix) == 0 {
		t.Error("no fix offered when the rules were never written")
	}
}

// Every repair doctor prints needs privilege, and servlo does not take it.
func TestCheckHealth_FixesAreSudoCommands(t *testing.T) {
	stubHost(t, 1024, true, nil, nil)
	for _, c := range CheckHealth(Sysctl).Fix {
		if !strings.HasPrefix(c, "sudo ") {
			t.Errorf("fix does not name the privilege it needs: %q", c)
		}
	}
}
