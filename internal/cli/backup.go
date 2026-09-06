package cli

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ServloOfficial/servlo/internal/backup"
	"github.com/ServloOfficial/servlo/internal/backupdest"
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/dbconn"
	"github.com/ServloOfficial/servlo/internal/dbdump"
	"github.com/ServloOfficial/servlo/internal/feedback"
	"github.com/ServloOfficial/servlo/internal/version"
	"github.com/spf13/cobra"
)

// NewBackupCmd is `servlo backup`: take one, and see what is already here.
func NewBackupCmd() *cobra.Command {
	var filesOnly bool
	cmd := &cobra.Command{
		Use:   "backup [site]",
		Short: "Back up a site: its files and its database, encrypted",
		Long: "Writes one encrypted archive holding the site's files and a dump of its database. " +
			"What it leaves out comes from the framework's definition, which names only what a " +
			"deploy puts back.\n\n" +
			"The archive can only be opened with this server's backup key. Copy that key " +
			"somewhere off this machine: without it the backups are not recoverable, and that " +
			"is the point of them being encrypted.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			feedback.Begin()
			return runBackup(siteRefOrCwd(args), filesOnly)
		},
	}
	cmd.Flags().BoolVar(&filesOnly, "files-only", false,
		"Back up the files and skip the database, for a site whose data is backed up elsewhere")
	cmd.AddCommand(newBackupListCmd(), newBackupKeyCmd(), newBackupScheduleCmd(), newBackupVerifyCmd(), newBackupStateCmd(), newBackupDestCmd())
	return cmd
}

func runBackup(ref string, filesOnly bool) error {
	if ref == "" {
		return fmt.Errorf("which site? Run this from a site's directory, or name one")
	}
	site, err := config.FindSiteByRef(ref)
	if err != nil {
		return fmt.Errorf("site %q not found", ref)
	}

	runner := backup.ForSites()
	if filesOnly {
		// Not a fallback and not an error: some sites have their data backed up
		// by the provider, and the archive says which it is so a restore never
		// mistakes one for the other.
		runner.Dump = nil
	}

	step := feedback.Start("backing up " + site.Name)
	rec, err := runner.Run(site)
	// Reported whichever way it went, because this is what the nightly timer
	// runs and nobody reads a journal at three in the morning.
	backup.Report(site.Name, rec, err)
	if err != nil {
		step.Fail(err)
		return err
	}
	step.OK(feedback.Val(filepath.Base(rec.Path)))

	what := fmt.Sprintf("%d %s", rec.Manifest.Files, plural(rec.Manifest.Files, "file", "files"))
	if rec.Manifest.Database {
		what += " and " + rec.Manifest.DatabaseName
	} else {
		what += ", no database"
	}
	fmt.Printf("  %s, %s on disk\n", what, humanSize(rec.Size))
	fmt.Printf("  %s\n", rec.Path)
	if rec.Pruned > 0 {
		fmt.Printf("  Retention removed %d older %s.\n", rec.Pruned, plural(rec.Pruned, "archive", "archives"))
	}
	for _, err := range rec.SendErrors {
		// The archive is on this server and usable. What failed is the copy
		// going elsewhere, and calling the backup failed would have an operator
		// re-running one that worked.
		feedback.Warn("the backup was written, but a destination could not be reached: %v", err)
	}
	if rec.PruneError != nil {
		// The backup is on disk. Only the tidying up failed, and saying so
		// without calling the backup failed is the difference between an
		// operator freeing some disk and an operator re-running a good backup.
		feedback.Warn("the backup was written, but clearing older ones failed: %v", rec.PruneError)
	}
	if !rec.Manifest.Database {
		// Two different situations that must not read the same. One is a
		// choice; the other is servlo telling you something you may not know.
		if filesOnly {
			fmt.Println("  Files only, as asked. The database was not touched.")
		} else {
			fmt.Println("  This site has no database servlo can name, so the archive holds files only.")
		}
	}
	return nil
}

func newBackupListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [site]",
		Short: "The backups on this server",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			var only string
			if len(args) > 0 {
				if site, err := config.FindSiteByRef(args[0]); err == nil {
					only = config.SiteSlug(site.Name)
				} else {
					only = config.SiteSlug(args[0])
				}
			}
			return runBackupList(only)
		},
	}
}

func runBackupList(only string) error {
	dir := config.SiteBackupsDir()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		fmt.Println("No backups yet. Take one with `servlo backup`.")
		return nil
	}
	if err != nil {
		return err
	}

	type row struct{ name, size string }
	var rows []row
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), backup.Extension) {
			continue
		}
		if only != "" && !strings.HasPrefix(e.Name(), only+"-") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		rows = append(rows, row{e.Name(), humanSize(info.Size())})
	}
	if len(rows) == 0 {
		fmt.Println("No backups yet. Take one with `servlo backup`.")
		return nil
	}
	// Newest first: the archive an operator wants during an incident is almost
	// always the most recent, and the name sorts by time already.
	sort.Slice(rows, func(i, j int) bool { return rows[i].name > rows[j].name })
	for _, r := range rows {
		fmt.Printf("  %-52s %10s\n", r.name, r.size)
	}
	fmt.Printf("\n  in %s\n", dir)
	return nil
}

func newBackupKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "key",
		Short: "Where this server's backup key is, and why it matters",
		Long: "Every backup this server writes is encrypted with one key, and nothing else can " +
			"open them. Copy it somewhere off this machine now rather than after the machine " +
			"you would be restoring from is gone.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if _, err := backup.Key(); err != nil {
				return err
			}
			fmt.Printf("  %s\n\n", backup.KeyPath())
			fmt.Println("  Keep a copy off this server. Without this key the backups cannot be")
			fmt.Println("  opened, by you or by anyone else, which is what makes them safe to")
			fmt.Println("  store somewhere you do not control.")
			return nil
		},
	}
	cmd.AddCommand(newBackupKeyImportCmd())
	return cmd
}

// newBackupKeyImportCmd is the first step of a rebuild. The archives are
// already encrypted with the old server's key, so nothing can be restored until
// it is here.
func newBackupKeyImportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import <key>",
		Short: "Bring the backup key over from another server",
		Long: "The archives were encrypted with that server's key and nothing else opens them, " +
			"so this is the first thing to do when rebuilding onto a new droplet.\n\n" +
			"An existing key is never overwritten. If this server has already written backups " +
			"of its own, that key is the only thing that opens them.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			feedback.Begin()
			if err := backup.ImportKey(args[0]); err != nil {
				return err
			}
			feedback.Start("importing the backup key").OK(feedback.Val(backup.KeyPath()))
			return nil
		},
	}
}

func newBackupScheduleCmd() *cobra.Command {
	var off bool
	var verify string
	var keepDaily, keepWeekly, keepMonthly int
	cmd := &cobra.Command{
		Use:   "schedule [site] [when]",
		Short: "Back this site up on a schedule",
		Long: "Arms a systemd timer for the site. The schedule can be written as a crontab " +
			"line, an @shorthand, or a systemd calendar expression: \"30 3 * * *\", \"daily\" " +
			"and \"*-*-* 03:30:00\" all work.\n\n" +
			"The timer catches up after a reboot, so a droplet that was off overnight still " +
			"takes the backup it missed rather than skipping the day.",
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			feedback.Begin()
			// One argument is ambiguous: `backup schedule acme` names a site,
			// `backup schedule daily` names a schedule for the site the shell
			// is in. It is settled by asking the registry, which is the same
			// thing an operator does in their head.
			var ref, when string
			switch {
			case len(args) == 2:
				ref, when = args[0], args[1]
			case len(args) == 1:
				if _, err := config.FindSiteByRef(args[0]); err == nil {
					ref = args[0]
				} else {
					ref, when = siteRefOrCwd(nil), args[0]
				}
			default:
				ref = siteRefOrCwd(nil)
			}
			keep := &config.BackupKeep{Daily: keepDaily, Weekly: keepWeekly, Monthly: keepMonthly}
			if !cmd.Flags().Changed("daily") && !cmd.Flags().Changed("weekly") && !cmd.Flags().Changed("monthly") {
				keep = nil
			}
			return runBackupSchedule(ref, when, verify, off, keep)
		},
	}
	cmd.Flags().BoolVar(&off, "off", false, "Stop backing this site up on a schedule")
	cmd.Flags().StringVar(&verify, "verify", "",
		"Also test-restore the newest backup on this schedule (e.g. \"Sun *-*-* 04:00:00\", or \"weekly\")")
	cmd.Flags().IntVar(&keepDaily, "daily", backup.DefaultPolicy.Daily, "How many daily backups to keep")
	cmd.Flags().IntVar(&keepWeekly, "weekly", backup.DefaultPolicy.Weekly, "How many weekly backups to keep")
	cmd.Flags().IntVar(&keepMonthly, "monthly", backup.DefaultPolicy.Monthly, "How many monthly backups to keep")
	return cmd
}

