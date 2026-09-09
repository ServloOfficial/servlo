package sftpaccess

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ServloOfficial/servlo/internal/atomicfile"
	"github.com/ServloOfficial/servlo/internal/config"
	"gopkg.in/yaml.v3"
)

// The half that needs root, and therefore the half servlo only ever prints.
//
// Confining an SFTP session to one directory is sshd's ChrootDirectory, and
// ChrootDirectory has a hard requirement: the directory and every one of its
// parents must be owned by root and writable by nobody else. A process running
// as the servlo user cannot create such a directory, cannot chown one to root,
// and cannot bind-mount the site inside it. There is no clever way round that
// and servlo does not pretend there is.
//
// Which leaves the question of how sshd tells one site from another, since
// every site here runs as the same Linux user and Match has no "which site is
// this" to match on. It has LocalPort. So each site with SFTP gets its own port
// on the same sshd, and the Match block on that port carries the chroot. It is
// unusual, and the alternative was a Linux user per site, which PRD section 6
// rules out on purpose.
//
// One generated file for all of it, regenerated whole, installed by the
// operator with a printed sudo command. Servlo stages it somewhere it can
// actually write and never touches /etc.

const (
	// ChrootBase is where the per-site chroots live. Under /srv because it is
	// root-owned server data that is not configuration and not a package's.
	ChrootBase = "/srv/servlo-sftp"
	// SSHDDropIn is the file servlo generates. The 60- prefix puts it after
	// Ubuntu's own drop-ins and after anything a cloud image left behind.
	SSHDDropIn = "/etc/ssh/sshd_config.d/60-servlo-sftp.conf"
	// sshdConfigPath and sshdDropInDir are where the host's own settings live.
	sshdConfigPath = "/etc/ssh/sshd_config"
	sshdDropInDir  = "/etc/ssh/sshd_config.d"
	// firstPort is where servlo starts allocating. Above the registered range
	// an operator is likely to be using, below anything ephemeral.
	firstPort = 2200
	// lastPort bounds the allocation, so a bug cannot walk off into the
	// ephemeral range and collide with an outbound connection.
	lastPort = 2999
)

// Chroot is one site's SFTP arrangement.
type Chroot struct {
	Domain   string `json:"domain"`
	SitePath string `json:"site_path"`
	Port     int    `json:"port"`
}

// Dir is the chroot directory for this site.
func (c Chroot) Dir() string { return ChrootBase + "/" + c.Domain }

// MountPoint is where the site's own directory is bind-mounted, inside the
// chroot. It has to be a level below the chroot itself, because the chroot is
// root-owned and unwritable and the site has to be writable.
func (c Chroot) MountPoint() string { return c.Dir() + "/site" }

// Config renders the sshd drop-in for every site with SFTP enabled.
//
// keepPorts are the ports sshd is already listening on. They are restated
// because declaring any Port in a drop-in replaces sshd's default of 22, and a
// generated file that named only the SFTP ports would take the operator's own
// way in away from them on the next reload.
func Config(chroots []Chroot, keepPorts []int) string {
	if len(chroots) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# Managed by servlo. Do not edit; regenerate with `servlo sftp config`.\n")
	b.WriteString("#\n")
	b.WriteString("# Servlo does not decide how you log in. Nothing here sets\n")
	b.WriteString("# PasswordAuthentication, PermitRootLogin or PubkeyAuthentication, and\n")
	b.WriteString("# nothing here ever will; those stay yours in /etc/ssh/sshd_config.\n")
	b.WriteString("#\n")
	b.WriteString("# Each site listens on its own port because every site on this machine\n")
	b.WriteString("# runs as the same Linux user, so sshd has nothing else to tell them\n")
	b.WriteString("# apart with. A session on one of these ports is chrooted to that site.\n")
	b.WriteString("\n")

	// Every global directive first. A Port line after the first Match falls
	// inside that block, and sshd refuses the file.
	b.WriteString("# The ports sshd already listens on, restated so this file does not\n")
	b.WriteString("# replace them.\n")
	for _, port := range keepPorts {
		fmt.Fprintf(&b, "Port %d\n", port)
	}
	b.WriteString("\n# One port per site with SFTP enabled.\n")
	for _, c := range chroots {
		fmt.Fprintf(&b, "Port %d\n", c.Port)
	}

	for _, c := range chroots {
		fmt.Fprintf(&b, "\n# %s\nMatch LocalPort %d\n", c.Domain, c.Port)
		fmt.Fprintf(&b, "    ChrootDirectory %s\n", c.Dir())
		// -d is relative to the chroot, so the session opens on the site rather
		// than on the empty root above it.
		b.WriteString("    ForceCommand internal-sftp -d /site\n")
		b.WriteString("    PermitTTY no\n")
		b.WriteString("    AllowTcpForwarding no\n")
		b.WriteString("    AllowAgentForwarding no\n")
		b.WriteString("    AllowStreamLocalForwarding no\n")
		b.WriteString("    X11Forwarding no\n")
		b.WriteString("    PermitTunnel no\n")
	}
	return b.String()
}

