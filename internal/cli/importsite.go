package cli

import (
	"fmt"

	"github.com/ServloOfficial/servlo/internal/feedback"
	"github.com/ServloOfficial/servlo/internal/siteimport"
	"github.com/spf13/cobra"
)

// newImportSiteCmd is `servlo import site`: a directory and a .sql dump become
// a site here.
//
// Under `import` rather than beside `link`, because what this is doing is not
// linking a directory somebody built here. It is taking a site that has been
// running somewhere else and bringing it over, and the dump is the half that
// makes it that rather than the other thing.
func newImportSiteCmd() *cobra.Command {
	var dump, connection, php string
	cmd := &cobra.Command{
		Use:   "site <directory> <domain>",
		Short: "Import a site that is already running somewhere else",
		Long: "Takes what somebody actually has when they are moving off shared hosting: a " +
			"directory of files, and a .sql dump. Servlo works out the framework and the document " +
			"root, registers the site, writes the vhost, and loads the dump into a database of its " +
			"own.\n\n" +
			"The files are not moved. A directory somebody has just uploaded a few gigabytes into " +
			"is not one to copy again for no reason.",
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			feedback.Begin()
			return runImportSite(args[0], args[1], dump, connection, php)
		},
	}
	cmd.Flags().StringVar(&dump, "dump", "", "A plain .sql file to load into the site's database")
	cmd.Flags().StringVar(&connection, "connection", "", "Which database connection to put it on")
	cmd.Flags().StringVar(&php, "php", "", "Pin a PHP version instead of letting servlo pick")
	return cmd
}

func runImportSite(path, domain, dump, connection, php string) error {
	step := feedback.Start("importing " + domain)
	res, err := siteimport.Import(siteimport.Options{
		Path: path, Domain: domain, Dump: dump, Connection: connection, PHPVersion: php,
	})
	if err != nil {
		step.Fail(err)
		return err
	}
	step.OK(feedback.Val(res.Site.Path))

	framework := res.Framework
	if framework == "" {
		framework = "not recognised"
	}
	fmt.Printf("  Framework   %s\n", framework)
	fmt.Printf("  Document root  %s\n", res.Site.PublicDir)
	fmt.Printf("  PHP         %s\n", res.Site.PHPVersion)
	if res.Database != "" {
		fmt.Printf("  Database    %s\n", res.Database)
	}
	for _, note := range res.Notes {
		feedback.Warn("%s", note)
	}
	fmt.Println()
	fmt.Println("  Point DNS at this server, then run servlo secure to get it a certificate.")
	return nil
}
