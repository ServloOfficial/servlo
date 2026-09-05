package deploy

import (
	"fmt"
	"strings"
	"time"

	gitpkg "github.com/ServloOfficial/servlo/internal/git"
)

// Going back a commit.
//
// The same sequence as a deploy with the pull replaced by a checkout, and the
// database left alone. That last part is the whole shape of the feature: code
// goes back, data does not. A migration that added a column is still applied
// after this runs, and the previous commit's code has to survive meeting it.
// Servlo says so rather than implying a rollback it cannot perform, because an
// operator who believes the database went back too will make the next decision
// on a false premise.
//
// There is no automatic reverse migration and there should not be. Reversing
// one means running the operator's own down() against production data on the
// word of a panel button, and the frameworks that offer it do not promise it is
// lossless. Restoring the pre-deploy snapshot is the honest recovery, and it is
// a separate, deliberate act.

// Redeploy checks a site out at an earlier commit and runs its deploy script
// again.
func Redeploy(o Options, commit string) (Result, error) {
	started := time.Now()
	var res Result
	res.Redeploy = true

	from, err := o.Head(o.Site.Path)
	if err != nil {
		return res, fmt.Errorf("reading the current commit: %w", err)
	}
	res.FromCommit = from

	// Resolved before anything moves. A commit this repository does not have is
	// a checkout that fails half way, and the tree it fails in is the one
	// currently serving.
	target, err := resolveCommit(o.Site.Path, commit)
	if err != nil {
		return res, err
	}

	script, err := o.Script(o.Site)
	if err != nil {
		return res, fmt.Errorf("reading the deploy script: %w", err)
	}
	excludes, err := o.Excludes(o.Site)
	if err != nil {
		return res, fmt.Errorf("reading the paths this deploy must not remove: %w", err)
	}

	phase(o.Out, "Going back to "+short(target))
	fmt.Fprintf(o.Out,
		"Code only. Database migrations are not undone, so the schema stays as this\n"+
			"deploy's migrations left it and %s has to run against it. To put the data\n"+
			"back too, restore the pre-deploy snapshot yourself.\n", short(target))

	// No pull. The operator is going backwards, and going to the network first
	// is how a redeploy ends up back on the commit it was escaping.
	if err := o.Git(o.Site.Path, o.Out, "checkout", "--force", target); err != nil {
		return res, fmt.Errorf("checking out %s: %w", short(target), err)
	}
	res.ToCommit = target

	kept, err := o.Keep(o.Site.Path, from, target, excludes, o.Out)
	if err != nil {
		return res, fmt.Errorf("this redeploy removed files the site protects and they could not be put back: %w", err)
	}
	res.Kept = kept

	if hasCommands(script) {
		phase(o.Out, "Deploy script")
		if err := o.RunScript(o.Site.Path, script, o.Out); err != nil {
			// The same reasoning as a forward deploy: without the reload, PHP
			// keeps serving the bytecode it has, so visitors stay on the
			// version that worked rather than on a half-prepared rollback.
			return res, fmt.Errorf("the deploy script failed, and %s was not made live: %w", short(target), err)
		}
	} else {
		fmt.Fprintf(o.Out, "\nNo deploy script, so the checkout is the redeploy.\n")
	}

	phase(o.Out, "Reload")
	if err := o.Reload(o.Site); err != nil {
		return res, fmt.Errorf("reloading PHP: %w", err)
	}

	res.Duration = time.Since(started)
	fmt.Fprintf(o.Out, "\nBack on %s in %s.\n", short(target), res.Duration.Round(time.Millisecond))
	return res, nil
}

// resolveCommit turns what the caller asked for into a commit this repository
// actually has.
func resolveCommit(dir, commit string) (string, error) {
	if strings.TrimSpace(commit) == "" {
		return "", fmt.Errorf("no commit to go back to: this site has no deploy recorded to go back from")
	}
	// ^{commit} so a tag or a branch name cannot smuggle in something that is
	// not a commit, and a name that does not resolve fails here rather than
	// during the checkout.
	out, err := gitpkg.Output(dir, "rev-parse", "--verify", "--quiet", commit+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("this repository has no commit %s", short(commit))
	}
	return strings.TrimSpace(out), nil
}

// CommitMeta is who wrote a commit and what they called it.
//
// Blank rather than an error when it cannot be read. This decorates a history
// entry, and an entry that says what happened without saying who wrote the
// change is still worth having; one that failed to write because git could not
// resolve a commit is not.
func CommitMeta(dir, commit string) (author, subject string) {
	if strings.TrimSpace(commit) == "" {
		return "", ""
	}
	out, err := gitpkg.Output(dir, "show", "--no-patch", "--format=%an%n%s", commit)
	if err != nil {
		return "", ""
	}
	parts := strings.SplitN(strings.TrimRight(out, "\n"), "\n", 2)
	if len(parts) < 2 {
		return strings.TrimSpace(parts[0]), ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}
