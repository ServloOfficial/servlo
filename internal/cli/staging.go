package cli

import (
	"fmt"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/feedback"
	"github.com/realrashid/servlo/internal/staging"
	"github.com/spf13/cobra"
)

// NewStagingCmd is `servlo staging`: a copy of a live site that is safe to have
// on the internet.
func NewStagingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "staging",
		Short: "A copy of a live site, not indexed and behind a password",
		Long: "A staging site is a site like any other: its own directory, its own domain, its own " +
			"database, its own certificate. What makes it staging is that it remembers which live " +
			"site it copies from, it is not indexed, and it is behind a password.\n\n" +
			"Those last two are not optional. A staging site search engines index is duplicate " +
			"content against the real site, and one anybody can open is a half-finished feature and " +
			"a copy of the client's data on a public address.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error { return runStagingList() },
	}
	cmd.AddCommand(newStagingCreateCmd(), newStagingRefreshCmd(), newStagingPasswordCmd())
	return cmd
}

func runStagingList() error {
	reg, err := config.LoadSites()
	if err != nil {
		return err
	}
	var found int
	for i := range reg.Sites {
		site := &reg.Sites[i]
		if !site.IsStaging() {
			continue
		}
		found++
		origin := site.Staging.Origin
		if origin == "" {
			origin = "nothing"
		}
		when := site.Staging.RefreshedAt
		if when == "" {
			when = "never refreshed"
		}
		fmt.Printf("  %-32s copies %s, %s\n", site.PrimaryDomain(), origin, when)
	}
	if found == 0 {
		fmt.Println("No staging sites. Make one with: servlo staging create <live site> <staging domain>")
	}
	return nil
}

func newStagingCreateCmd() *cobra.Command {
	var path, user string
	var refresh bool
	cmd := &cobra.Command{
		Use:   "create <live site> <staging domain>",
		Short: "Make a staging copy of a live site",
		Long: "Creates the site and its credentials, and nothing else. Filling it is a separate " +
			"step, because putting today's live data on it is the part somebody will want to " +
			"repeat.\n\n" +
			"The staging domain is given rather than derived. Servlo appends no suffix to " +
			"anything, and guessing staging.<the live domain> would be wrong for everybody whose " +
			"staging lives somewhere else.",
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			feedback.Begin()
			return runStagingCreate(args[0], args[1], path, user, refresh)
		},
	}
	cmd.Flags().StringVar(&path, "path", "", "Where the files go. Empty puts it beside the live site")
	cmd.Flags().StringVar(&user, "user", staging.DefaultUser, "Who logs in")
	cmd.Flags().BoolVar(&refresh, "refresh", false, "Copy live across as soon as it is made")
	return cmd
}

func runStagingCreate(origin, domain, path, user string, refresh bool) error {
	step := feedback.Start("creating " + domain)
	made, err := staging.Create(staging.Options{Origin: origin, Domain: domain, Path: path, User: user})
	if err != nil {
		step.Fail(err)
		return err
	}
	step.OK(feedback.Val(made.Site.Path))

	// Printed once and stored nowhere. A staging password kept somewhere
	// readable is the password on a copy of production data kept somewhere
	// readable, so this is the only time anybody sees it.
	fmt.Println()
	fmt.Printf("  Username  %s\n", made.Credentials.User)
	fmt.Printf("  Password  %s\n", made.Credentials.Password)
	fmt.Println()
	fmt.Println("  Write that down now. Servlo keeps only the hash, so it cannot be shown again.")
	fmt.Println("  Reset it with: servlo staging password " + domain)
	if made.Note != "" {
		feedback.Warn("%s", made.Note)
	}

	if refresh {
		site, err := config.FindSiteByRef(domain)
		if err != nil {
			return err
		}
		return runStagingRefresh(site, staging.Everything())
	}
	fmt.Println()
	fmt.Printf("  It is empty. Put live on it with: servlo staging refresh %s\n", domain)
	return nil
}

func newStagingRefreshCmd() *cobra.Command {
	var filesOnly, databaseOnly bool
	cmd := &cobra.Command{
		Use:   "refresh <staging site>",
		Short: "Copy the live site over its staging copy",
		Long: "Writes live's files over staging's and loads live's database into staging's. What is " +
			"only on staging is left alone, the same way a deploy leaves it alone.\n\n" +
			"The .env never crosses. Staging has its own database credentials and its own address, " +
			"and overwriting them with live's would point the staging site at the production " +
			"database.\n\n" +
			"There is no command for the other direction, and there is not going to be one.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if filesOnly && databaseOnly {
				return fmt.Errorf("--files-only and --database-only cannot both be given")
			}
			feedback.Begin()
			site, err := config.FindSiteByRef(args[0])
			if err != nil {
				return fmt.Errorf("%q is not a site on this server", args[0])
			}
			bring := staging.Everything()
			if filesOnly {
				bring = staging.Bring{Files: true}
			}
			if databaseOnly {
				bring = staging.Bring{Database: true}
			}
			return runStagingRefresh(site, bring)
		},
	}
	cmd.Flags().BoolVar(&filesOnly, "files-only", false, "Copy the files and leave the staging database alone")
	cmd.Flags().BoolVar(&databaseOnly, "database-only", false, "Copy the database and leave the files alone")
	return cmd
}

func runStagingRefresh(site *config.Site, bring staging.Bring) error {
	step := feedback.Start("copying " + site.Staging.Origin + " onto " + site.Name)
	res, err := staging.Refresh(site, bring)
	if err != nil {
		step.Fail(err)
		return err
	}
	what := fmt.Sprintf("%d %s", res.Files, plural(res.Files, "file", "files"))
	if res.Database != "" {
		what += " and " + res.Database
	}
	step.OK(feedback.Val(what))
	if res.Bytes > 0 {
		fmt.Printf("  %s copied\n", humanSize(res.Bytes))
	}
	if res.Note != "" {
		feedback.Warn("%s", res.Note)
	}
	return nil
}

func newStagingPasswordCmd() *cobra.Command {
	var user string
	return &cobra.Command{
		Use:   "password <staging site>",
		Short: "Give a staging site a new password",
		Long: "Generates a new one and prints it once. The old one stops working immediately, which " +
			"is the point: this is what to run when somebody who had it should not have it any more.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			feedback.Begin()
			site, err := config.FindSiteByRef(args[0])
			if err != nil {
				return fmt.Errorf("%q is not a site on this server", args[0])
			}
			if !site.IsStaging() {
				return fmt.Errorf("%s is not a staging site, so it has no staging password", site.Name)
			}
			if user == "" {
				user = site.Staging.User
			}
			creds, hash, err := staging.NewCredentials(user)
			if err != nil {
				return err
			}
			if err := staging.WriteHtpasswd(site.PrimaryDomain(), creds.User, hash); err != nil {
				return err
			}
			site.Staging.User, site.Staging.Hash = creds.User, hash
			if err := config.AddSite(*site); err != nil {
				return err
			}
			feedback.Start("resetting the password").OK(feedback.Val(site.PrimaryDomain()))
			fmt.Println()
			fmt.Printf("  Username  %s\n", creds.User)
			fmt.Printf("  Password  %s\n", creds.Password)
			fmt.Println()
			fmt.Println("  The old password no longer works. Servlo keeps only the hash, so this")
			fmt.Println("  one cannot be shown again either.")
			return nil
		},
	}
}
