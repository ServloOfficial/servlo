package sftpaccess

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigDeclaresTheSSHPortsAlreadyInUse(t *testing.T) {
	// Declaring any Port in a drop-in replaces sshd's default of 22. A generated
	// file that named only the SFTP ports would take the operator's own way in
	// away from them on the next reload, which is the one bug in this feature
	// nobody would recover from remotely.
	config := Config([]Chroot{{Domain: "acme.com", SitePath: "/srv/sites/acme", Port: 2201}}, []int{22, 2222})

	for _, want := range []string{"\nPort 22\n", "\nPort 2222\n", "\nPort 2201\n"} {
		if !strings.Contains(config, want) {
			t.Fatalf("config does not declare %q:\n%s", strings.TrimSpace(want), config)
		}
	}
	// Every Port has to precede the first Match, or it lands inside a block and
	// sshd rejects the file.
	if strings.Index(config, "Match LocalPort") < strings.LastIndex(config, "\nPort ") {
		t.Fatalf("a Port line falls inside a Match block:\n%s", config)
	}
}

// Servlo never disables password login (CLAUDE.md section 3.2). The generated
// file is the one place that could, so it is asserted rather than assumed.
func TestConfigNeverTouchesPasswordAuthentication(t *testing.T) {
	config := Config([]Chroot{{Domain: "acme.com", SitePath: "/srv/sites/acme", Port: 2201}}, []int{22})

	// Directives only: the header says in prose that servlo does not set these,
	// and naming them there is the point.
	for _, line := range strings.Split(config, "\n") {
		directive := strings.TrimSpace(line)
		if directive == "" || strings.HasPrefix(directive, "#") {
			continue
		}
		for _, forbidden := range []string{"PasswordAuthentication", "PermitRootLogin", "PubkeyAuthentication", "AuthorizedKeysFile", "ChallengeResponseAuthentication"} {
			if strings.HasPrefix(directive, forbidden) {
				t.Fatalf("the generated config sets %s: %q", forbidden, line)
			}
		}
	}
}

func TestConfigConfinesEachSiteToItsOwnPort(t *testing.T) {
	config := Config([]Chroot{
		{Domain: "acme.com", SitePath: "/srv/sites/acme", Port: 2201},
		{Domain: "beta.example", SitePath: "/srv/sites/beta", Port: 2202},
	}, []int{22})

	for _, want := range []string{
		"Match LocalPort 2201",
		"ChrootDirectory /srv/servlo-sftp/acme.com",
		"Match LocalPort 2202",
		"ChrootDirectory /srv/servlo-sftp/beta.example",
		"ForceCommand internal-sftp -d /site",
		"PermitTTY no",
		"AllowTcpForwarding no",
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("config is missing %q:\n%s", want, config)
		}
	}
}

func TestConfigIsEmptyWithNoSites(t *testing.T) {
	if got := Config(nil, []int{22}); got != "" {
		t.Fatalf("config with no sites = %q, want nothing to install", got)
	}
}

// The plan is printed, never run. What matters is that every command is one an
// operator can read, and that the sudo lives in the printed form only.
func TestPlanPrintsTheSudoBlockAndRunsNothing(t *testing.T) {
	staged := filepath.Join(t.TempDir(), "60-servlo-sftp.conf")
	plan := ChrootPlan([]Chroot{{Domain: "acme.com", SitePath: "/srv/sites/acme", Port: 2201}}, staged, "deploy", "deploy")

	if plan.Satisfied {
		t.Fatal("a plan for a machine with no chroot should not report satisfied")
	}
	joined := strings.Join(plan.Commands, "\n")
	for _, want := range []string{
		"install -d -o root -g root -m 755 " + ChrootBase + "/acme.com",
		"install -d -o deploy -g deploy -m 755 " + ChrootBase + "/acme.com/site",
		"mount --bind /srv/sites/acme " + ChrootBase + "/acme.com/site",
		"/etc/fstab",
		"install -m 644 -o root -g root " + staged + " " + SSHDDropIn,
		"sshd -t",
		"systemctl reload ssh",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("plan is missing %q:\n%s", want, joined)
		}
	}
	for _, command := range plan.Commands {
		if strings.HasPrefix(command, "sudo ") {
			t.Fatalf("a command carries its own sudo: %q", command)
		}
	}
	for _, line := range plan.ForHuman() {
		if !strings.HasPrefix(line, "sudo ") {
			t.Fatalf("the printed form is missing sudo: %q", line)
		}
	}
}

// sshd -t before the reload, so a configuration servlo generated wrong is
// refused on the operator's terminal rather than taking sshd down.
func TestPlanTestsTheConfigBeforeReloadingSSH(t *testing.T) {
	plan := ChrootPlan([]Chroot{{Domain: "acme.com", SitePath: "/srv/sites/acme", Port: 2201}}, "/tmp/staged.conf", "deploy", "deploy")

	joined := strings.Join(plan.Commands, "\n")
	if strings.Index(joined, "sshd -t") > strings.Index(joined, "systemctl reload ssh") {
		t.Fatalf("the reload comes before the test:\n%s", joined)
	}
}