// Stage writes the generated configuration somewhere servlo can actually write,
// and returns the path, so the operator can read it before installing it and
// the printed command has a real file to name.
func Stage(chroots []Chroot, keepPorts []int) (string, error) {
	dir := filepath.Join(config.ConfigDir(), "sftp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, filepath.Base(SSHDDropIn))
	// 0644 because it is about to be installed as a file sshd reads, and
	// because the operator is going to cat it first.
	if err := os.WriteFile(path, []byte(Config(chroots, keepPorts)), 0o644); err != nil { //nolint:gosec
		return "", err
	}
	return path, nil
}

// Plan is one privileged step, whether it already holds, and what a human has
// to run if not. The shape mirrors internal/serverbasics deliberately: every
// privileged step in servlo is described the same way and executed by nobody.
type Plan struct {
	Name      string   `json:"name"`
	Satisfied bool     `json:"satisfied"`
	Commands  []string `json:"commands"`
	Detail    string   `json:"detail"`
}

// ForHuman renders the commands the way an operator types them, which is with
// sudo in front. Kept separate from Commands so the printed form and the
// operation cannot drift.
func (p Plan) ForHuman() []string {
	out := make([]string, 0, len(p.Commands))
	for _, c := range p.Commands {
		out = append(out, "sudo "+c)
	}
	return out
}

// ChrootPlan is everything an operator has to run to make the confinement real.
//
// user and group are the account servlo runs as, which is the account that owns
// every site on the machine. The mount point is owned by it because the site
// has to be writable over SFTP; the chroot above it is owned by root because
// sshd will not chroot into anything else.
func ChrootPlan(chroots []Chroot, stagedConfig, user, group string) Plan {
	plan := Plan{Name: "sftp chroot"}
	if len(chroots) == 0 {
		plan.Satisfied = true
		plan.Detail = "no site has SFTP enabled, so there is nothing for sshd to do"
		return plan
	}

	plan.Commands = append(plan.Commands, fmt.Sprintf("install -d -o root -g root -m 755 %s", ChrootBase))
	for _, c := range chroots {
		fstab := fmt.Sprintf("%s %s none bind 0 0", c.SitePath, c.MountPoint())
		plan.Commands = append(plan.Commands,
			fmt.Sprintf("install -d -o root -g root -m 755 %s", c.Dir()),
			fmt.Sprintf("install -d -o %s -g %s -m 755 %s", user, group, c.MountPoint()),
			fmt.Sprintf("mountpoint -q %s || mount --bind %s %s", c.MountPoint(), c.SitePath, c.MountPoint()),
			// Persistence is the half that gets forgotten: the bind mount works
			// until the reboot that undoes it, and then SFTP lands in an empty
			// directory with nothing in the logs to explain it.
			fmt.Sprintf("grep -qF %q /etc/fstab || printf '%%s\\n' %q >> /etc/fstab", fstab, fstab),
		)
	}
	plan.Commands = append(plan.Commands,
		fmt.Sprintf("install -m 644 -o root -g root %s %s", stagedConfig, SSHDDropIn),
		// Tested before the reload, so a file servlo generated wrong is refused
		// on the operator's own terminal instead of taking sshd down with it.
		"sshd -t",
		"systemctl reload ssh",
	)
	plan.Detail = fmt.Sprintf("%d site(s) need a root-owned chroot and an sshd block; servlo cannot create either", len(chroots))
	return plan
}

// PortsFile is where the per-site port allocation lives.
func PortsFile() string { return filepath.Join(config.DataDir(), "sftp-ports.yaml") }

type portsFile struct {
	Ports map[string]int `yaml:"ports"`
}

// Ports is the current allocation, domain to port.
func Ports() (map[string]int, error) {
	data, err := os.ReadFile(PortsFile())
	if os.IsNotExist(err) {
		return map[string]int{}, nil
	}
	if err != nil {
		return nil, err
	}
	var file portsFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	if file.Ports == nil {
		file.Ports = map[string]int{}
	}
	return file.Ports, nil
}

// reservedSSHPorts is what sshd already answers on, as a seam so a test does
// not need /etc.
var reservedSSHPorts = ConfiguredSSHPorts

// AssignPort returns the site's port, allocating one the first time.
//
// Stable, because the port is in whatever the operator saved in their SFTP
// client, and a port that moved when an unrelated site was removed would break
// a connection that had nothing to do with the change.
//
// Stable except against sshd. The range this allocates from is exactly where an
// operator who moved sshd off 22 tends to have put it, and a site holding that
// port puts a Match LocalPort block on it: every shell login there becomes a
// chrooted internal-sftp session, and the operator loses the only way into their
// own machine, remotely, from a panel action. So a reserved port is never handed
// out, and one handed out before it was reserved is taken back. A saved
// connection breaking is worth less than a shell.
func AssignPort(domain string) (int, error) {
	ports, err := Ports()
	if err != nil {
		return 0, err
	}
	reserved := map[int]bool{}
	for _, port := range reservedSSHPorts() {
		reserved[port] = true
	}
	if port, ok := ports[domain]; ok && !reserved[port] {
		return port, nil
	}
	taken := map[int]bool{}
	for other, port := range ports {
		if other != domain {
			taken[port] = true
		}
	}
	for port := firstPort; port <= lastPort; port++ {
		if taken[port] || reserved[port] {
			continue
		}
		ports[domain] = port
		return port, savePorts(ports)
	}
	return 0, fmt.Errorf("every port between %d and %d is already allocated or in use by sshd", firstPort, lastPort)
}

// ReleasePort forgets a site, so its port can be handed to another one.
func ReleasePort(domain string) error {
	ports, err := Ports()
	if err != nil {
		return err
	}
	if _, ok := ports[domain]; !ok {
		return nil
	}
	delete(ports, domain)
	return savePorts(ports)
}

func savePorts(ports map[string]int) error {
	data, err := yaml.Marshal(portsFile{Ports: ports})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(PortsFile()), 0o700); err != nil {
		return err
	}
	// Replaced rather than rewritten. This file is the only record of which port
	// a site answers on, and the whole point of it is that the port does not
	// move: a half-written one reads as no allocations at all, and every site
	// would be handed a new port on the next pass.
	return atomicfile.Write(PortsFile(), data, 0o600)
}

