package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/realrashid/servlo/internal/backup"
	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/dbconn"
	"github.com/realrashid/servlo/internal/dbdump"
	"github.com/realrashid/servlo/internal/feedback"
	"github.com/realrashid/servlo/internal/sitetpl"
	"github.com/spf13/cobra"
)

// NewRestoreCmd is `servlo restore`: put an archive back.
func NewRestoreCmd() *cobra.Command {
	var into string
	var filesOnly bool
	var yes bool
	var stateOnly bool
	cmd := &cobra.Command{
		Use:   "restore <archive>",
		Short: "Restore a site from a backup archive",
		Long: "Writes the archive's files over the site's directory and, unless --files-only, " +
			"loads its database dump.\n\n" +
			"This overwrites a live site. Anything in the directory that was not in the " +
			"archive stays where it is, and anything that was is replaced, so a restore is " +
			"not a way to undo a file that was added since.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			feedback.Begin()
			if stateOnly {
				return runRestoreState(args[0], yes)
			}
			return runRestore(args[0], into, filesOnly, yes)
		},
	}
	cmd.Flags().StringVar(&into, "into", "", "Restore into this directory instead of the site's own")
	cmd.Flags().BoolVar(&filesOnly, "files-only", false, "Restore the files and leave the database alone")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip the confirmation")
	cmd.Flags().BoolVar(&stateOnly, "state", false,
		"Restore a servlo state archive: the registry and every setting, not a site")
	return cmd
}

func runRestore(archivePath, into string, filesOnly, yes bool) error {
	path, err := resolveArchive(archivePath)
	if err != nil {
		return err
	}
	key, err := backup.Key()
	if err != nil {
		return err
	}

	// Read what the archive says before anything is written, so a restore into
	// the wrong place is refused rather than discovered afterwards.
	man, err := readArchiveManifest(path, key)
	if err != nil {
		return err
	}

	if man.Kind == backup.KindState {
		return fmt.Errorf("this is a backup of the server's own state, not of a site. Restore it with: servlo restore %s --state", filepath.Base(path))
	}

	target := into
	var site *config.Site
	if target == "" {
		site, err = config.FindSite(man.Site)
		if err != nil {
			return fmt.Errorf("this archive is of %q, which is not a site on this server. "+
				"Name a directory with --into to restore it somewhere", man.Site)
		}
		target = site.Path
	}

	fmt.Printf("  %s, taken %s\n", man.Site, man.Taken.Local().Format("2 Jan 2006 15:04"))
	fmt.Printf("  %d files", man.Files)
	if man.Database {
		fmt.Printf(", database %s", man.DatabaseName)
	} else {
		fmt.Print(", no database")
	}
	fmt.Printf("\n  into %s\n\n", target)

	if !yes && !confirmByTyping(man.Site) {
		fmt.Println("  Nothing was restored.")
		return nil
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck

	step := feedback.Start("restoring files")
	if _, err := backup.RestoreFiles(f, key, target); err != nil {
		step.Fail(err)
		return err
	}
	step.OK(feedback.Val(target))

	if filesOnly || !man.Database {
		if man.Database {
			fmt.Println("  The database in this archive was left alone.")
		}
		return nil
	}
	return restoreDatabase(path, key, site, man)
}

// restoreDatabase loads the archive's dump back into the site's database.
func restoreDatabase(path string, key []byte, site *config.Site, man backup.Manifest) error {
	if site == nil {
		fmt.Println("  Restored somewhere other than a registered site, so the database was left alone.")
		return nil
	}
	conn, err := dbconn.Named(site.Database)
	if err != nil {
		return fmt.Errorf("the files are restored, but servlo cannot tell which database to load into: %w", err)
	}
	database := sitetpl.DBName(site.Path)
	if database == "" {
		return fmt.Errorf("the files are restored, but this site names no database to load into")
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck

	step := feedback.Start("loading " + database)
	// The dump is piped from the archive straight into the engine's client, so
	// a database larger than the droplet's memory never lands anywhere in
	// between. Closing the writer with the unpacking error is what makes the
	// client stop rather than sit waiting on a stream that will not finish.
	pr, pw := io.Pipe()
	errc := make(chan error, 1)
	go func() {
		_, err := backup.OpenDump(f, key, pw)
		_ = pw.CloseWithError(err)
		errc <- err
	}()
	loadErr := dbdump.Load(conn, database, pr)
	dumpErr := <-errc
	if dumpErr != nil {
		step.Fail(dumpErr)
		return dumpErr
	}
	if loadErr != nil {
		step.Fail(loadErr)
		return loadErr
	}
	step.OK(feedback.Val(man.DatabaseName))
	return nil
}

// resolveArchive accepts a path, or the bare name of an archive in the usual
// place, because that is what `servlo backup list` prints.
func resolveArchive(ref string) (string, error) {
	if _, err := os.Stat(ref); err == nil {
		return ref, nil
	}
	candidate := filepath.Join(config.SiteBackupsDir(), ref)
	if !strings.HasSuffix(candidate, backup.Extension) {
		candidate += backup.Extension
	}
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	return "", fmt.Errorf("no archive at %s, and none called %q in %s", ref, ref, config.SiteBackupsDir())
}

func readArchiveManifest(path string, key []byte) (backup.Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return backup.Manifest{}, err
	}
	defer f.Close() //nolint:errcheck
	return backup.ReadManifest(f, key)
}

// confirmByTyping makes the operator write the site's name out.
//
// A y/n prompt is muscle memory by the time anyone reaches a restore, and this
// one overwrites a live site. Typing the name is the friction that makes it a
// decision rather than a reflex.
func confirmByTyping(site string) bool {
	return confirmTyping("This overwrites the live site.", site)
}

func confirmTyping(what, word string) bool {
	fmt.Printf("  %s Type %s to continue: ", what, word)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false
	}
	return strings.TrimSpace(line) == word
}