func runBackupSchedule(ref, when, verify string, off bool, keep *config.BackupKeep) error {
	if ref == "" {
		return fmt.Errorf("which site? Run this from a site's directory, or name one")
	}
	site, err := config.FindSiteByRef(ref)
	if err != nil {
		return fmt.Errorf("site %q not found", ref)
	}

	switch {
	case off:
		site.Backup = nil
	case when == "":
		return showBackupSchedule(site)
	default:
		calendar, err := backup.NormalizeSchedule(when)
		if err != nil {
			return err
		}
		next := &config.SiteBackup{Schedule: calendar, Keep: keep}
		if site.Backup != nil {
			// Setting a schedule again should not silently switch off a check
			// that was already arranged.
			next.Verify = site.Backup.Verify
		}
		if verify != "" {
			check, err := backup.NormalizeSchedule(verify)
			if err != nil {
				return err
			}
			next.Verify = check
		}
		site.Backup = next
	}

	if err := config.AddSite(*site); err != nil {
		return err
	}
	if err := backup.ApplySchedule(*site, servloBinaryPath()); err != nil {
		return err
	}
	if off {
		feedback.Start("unscheduling backups for " + site.Name).OK("")
		return nil
	}
	feedback.Start("scheduling backups for " + site.Name).OK(feedback.Val(site.Backup.Schedule))
	return showBackupRetention(site)
}

func showBackupSchedule(site *config.Site) error {
	if site.Backup == nil || site.Backup.Schedule == "" {
		fmt.Printf("  %s is not backed up on a schedule.\n", site.Name)
		fmt.Printf("  Arm one with `servlo backup schedule %s daily`.\n", site.Name)
		return nil
	}
	state := site.Backup.Schedule
	if site.Backup.Disabled {
		state += " (switched off)"
	}
	fmt.Printf("  %s backs up %s\n", site.Name, state)
	if site.Backup.Verify != "" {
		fmt.Printf("  Test restore %s\n", site.Backup.Verify)
	} else {
		fmt.Println("  No scheduled test restore. A backup nothing has ever restored is not known to work.")
	}
	return showBackupRetention(site)
}

func showBackupRetention(site *config.Site) error {
	p := backup.SitePolicy(site)
	fmt.Printf("  Keeping %d daily, %d weekly, %d monthly\n", p.Daily, p.Weekly, p.Monthly)
	return nil
}

// plural is for one line of output, not a translation layer.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// servloBinaryPath is what the timer's unit runs. An absolute path, because a
// unit that resolves servlo from PATH stops working the first time PATH
// changes, and the way that failure shows up is a backup that silently stopped
// happening months ago.
func servloBinaryPath() string {
	if self, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(self); err == nil {
			return resolved
		}
		return self
	}
	return filepath.Join(config.BinDir(), "servlo")
}

