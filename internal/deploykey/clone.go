package deploykey

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Cloning with a deploy key.
//
// The operator pastes whichever URL the repository's page offered them, which
// is one of three spellings of the same thing, and only the SSH one can be
// cloned with a key. So the URL is normalised rather than validated: refusing
// the https form would be technically correct and practically useless, since
// that is the one GitHub puts first.

// CloneURL is a repository reference servlo is willing to clone.
type CloneURL struct {
	// SSH is the scp-style form git clones with.
	SSH string `json:"ssh"`
	// Host is what the connection test connects to.
	Host string `json:"host"`
	// Owner and Repo name the repository. Repo doubles as the suggested
	// directory name, since it is what `git clone` would have picked.
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
}

var (
	// scpLike matches git@host:owner/repo(.git), the form git itself accepts.
	scpLike = regexp.MustCompile(`^([a-zA-Z0-9._-]+)@([a-zA-Z0-9.-]+):([a-zA-Z0-9._/-]+?)(?:\.git)?$`)
	// hostPattern is a hostname and nothing else: no port, no userinfo, no path.
	hostPattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9.-]{0,251}[a-zA-Z0-9])?$`)
	// repoPath is owner/repo, or a GitLab-style group/subgroup/repo.
	repoPath = regexp.MustCompile(`^[a-zA-Z0-9._-]+(?:/[a-zA-Z0-9._-]+)+$`)
)

// NormalizeCloneURL turns what the operator pasted into the SSH form, or
// refuses it.
//
// Refusing is the important half. The result becomes an argv git runs and a
// destination ssh connects to, so anything that could smuggle a second option
// (a leading dash), reach a local path, or name a transport that executes a
// command (git's ext:: helper) is rejected rather than escaped. Embedded
// credentials are refused too: a token in a URL ends up in the site config and
// the audit log, and a deploy key is what this story exists to use instead.
func NormalizeCloneURL(raw string) (CloneURL, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return CloneURL{}, fmt.Errorf("no repository URL given")
	}
	if strings.ContainsAny(s, " \t\n") {
		return CloneURL{}, fmt.Errorf("%q contains whitespace: paste just the repository URL", raw)
	}
	if strings.HasPrefix(s, "-") {
		return CloneURL{}, fmt.Errorf("%q starts with a dash, which git would read as an option", raw)
	}

	var host, path string
	switch {
	case scpLike.MatchString(s):
		m := scpLike.FindStringSubmatch(s)
		host, path = m[2], m[3]
	case strings.HasPrefix(s, "https://"), strings.HasPrefix(s, "http://"), strings.HasPrefix(s, "ssh://"):
		u, err := url.Parse(s)
		if err != nil {
			return CloneURL{}, fmt.Errorf("%q is not a URL servlo can read", raw)
		}
		// ssh://git@host/... legitimately names the git user. Anything else in
		// the userinfo is a token somebody is about to paste into a form that
		// writes it to the site config and the audit log.
		if u.User != nil {
			_, hasPassword := u.User.Password()
			if hasPassword || u.User.Username() != "git" || u.Scheme != "ssh" {
				return CloneURL{}, fmt.Errorf("that URL carries a username or token: paste the plain repository URL and let the deploy key authenticate")
			}
		}
		host = u.Hostname()
		path = strings.TrimSuffix(strings.TrimPrefix(u.Path, "/"), ".git")
	default:
		return CloneURL{}, fmt.Errorf("%q is not a repository URL: use the SSH or HTTPS address from the repository's clone menu", raw)
	}

	if !hostPattern.MatchString(host) {
		return CloneURL{}, fmt.Errorf("%q does not name a host servlo can connect to", raw)
	}
	if !repoPath.MatchString(path) {
		return CloneURL{}, fmt.Errorf("%q does not name a repository as owner/name", raw)
	}
	// ".." is a legal segment by the character class above and is never a real
	// owner or repository name, so a path built out of it is a traversal
	// wearing the right shape.
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return CloneURL{}, fmt.Errorf("%q does not name a repository as owner/name", raw)
		}
	}

	parts := strings.Split(path, "/")
	return CloneURL{
		SSH:   "git@" + host + ":" + path + ".git",
		Host:  host,
		Owner: strings.Join(parts[:len(parts)-1], "/"),
		Repo:  parts[len(parts)-1],
	}, nil
}

// TestResult is what a connection test found.
type TestResult struct {
	OK bool `json:"ok"`
	// Reason names what to do about a failure, and is empty on success.
	Reason string `json:"reason,omitempty"`
	// Greeting is the host's own answer, shown on success because "Hi
	// owner/repo!" is how an operator confirms the key landed on the repository
	// they meant rather than a different one.
	Greeting string `json:"greeting,omitempty"`
}

// testTimeout bounds a connection that is being dropped rather than refused,
// which is what an outbound firewall usually looks like.
const testTimeout = 20 * time.Second

// TestConnection asks the host whether it knows this key.
//
// Exit status is deliberately ignored. GitHub answers a perfectly good deploy
// key with its refusal-of-shell-access banner and exits 1, so reading the code
// would report every working key as broken. What the host said is the answer.
func TestConnection(ctx context.Context, key Key, target CloneURL) TestResult {
	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ssh", "-T",
		"-i", key.PrivatePath,
		"-o", "IdentitiesOnly=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"git@"+target.Host,
	)
	out, _ := cmd.CombinedOutput()
	answer := strings.TrimSpace(string(out))

	if ctx.Err() != nil && answer == "" {
		return TestResult{Reason: "the connection to " + target.Host + " timed out: check the outbound firewall on port 22"}
	}
	if Authenticated(answer) {
		return TestResult{OK: true, Greeting: firstLines(answer, 1)}
	}
	return TestResult{Reason: ExplainSSHFailure(answer)}
}

// Clone runs git clone into dir with this key and no other identity. Output is
// streamed to progress line by line so the panel can show it happening.
func Clone(ctx context.Context, key Key, target CloneURL, dir string, progress func(string)) error {
	cmd := exec.CommandContext(ctx, "git", "clone", "--", target.SSH, dir)
	cmd.Env = append(os.Environ(),
		"GIT_SSH_COMMAND="+SSHCommand(key.PrivatePath),
		// A clone that would otherwise sit waiting for a passphrase or a
		// username prompt has nobody to answer it, so fail instead of hanging.
		"GIT_TERMINAL_PROMPT=0",
	)

	out, err := cmd.CombinedOutput()
	if progress != nil {
		for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
			if line != "" {
				progress(line)
			}
		}
	}
	if err != nil {
		return fmt.Errorf("%s", ExplainSSHFailure(string(out)))
	}
	return nil
}