func TestStageWritesTheConfigWhereTheOperatorCanReadIt(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	path, err := Stage([]Chroot{{Domain: "acme.com", SitePath: "/srv/sites/acme", Port: 2201}}, []int{22})
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Match LocalPort 2201") {
		t.Fatalf("staged config = %q", body)
	}
	// 0644, because it is going to be installed as a file sshd reads and an
	// operator is going to cat it first.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("staged mode = %v, want 0644", info.Mode().Perm())
	}
}

func TestPortsAreStableAcrossCallsAndUniquePerSite(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	acme, err := AssignPort("acme.com")
	if err != nil {
		t.Fatal(err)
	}
	beta, err := AssignPort("beta.example")
	if err != nil {
		t.Fatal(err)
	}
	if acme == beta {
		t.Fatalf("two sites got the same port: %d", acme)
	}
	if acme < firstPort {
		t.Fatalf("port %d is below the range servlo allocates from", acme)
	}
	again, err := AssignPort("acme.com")
	if err != nil {
		t.Fatal(err)
	}
	if again != acme {
		t.Fatalf("the port moved from %d to %d; the operator's saved connection would break", acme, again)
	}
}

func TestReleasePortForgetsASite(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if _, err := AssignPort("acme.com"); err != nil {
		t.Fatal(err)
	}

	if err := ReleasePort("acme.com"); err != nil {
		t.Fatal(err)
	}
	ports, err := Ports()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ports["acme.com"]; ok {
		t.Fatalf("ports = %+v, want acme.com gone", ports)
	}
}

func TestConfiguredSSHPortsReadsTheHostsOwnFiles(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "sshd_config")
	if err := os.WriteFile(main, []byte("#Port 22\nPort 2222\nPermitRootLogin no\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dropIns := filepath.Join(dir, "sshd_config.d")
	if err := os.Mkdir(dropIns, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dropIns, "10-other.conf"), []byte("Port 2020\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Servlo's own file is skipped: reading its own Port lines back would pin
	// the SFTP ports into the "ports sshd already uses" list for ever.
	if err := os.WriteFile(filepath.Join(dropIns, filepath.Base(SSHDDropIn)), []byte("Port 2201\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := configuredSSHPorts(main, dropIns)

	if len(got) != 2 || got[0] != 2020 || got[1] != 2222 {
		t.Fatalf("ports = %v, want [2020 2222]", got)
	}
}

// A host that declares no Port at all is listening on 22, and a generated file
// that forgot to say so would move it.
func TestConfiguredSSHPortsFallsBackToTwentyTwo(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "sshd_config")
	if err := os.WriteFile(main, []byte("#Port 22\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := configuredSSHPorts(main, filepath.Join(dir, "missing"))

	if len(got) != 1 || got[0] != 22 {
		t.Fatalf("ports = %v, want [22]", got)
	}
}

// The range servlo allocates from, 2200 to 2999, is exactly where an operator
// who moved sshd off 22 tends to have put it. Handing a site the port sshd is
// already on writes a Match LocalPort block for it, and every shell login on
// that port becomes a chrooted internal-sftp session: the operator loses the
// only way into their own machine, remotely, from a panel action.
func TestAssignPort_NeverTakesAPortSSHIsAlreadyOn(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	prev := reservedSSHPorts
	reservedSSHPorts = func() []int { return []int{22, firstPort, firstPort + 1} }
	t.Cleanup(func() { reservedSSHPorts = prev })

	port, err := AssignPort("acme.com")
	if err != nil {
		t.Fatal(err)
	}
	if port == firstPort || port == firstPort+1 {
		t.Errorf("a site was given %d, which is a port sshd is listening on", port)
	}
	if port != firstPort+2 {
		t.Errorf("port = %d, want the first one below sshd's", port)
	}
}

// And an allocation made before that was true is corrected rather than kept.
// Stability is worth a saved connection breaking; it is not worth the operator's
// shell.
func TestAssignPort_MovesASiteOffAPortSSHHasTaken(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if err := savePorts(map[string]int{"acme.com": 2222}); err != nil {
		t.Fatal(err)
	}
	prev := reservedSSHPorts
	reservedSSHPorts = func() []int { return []int{2222} }
	t.Cleanup(func() { reservedSSHPorts = prev })

	port, err := AssignPort("acme.com")
	if err != nil {
		t.Fatal(err)
	}
	if port == 2222 {
		t.Error("the site kept the port sshd listens on")
	}
	ports, err := Ports()
	if err != nil {
		t.Fatal(err)
	}
	if ports["acme.com"] != port {
		t.Errorf("the move was not recorded: %+v", ports)
	}
}

// The file is the only record of which port a site answers on, and the whole
// point of it is that the port does not move. os.WriteFile empties a file before
// it writes a byte, so a write that could not finish would read as no
// allocations at all and every site would be handed a new port on the next pass.
func TestSavePorts_ReplacesTheFileRatherThanRewritingIt(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if err := savePorts(map[string]int{"acme.com": 2201}); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(PortsFile())
	if err != nil {
		t.Fatal(err)
	}
	if err := savePorts(map[string]int{"acme.com": 2201, "beta.example": 2202}); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(PortsFile())
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Error("the port file was rewritten in place, so a write that runs out of disk moves every site's SFTP port")
	}
	if mode := after.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode = %o, want 600", mode)
	}
}
