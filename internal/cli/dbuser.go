package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/dbconn"
	"github.com/realrashid/servlo/internal/dbcred"
	"github.com/realrashid/servlo/internal/dbuser"
	"github.com/realrashid/servlo/internal/feedback"
	"github.com/realrashid/servlo/internal/sitetpl"
	"github.com/spf13/cobra"
)

// newDbUserCmd is `servlo db user`: what a site reaches its database as, and
// how to give it a new password.
func newDbUserCmd(use string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use,
		Short: "The database account a site reaches its database as",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runDbUserShow(siteRefOrCwd(args))
		},
	}
	cmd.AddCommand(newDbUserRotateCmd())
	return cmd
}

func newDbUserRotateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rotate [site]",
		Short: "Give the site's database account a new password",
		Long: "Issues a new password for the site's own database account, updates it on the " +
			"database server, and writes it into the site's env file. The application picks " +
			"it up at its next deploy or PHP-FPM reload; until then it keeps serving with the " +
			"connection it already has open.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runDbUserRotate(siteRefOrCwd(args))
		},
	}
}

// siteRefOrCwd is the site named on the command line, or the one the shell is
// standing in.
func siteRefOrCwd(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	if site, err := config.FindSiteByPath(cwd); err == nil {
		return site.Name
	}
	return ""
}

// siteAndConnection resolves a site reference to the site, the connection its
// database is on, and the database it owns there.
func siteAndConnection(ref string) (*config.Site, dbconn.Connection, string, error) {
	if strings.TrimSpace(ref) == "" {
		return nil, dbconn.Connection{}, "", fmt.Errorf("name a site, or run this from inside one")
	}
	site, err := config.FindSiteByRef(ref)
	if err != nil {
		return nil, dbconn.Connection{}, "", err
	}
	conn, err := dbconn.Named(site.Database)
	if err != nil {
		return nil, dbconn.Connection{}, "", err
	}
	// sitetpl.DBName, not the site name: a site sharing a group main's database
	// owns the account on that database rather than one of its own.
	return site, conn, sitetpl.DBName(site.Path), nil
}

func runDbUserShow(ref string) error {
	site, conn, database, err := siteAndConnection(ref)
	if err != nil {
		return err
	}
	cred, ok := dbcred.For(conn, database)
	if !ok {
		fmt.Printf("%s reaches %s on %s as %s, the connection's administrator.\n",
			site.Name, database, dbuser.Address(conn), conn.User)
		fmt.Printf("Run `servlo env` in the site to give it an account of its own, or `servlo db user rotate %s`.\n", site.Name)
		return nil
	}
	fmt.Printf("%s reaches %s on %s as %s, granted on its own schemas.\n",
		site.Name, database, dbuser.Address(conn), cred.User)
	fmt.Println("The password is in the site's env file; servlo does not print it.")
	return nil
}

func runDbUserRotate(ref string) error {
	site, conn, database, err := siteAndConnection(ref)
	if err != nil {
		return err
	}
	if _, err := dbuser.Rotate(conn, database); err != nil {
		return err
	}
	fmt.Printf("Rotated the password for %s's database account on %s.\n", site.Name, dbuser.Address(conn))

	keys, err := dbuser.WriteEnv(site)
	if err != nil {
		// The server has the new password and the env file does not, which is
		// the one state worth being loud about: the site will fail to connect
		// at its next reload until somebody writes it in.
		feedback.Warn("the new password is on the server but not in the site's env file: %v", err)
		return err
	}
	sort.Strings(keys)
	fmt.Printf("Wrote %s. The application picks the new password up at its next deploy or PHP-FPM reload.\n", strings.Join(keys, ", "))
	return nil
}