// runRestoreState puts the server's own configuration back, which is the first
// step of a rebuild: without it a restored site lands on a server that does not
// know it exists.
func runRestoreState(ref string, yes bool) error {
	path, err := resolveArchive(ref)
	if err != nil {
		return err
	}
	// Whether the key existed before this call decides how a failure to open
	// the archive is explained.
	_, statErr := os.Stat(backup.KeyPath())
	fresh := os.IsNotExist(statErr)
	key, err := backup.Key()
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck

	fmt.Printf("  %s\n", filepath.Base(path))
	fmt.Printf("  into %s and %s\n\n", config.ConfigDir(), filepath.Dir(config.SitesFile()))
	if !yes && !confirmTyping("This replaces this server's configuration.", "restore") {
		fmt.Println("  Nothing was restored.")
		return nil
	}

	step := feedback.Start("restoring this server's state")
	man, err := backup.RestoreState(f, key, config.ConfigDir(), filepath.Dir(config.SitesFile()))
	if err != nil {
		step.Fail(err)
		if fresh {
			// The commonest way to arrive here: a rebuild on a new droplet,
			// where servlo has just generated a key of its own that opens
			// nothing. Printed rather than wrapped, because the step above has
			// already reported the failure and a wrapped error is not shown
			// again.
			fmt.Println()
			fmt.Println("  This server had no backup key, so servlo generated one just now, and it is")
			fmt.Println("  not the key this archive was written with. Bring the original over first:")
			fmt.Printf("    servlo backup key import <key from the old server's %s>\n", backup.KeyPath())
		}
		return err
	}
	step.OK(feedback.Val(fmt.Sprintf("%d files", man.Files)))
	fmt.Println("  Certificates are not in a backup and are reissued, so point DNS at this")
	fmt.Println("  server first and then run servlo secure for each site.")
	fmt.Println("  A managed database needs this server's address added to its trusted sources.")
	return nil
}