// ConfiguredSSHPorts reads the ports sshd is already listening on.
func ConfiguredSSHPorts() []int { return configuredSSHPorts(sshdConfigPath, sshdDropInDir) }

// configuredSSHPorts takes its paths so a test does not need /etc.
//
// Servlo's own drop-in is skipped. Reading its Port lines back would fold the
// SFTP ports into "the ports sshd already uses", and they would then be
// restated as host ports for ever, even for a site that had been removed.
func configuredSSHPorts(mainConfig, dropInDir string) []int {
	found := map[int]bool{}
	files := []string{mainConfig}
	if entries, err := os.ReadDir(dropInDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".conf") || e.Name() == filepath.Base(SSHDDropIn) {
				continue
			}
			files = append(files, filepath.Join(dropInDir, e.Name()))
		}
	}
	for _, path := range files {
		for _, port := range portsIn(path) {
			found[port] = true
		}
	}
	if len(found) == 0 {
		// No Port anywhere means sshd is on its default, and a generated file
		// that forgot to say so would move it.
		return []int{22}
	}
	out := make([]int, 0, len(found))
	for port := range found {
		out = append(out, port)
	}
	sort.Ints(out)
	return out
}

func portsIn(path string) []int {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close() //nolint:errcheck

	var ports []int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || !strings.EqualFold(fields[0], "Port") {
			continue
		}
		if port, err := strconv.Atoi(fields[1]); err == nil && port > 0 && port < 65536 {
			ports = append(ports, port)
		}
	}
	return ports
}
