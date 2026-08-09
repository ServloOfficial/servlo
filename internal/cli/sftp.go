package cli

import (
	"fmt"
	"os"
	"os/user"
	"sort"
	"strings"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/feedback"
	"github.com/realrashid/servlo/internal/sftpaccess"
	"github.com/spf13/cobra"
)

// The SFTP command group.
//
// Two halves, and the command names say which is which. `add`, `remove` and
// `status` are work servlo does: authorised keys in the servlo user's own file,
// additively, no privilege asked for. `setup` is work servlo will not do: it
// prints the root commands that create the chroot and reload sshd, and stops
// there. Nothing in this file runs sudo, which is the rule in CLAUDE.md section
// 3.2 and the reason the printed form and the commands are kept apart in
// internal/sftpaccess.

// NewSFTPCmd returns the `sftp` command group.
func NewSFTPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sftp",
		Short: "Per-site SFTP access: authorised keys, and the root setup to print",
		Long: "Manage SFTP for a site. Servlo authorises keys in its own " +
			"~/.ssh/authorized_keys, restricted to file transfer and opening in the " +
			"site's directory. Confining the session so it cannot leave that " +
			"directory needs sshd's ChrootDirectory, which needs root; `servlo sftp " +
			"setup` prints exactly what to run and never runs it.",
	}
	cmd.AddCommand(newSFTPStatusCmd(), newSFTPAddCmd(), newSFTPRemoveCmd(), newSFTPSetupCmd())
	return cmd
}

func newSFTPStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "status",
		Short:        "Show which sites have SFTP keys and whether sshd confines them",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE:         func(_ *cobra.Command, _ []string) error { return sftpStatus() },
	}
}

func newSFTPAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <domain> <label> <public-key-file>",
		Short: "Authorise a public key for one site",
		Long: "Authorise a public key for one site. The label is how the key is " +
			"named in the panel and in `servlo sftp status`; it may contain letters, " +
			"digits, dots, dashes and underscores. Pass the path to a .pub file, " +
			"never a private key.",
		Args:         cobra.ExactArgs(3),
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return sftpAdd(args[0], args[1], args[2])
		},
	}
}

func newSFTPRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "remove <fingerprint>",
		Short:        "Withdraw an authorised key",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE:         func(_ *cobra.Command, args []string) error { return sftpRemove(args[0]) },
	}
}

func newSFTPSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Print the root commands that confine each site's SFTP session",
		Long: "Print the commands that create each site's root-owned chroot, bind-mount " +
			"the site inside it, install servlo's sshd drop-in and reload sshd. Servlo " +
			"is not root and does not run any of this; copy the block and run it yourself.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE:         func(_ *cobra.Command, _ []string) error { return sftpSetup() },
	}
}

func sftpStatus() error {
	keys, err := sftpaccess.List()
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		feedback.Note("No site has an SFTP key. Add one with: servlo sftp add <domain> <label> <key.pub>")
		return nil
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Site != keys[j].Site {
			return keys[i].Site < keys[j].Site
		}
		return keys[i].Label < keys[j].Label
	})
	ports, err := sftpaccess.Ports()
	if err != nil {
		return err
	}

	feedback.Header("SFTP keys")
	site := ""
	for _, key := range keys {
		if key.Site != site {
			site = key.Site
			fmt.Printf("\n%s  (port %d)\n", site, ports[site])
		}
		fmt.Printf("  %-24s %s %s\n", key.Label, key.Type, key.Fingerprint)
	}

	fmt.Println()
	if confined, detail := sftpConfinement(); confined {
		feedback.Done(detail)
	} else {
		feedback.Warn("%s", detail)
	}
	return nil
}

// sftpConfinement reads whether the chroot block servlo generated is the one
// installed. Both facts are readable without any privilege at all.
func sftpConfinement() (bool, string) {
	chroots, keepPorts, err := sftpChroots()
	if err != nil {
		return false, "could not work out the chroot state: " + err.Error()
	}
	installed, err := os.ReadFile(sftpaccess.SSHDDropIn)
	if err != nil {
		return false, "sshd is not confining these sessions yet, so a key can reach the whole account; run `servlo sftp setup`"
	}
	if string(installed) != sftpaccess.Config(chroots, keepPorts) {
		return false, sftpaccess.SSHDDropIn + " no longer matches what servlo would generate, so a site added since is not confined; run `servlo sftp setup`"
	}
	return true, "sshd is chrooting each of these sessions to its own site"
}