func newBackupVerifyCmd() *cobra.Command {
	var latest bool
	cmd := &cobra.Command{
		Use:   "verify <archive>",
		Short: "Restore a backup into a scratch database and check what came back",
		Long: "Opens the archive, restores its dump into a database of its own, counts what " +
			"arrived, and drops the scratch database again. The site's own database is never " +
			"touched.\n\n" +
			"A backup that has never been restored is not a backup. Most panels ship a green " +
			"tick that means a file was written, which says nothing about whether it can be " +
			"put back, and the day you find out is the day it matters.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			feedback.Begin()
			ref := args[0]
			if latest {
				site, err := config.FindSiteByRef(ref)
				if err != nil {
					return fmt.Errorf("site %q not found", ref)
				}
				ref, err = backup.Newest(config.SiteBackupsDir(), config.SiteSlug(site.Name))
				if err != nil {
					return err
				}
			}
			return runBackupVerify(ref)
		},
	}
	cmd.Flags().BoolVar(&latest, "latest", false,
		"Treat the argument as a site and check its newest backup, which is what the scheduled check does")
	return cmd
}

func runBackupVerify(ref string) error {
	path, err := resolveArchive(ref)
	if err != nil {
		return err
	}
	key, err := backup.Key()
	if err != nil {
		return err
	}
	man, err := readArchiveManifest(path, key)
	if err != nil {
		return err
	}
	if !man.Database {
		fmt.Printf("  %s holds no database, so there is nothing to restore and check.\n", filepath.Base(path))
		fmt.Println("  Its files were verified by opening the archive: it decrypts and it is complete.")
		return nil
	}

	site, err := config.FindSite(man.Site)
	if err != nil {
		return fmt.Errorf("this archive is of %q, which is not a site on this server, so servlo does not know which database server to check it against", man.Site)
	}
	conn, err := dbconn.Named(site.Database)
	if err != nil {
		return err
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck

	scratch := dbdump.ScratchName(man.Site)
	step := feedback.Start("restoring " + filepath.Base(path) + " into " + scratch)

	pr, pw := io.Pipe()
	errc := make(chan error, 1)
	go func() {
		_, err := backup.OpenDump(f, key, pw)
		_ = pw.CloseWithError(err)
		errc <- err
	}()
	res, verifyErr := dbdump.Verify(conn, scratch, pr)
	dumpErr := <-errc
	// A check that runs weekly on a timer reports what it found, the same way
	// the backup itself does. An unverifiable backup that only ever appeared in
	// a journal is the failure this whole story exists to catch.
	backup.ReportVerify(man.Site, filepath.Base(path), cmp.Or(dumpErr, verifyErr))
	if dumpErr != nil {
		step.Fail(dumpErr)
		return dumpErr
	}
	if verifyErr != nil {
		step.Fail(verifyErr)
		return verifyErr
	}
	step.OK(feedback.Val(fmt.Sprintf("%d %s", res.Tables, plural(res.Tables, "table", "tables"))))
	fmt.Printf("  This backup restores. The scratch database was dropped.\n")
	return nil
}

func newBackupStateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "state",
		Short: "Back up servlo's own configuration and the site registry",
		Long: "Everything servlo knows that is not a site's files or data: the registry, the " +
			"connections, the per-site database accounts, the SMTP settings, the provider " +
			"certificates and every per-site setting.\n\n" +
			"This is what makes a rebuild onto a fresh droplet possible. Restoring a site's " +
			"archive alone gives you the files and the database on a server with no idea what " +
			"a site is.\n\n" +
			"The backup key is deliberately not in it. It is what opens this archive, so " +
			"putting it inside would be locking the door and taping the key to the front.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			feedback.Begin()
			return runBackupState()
		},
	}
}

func runBackupState() error {
	key, err := backup.Key()
	if err != nil {
		return err
	}
	step := feedback.Start("backing up this server's own state")
	path, man, size, err := backup.WriteState(config.SiteBackupsDir(), key, backup.StateOptions{Version: version.Version})
	if err != nil {
		step.Fail(err)
		return err
	}
	step.OK(feedback.Val(filepath.Base(path)))
	fmt.Printf("  %d %s, %s on disk\n", man.Files, plural(man.Files, "file", "files"), humanSize(size))
	fmt.Printf("  %s\n", path)
	fmt.Println("  The backup key is not in here. Keep a copy of it somewhere else, or this")
	fmt.Println("  archive is not recoverable either.")
	return nil
}

func newBackupDestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "destination",
		Aliases: []string{"destinations", "dest"},
		Short:   "Where finished archives are copied to",
		Long: "A backup that only exists on the machine it is a backup of is not a backup. " +
			"Archives are copied to every destination configured here as soon as they are " +
			"written.\n\n" +
			"Two kinds: S3-compatible storage, which covers DigitalOcean Spaces and Amazon S3 " +
			"with one driver, and SFTP to another server.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error { return runDestList() },
	}
	cmd.AddCommand(newDestAddCmd(), newDestRemoveCmd(), newDestTestCmd())
	return cmd
}

func runDestList() error {
	reg, err := backupdest.Load()
	if err != nil {
		return err
	}
	if len(reg.Destinations) == 0 {
		fmt.Println("  No destinations. Archives stay on this server only, which is not an")
		fmt.Println("  offsite backup. Add one with `servlo backup destination add`.")
		return nil
	}
	for _, d := range reg.Destinations {
		fmt.Printf("  %-16s %-6s %s\n", d.Name, d.Kind, destWhere(d))
	}
	return nil
}

func destWhere(d backupdest.Destination) string {
	if d.Kind == backupdest.KindS3 {
		where := d.Bucket
		if d.Prefix != "" {
			where += "/" + strings.Trim(d.Prefix, "/")
		}
		return where + " at " + d.Endpoint
	}
	return d.User + "@" + d.Host + ":" + d.Path
}

func newDestAddCmd() *cobra.Command {
	var d backupdest.Destination
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a destination archives are copied to",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			feedback.Begin()
			d.Name = args[0]
			if err := backupdest.Add(d); err != nil {
				return err
			}
			feedback.Start("adding " + d.Name).OK(feedback.Val(destWhere(d)))
			fmt.Println("  Take a backup, or run `servlo backup destination test` to check it now.")
			return nil
		},
	}
	cmd.Flags().StringVar(&d.Kind, "kind", backupdest.KindS3, "s3 or sftp")
	cmd.Flags().StringVar(&d.Bucket, "bucket", "", "S3 bucket")
	cmd.Flags().StringVar(&d.Prefix, "prefix", "", "S3 prefix, so one bucket can hold several servers")
	cmd.Flags().StringVar(&d.Endpoint, "endpoint", "", "S3 endpoint, e.g. https://fra1.digitaloceanspaces.com")
	cmd.Flags().StringVar(&d.Region, "region", "", "S3 region")
	cmd.Flags().StringVar(&d.AccessKey, "access-key", "", "S3 access key")
	cmd.Flags().StringVar(&d.SecretKey, "secret-key", "", "S3 secret key")
	cmd.Flags().StringVar(&d.Host, "host", "", "SFTP host")
	cmd.Flags().IntVar(&d.Port, "port", 0, "SFTP port (default 22)")
	cmd.Flags().StringVar(&d.User, "user", "", "SFTP user")
	cmd.Flags().StringVar(&d.Path, "path", "", "SFTP directory to write into")
	cmd.Flags().StringVar(&d.KeyFile, "key-file", "", "SFTP private key")
	return cmd
}

func newDestRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Stop copying archives to a destination",
		Long: "The archives already there are left alone. Deleting somebody's offsite copies " +
			"as a side effect of editing a setting is never what was meant.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			feedback.Begin()
			if err := backupdest.Remove(args[0]); err != nil {
				return err
			}
			feedback.Start("removing " + args[0]).OK("")
			fmt.Println("  The archives already at that destination were left where they are.")
			return nil
		},
	}
}

func newDestTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test [name]",
		Short: "Check a destination can be reached",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			feedback.Begin()
			reg, err := backupdest.Load()
			if err != nil {
				return err
			}
			var failed bool
			for _, d := range reg.Destinations {
				if len(args) > 0 && !strings.EqualFold(d.Name, args[0]) {
					continue
				}
				step := feedback.Start("reaching " + d.Name)
				names, err := backupdest.List(d)
				if err != nil {
					step.Fail(err)
					failed = true
					continue
				}
				step.OK(feedback.Val(fmt.Sprintf("%d %s there", len(names), plural(len(names), "archive", "archives"))))
			}
			if failed {
				return fmt.Errorf("a destination could not be reached")
			}
			return nil
		},
	}
}
