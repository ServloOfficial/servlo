package cli

import (
	"fmt"
	"os"

	"github.com/realrashid/servlo/internal/config"
)

// errNotLinked is the single message every directory-scoped command shows when
// the current directory has no registered site, replacing several earlier
// phrasings so the guidance is consistent everywhere.
func errNotLinked() error {
	return fmt.Errorf("no site registered for this directory — run 'servlo link' first")
}

// ensureSiteForCwd resolves the site for the current working directory.
func ensureSiteForCwd() (*config.Site, error) {
	site, _, err := ensureSiteAndBranchForCwd()
	return site, err
}

// ensureSiteAndBranchForCwd resolves the site for the current working
// directory, using os.Getwd for both lookup and link so they can't diverge. On
// a miss in an interactive terminal it offers to link (cascading into init) and
// re-resolves.
func ensureSiteAndBranchForCwd() (*config.Site, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", err
	}
	if site, err := config.FindSiteByPath(cwd); err == nil {
		return site, "", nil
	}
	if !isInteractive() {
		return nil, "", errNotLinked()
	}

	// Wrap the prompt and link in envInterrupt so, when reached from `servlo env`
	// under its live spinner, the prompt and wizard print cleanly instead of
	// being clobbered by the spinner's redraw. Outside runEnvLive it just runs.
	var linkErr error
	envInterrupt(func() {
		fmt.Print("This directory isn't linked to servlo. Link it now? [Y/n] ")
		var answer string
		fmt.Scanln(&answer) //nolint:errcheck
		if !(answer == "" || answer[0] == 'Y' || answer[0] == 'y') {
			linkErr = errNotLinked()
			return
		}
		// runLinkOrInit, not runLink, so a fresh non-PHP/empty project gets the
		// same init wizard `servlo link` now offers instead of a bare PHP link.
		linkErr = runLinkOrInit(nil)
	})
	if linkErr != nil {
		return nil, "", linkErr
	}
	site, err := config.FindSiteByPath(cwd)
	if err != nil {
		return nil, "", errNotLinked()
	}
	return site, "", nil
}
