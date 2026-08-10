package cli

import (
	"fmt"
	"strings"

	"github.com/realrashid/servlo/internal/appinstall"
	"github.com/realrashid/servlo/internal/appstore"
	"github.com/realrashid/servlo/internal/feedback"
	"github.com/spf13/cobra"
)

// NewAppsCmd is `servlo apps`: the fourth way to make a site, alongside an
// existing folder, a ZIP and a clone.
func NewAppsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apps",
		Short: "Install an application as a new site",
		Long: "An app is a pinned, checksummed release servlo fetches, configures and registers " +
			"as a site of its own.\n\n" +
			"What an install finishes with depends on the application. Where its own installer is " +
			"a single form, servlo posts it and the site is ready to log into. Where it is not, " +
			"servlo says so and leaves that step to you, because an installer nobody has completed " +
			"is an installer anybody who reaches it can complete.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error { return runAppsList() },
	}
	cmd.AddCommand(newAppsInstallCmd())
	return cmd
}

func runAppsList() error {
	apps, err := appstore.List()
	if err != nil {
		return err
	}
	if len(apps) == 0 {
		fmt.Println("The app store is empty.")
		return nil
	}
	for _, app := range apps {
		fmt.Printf("  %-12s %-9s %s\n", app.Name, app.Source.Version, app.Label)
		if d := summarize(app.Description); d != "" {
			fmt.Printf("  %-12s %-9s %s\n", "", "", d)
		}
	}
	return nil
}

// summarize folds a wrapped YAML description onto one line and keeps the first
// sentence of it. A definition's description is written for a panel card and
// runs to several lines; printed whole it wraps off the side of a terminal and
// the column beside it stops lining up with anything.
func summarize(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if i := strings.Index(s, ". "); i > 0 {
		s = s[:i+1]
	}
	const width = 66
	if len(s) > width {
		s = strings.TrimSpace(s[:width]) + "…"
	}
	return s
}

func newAppsInstallCmd() *cobra.Command {
	var opts appinstall.Options

	cmd := &cobra.Command{
		Use:   "install <app> <domain>",
		Short: "Fetch an application, configure it, and serve it on a domain",
		Long: "The domain is given, never derived: servlo appends no TLD to anything.\n\n" +
			"  servlo apps install wordpress blog.acme.com\n" +
			"  servlo apps install wordpress blog.acme.com --path /srv/blog --admin-email me@acme.com\n\n" +
			"The directory must be empty. The database, its account and the admin password are all " +
			"generated, and the password is printed once and never written to a log.",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.App, opts.Domain = args[0], args[1]
			return runAppsInstall(cmd, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Path, "path", "", "Directory for the site (default: ./<domain>)")
	cmd.Flags().StringVar(&opts.Connection, "connection", "", "Database connection to use (default: the default one)")
	cmd.Flags().StringVar(&opts.AdminUser, "admin-user", "", "Administrator account name (default: admin)")
	cmd.Flags().StringVar(&opts.AdminEmail, "admin-email", "", "Administrator email address")
	cmd.Flags().StringVar(&opts.SiteTitle, "title", "", "Site title (default: the domain)")
	return cmd
}

func runAppsInstall(cmd *cobra.Command, opts appinstall.Options) error {
	feedback.Begin()
	res, err := appinstall.Install(cmd.Context(), opts)
	if err != nil {
		return err
	}

	feedback.Done("installed " + feedback.Val(res.App.Label) + " at " + feedback.Val(res.Site.PrimaryDomain()))
	fmt.Println()
	fmt.Printf("  Site       http://%s\n", res.Site.PrimaryDomain())
	fmt.Printf("  Directory  %s\n", res.Site.Path)
	if res.Database.Name != "" {
		fmt.Printf("  Database   %s as %s\n", res.Database.Name, res.Database.User)
	}
	if res.AdminPassword != "" {
		// Printed once, here, and nowhere else. It is not in the audit log and
		// not in any file servlo writes.
		fmt.Println()
		fmt.Printf("  Administrator  %s\n", res.AdminUser)
		fmt.Printf("  Password       %s\n", res.AdminPassword)
		fmt.Println("\n  Written down nowhere else. Save it now.")
	}
	if res.Note != "" {
		fmt.Println()
		fmt.Println("  " + res.Note)
	}
	fmt.Println()
	fmt.Println("Point DNS at this server, then run: servlo secure " + res.Site.PrimaryDomain())
	return nil
}