func sftpAdd(domain, label, keyFile string) error {
	site, err := config.FindSiteByDomain(domain)
	if err != nil {
		return fmt.Errorf("no site called %s", domain)
	}
	data, err := os.ReadFile(keyFile)
	if err != nil {
		return fmt.Errorf("reading %s: %w", keyFile, err)
	}
	if strings.Contains(string(data), "PRIVATE KEY") {
		return fmt.Errorf("%s is a private key; pass the matching .pub file", keyFile)
	}
	port, err := sftpaccess.AssignPort(domain)
	if err != nil {
		return err
	}
	key, err := sftpaccess.Add(domain, site.Path, label, strings.TrimSpace(string(data)))
	if err != nil {
		return err
	}
	feedback.Done(fmt.Sprintf("authorised %s (%s) for %s", key.Label, key.Fingerprint, domain))
	if confined, detail := sftpConfinement(); confined {
		feedback.Note(fmt.Sprintf("connect with: sftp -P %d %s@<this server>", port, sftpUsername()))
	} else {
		feedback.Warn("%s", detail)
	}
	return nil
}

func sftpRemove(fingerprint string) error {
	removed, err := sftpaccess.Remove(fingerprint)
	if err != nil {
		return err
	}
	if !removed {
		return fmt.Errorf("no key with the fingerprint %s is authorised", fingerprint)
	}
	feedback.Done("withdrew " + fingerprint)
	return nil
}

func sftpSetup() error {
	chroots, keepPorts, err := sftpChroots()
	if err != nil {
		return err
	}
	if len(chroots) == 0 {
		feedback.Note("No site has an SFTP key, so there is nothing for sshd to do.")
		return nil
	}
	staged, err := sftpaccess.Stage(chroots, keepPorts)
	if err != nil {
		return err
	}
	name, group := currentUserAndGroup()
	plan := sftpaccess.ChrootPlan(chroots, staged, name, group)

	feedback.Header("SFTP confinement")
	fmt.Println("Servlo has written the sshd configuration to:")
	fmt.Println("  " + staged)
	fmt.Println()
	fmt.Println("Read it, then run this as root. Servlo will not run it for you.")
	fmt.Println()
	for _, line := range plan.ForHuman() {
		fmt.Println("  " + line)
	}
	fmt.Println()
	fmt.Println("Until you do, an authorised key opens in the site's directory but is")
	fmt.Println("not confined to it. Every site here runs as the same Linux account, so")
	fmt.Println("the chroot is what keeps one session out of another site's files.")
	return nil
}

// sftpChroots is every site with at least one key, with its port.
func sftpChroots() ([]sftpaccess.Chroot, []int, error) {
	keys, err := sftpaccess.List()
	if err != nil {
		return nil, nil, err
	}
	ports, err := sftpaccess.Ports()
	if err != nil {
		return nil, nil, err
	}
	seen := map[string]bool{}
	var chroots []sftpaccess.Chroot
	for _, key := range keys {
		if seen[key.Site] {
			continue
		}
		seen[key.Site] = true
		site, err := config.FindSiteByDomain(key.Site)
		if err != nil {
			continue
		}
		chroots = append(chroots, sftpaccess.Chroot{Domain: key.Site, SitePath: site.Path, Port: ports[key.Site]})
	}
	sort.Slice(chroots, func(i, j int) bool { return chroots[i].Domain < chroots[j].Domain })
	return chroots, sftpaccess.ConfiguredSSHPorts(), nil
}

// sftpUsername is the account an SFTP session logs in as, which is servlo's own
// and the same one for every site.
func sftpUsername() string {
	name, _ := currentUserAndGroup()
	return name
}

func currentUserAndGroup() (string, string) {
	u, err := user.Current()
	if err != nil {
		return "servlo", "servlo"
	}
	group := u.Username
	if g, err := user.LookupGroupId(u.Gid); err == nil {
		group = g.Name
	}
	return u.Username, group
}
