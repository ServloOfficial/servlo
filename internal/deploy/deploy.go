// Package deploy runs a site's deploy: back up, pull, run the script, make it
// live.
//
// The order is the design. A migration never runs without a database backup
// taken first, a script never runs against a tree that half-pulled, and new
// code never becomes live if the script that was meant to prepare it failed.
// Each of those is one step refusing to start because the one before it did not
// finish, which is why they are separate phases rather than one shell command.
//
// Every step that reaches outside this process is a field on Options. That is
// not only for tests: the panel and the CLI drive the same sequence and differ
// only in where the output goes.
package deploy

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ServloOfficial/servlo/internal/config"
)

// writer is io.Writer, spelled inline so Options reads as a list of things the
// caller supplies rather than as an import.
type writer = io.Writer

// Options is everything a deploy needs and everything it is allowed to touch.
type Options struct {
	Site *config.Site
	// Out receives the phase headers and every command's own output.
	Out writer

	// Git runs a git command in dir, streaming to out.
	Git func(dir string, out writer, args ...string) error
	// Head reports the commit currently checked out.
	Head func(dir string) (string, error)
	// Snapshot takes a database backup and returns its name.
	Snapshot func(*config.Site) (string, error)
	// RunScript runs the site's deploy script with dir as its working
	// directory, streaming to out.
	RunScript func(dir, script string, out writer) error
	// Reload makes the new code live. Graceful: a deploy that dropped every
	// in-flight request would be a worse outcome than the one it replaced.
	Reload func(*config.Site) error

	// Keep restores files under the site's excluded paths that the update
	// removed, and reports which ones it kept.
	Keep func(dir, from, to string, excludes []string, out writer) ([]string, error)
	// Excludes are the paths this deploy must not remove.
	Excludes func(*config.Site) ([]string, error)

	// BuildWarning is what to tell the operator before the script runs, empty
	// when there is nothing worth saying.
	BuildWarning func(script string) string

	// Migrates reports whether this deploy is schema-changing.
	Migrates func(*config.Site) (bool, error)
	// Script is the site's deploy script.
	Script func(*config.Site) (string, error)
}

// Result is what happened, and what a deploy history is made of.
type Result struct {
	FromCommit string
	ToCommit   string
	// Snapshot names the pre-deploy database backup, empty when the deploy was
	// not schema-changing.
	Snapshot string
	// Kept are files the update removed that the site's exclude list put back.
	Kept []string
	// Redeploy marks a result that went back to an earlier commit rather than
	// forward to a new one.
	Redeploy bool
	Duration time.Duration
}

// Run deploys a site.
func Run(o Options) (Result, error) {
	started := time.Now()
	var res Result

	from, err := o.Head(o.Site.Path)
	if err != nil {
		return res, fmt.Errorf("reading the current commit: %w", err)
	}
	res.FromCommit = from

	script, err := o.Script(o.Site)
	if err != nil {
		return res, fmt.Errorf("reading the deploy script: %w", err)
	}

	// Read before the pull, because after it the answer could come from a
	// framework definition the pull itself changed.
	excludes, err := o.Excludes(o.Site)
	if err != nil {
		return res, fmt.Errorf("reading the paths this deploy must not remove: %w", err)
	}

	migrates, err := o.Migrates(o.Site)
	if err != nil {
		return res, fmt.Errorf("deciding whether this deploy migrates: %w", err)
	}
	if migrates {
		phase(o.Out, "Database backup")
		name, err := o.Snapshot(o.Site)
		if err != nil {
			// The deploy stops here. A guarantee that only holds when nothing
			// goes wrong is not one, and this is the case it exists for.
			return res, fmt.Errorf("this deploy runs a migration and its database backup failed, so nothing was deployed: %w", err)
		}
		res.Snapshot = name
		fmt.Fprintf(o.Out, "Saved as %s\n", name)
	}

	phase(o.Out, "Pull")
	// Fast-forward only. A deploy that merges is a deploy that can produce a
	// commit nobody wrote and nobody reviewed, on a server, unattended.
	if err := o.Git(o.Site.Path, o.Out, "pull", "--ff-only"); err != nil {
		return res, fmt.Errorf("pulling: %w", err)
	}

	to, err := o.Head(o.Site.Path)
	if err != nil {
		return res, fmt.Errorf("reading the new commit: %w", err)
	}
	res.ToCommit = to

	// Before the script rather than after it. A script that rebuilds a cache or
	// runs a plugin update should see the tree the site actually has, not one
	// briefly missing the client's plugins.
	kept, err := o.Keep(o.Site.Path, from, to, excludes, o.Out)
	if err != nil {
		// Not something to carry on past. The operator named these paths
		// precisely so a deploy would not lose them, and one that reports
		// success having lost them is the outcome this exists to prevent.
		return res, fmt.Errorf("this deploy removed files the site protects and they could not be put back: %w", err)
	}
	res.Kept = kept

	if hasCommands(script) {
		phase(o.Out, "Deploy script")
		// Before the build, not after it. After it is a failed deploy and the
		// moment the operator could have done something about it has passed.
		if warning := o.BuildWarning(script); warning != "" {
			fmt.Fprintf(o.Out, "%s\n\n", warning)
		}
		if err := o.RunScript(o.Site.Path, script, o.Out); err != nil {
			// No reload. Production OPcache runs with validate_timestamps off,
			// so PHP keeps serving the bytecode it already has: withholding the
			// reload leaves visitors on the last version that worked rather
			// than on the half-deployed one now on disk.
			//
			// Worth knowing what that does and does not cover, because it
			// reads stronger than it is. It keeps live whatever is already in
			// the cache. A file the cache has never seen is compiled off disk
			// on the next request however new it is, so a route nobody hit
			// between the last successful deploy and this failed one serves
			// the half-deployed code. The window is widest right after a
			// successful deploy, whose own reload starts workers with an empty
			// cache. Deploy is a pull in place (CLAUDE.md §3.5) and the tree is
			// not rolled back, so this is a real limit rather than a bug to fix
			// here: on a busy site the cache is warm and it holds, on a quiet
			// one it may not.
			return res, fmt.Errorf("the deploy script failed, and the new code was not made live: %w", err)
		}
	} else {
		fmt.Fprintf(o.Out, "\nNo deploy script, so the pull is the deploy.\n")
	}

	phase(o.Out, "Reload")
	if err := o.Reload(o.Site); err != nil {
		return res, fmt.Errorf("reloading PHP: %w", err)
	}

	res.Duration = time.Since(started)
	fmt.Fprintf(o.Out, "\nDeployed %s in %s.\n", short(to), res.Duration.Round(time.Millisecond))
	return res, nil
}

// phase announces a step before it runs, so a failure part-way through a long
// deploy is attributable to one of them rather than to the wall of output.
func phase(w writer, name string) {
	fmt.Fprintf(w, "\n=== %s ===\n", name)
}

// hasCommands reports whether a script does anything, ignoring the header
// comments every script starts with. An empty one is not an error: on most
// WordPress sites the pull is the deploy.
func hasCommands(script string) bool {
	for _, line := range strings.Split(script, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			return true
		}
	}
	return false
}

func short(commit string) string {
	if len(commit) > 8 {
		return commit[:8]
	}
	return commit
}
