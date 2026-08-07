package deploykey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

func TestEnsure_GeneratesAKeyPairForASite(t *testing.T) {
	isolate(t)

	key, err := Ensure("example.com")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	// The public half is what the operator pastes into the repository, so it
	// has to be in the one-line authorized_keys form GitHub accepts.
	if !strings.HasPrefix(key.Public, "ssh-ed25519 ") {
		t.Errorf("public key = %q, not an ssh-ed25519 line", key.Public)
	}
	if strings.Count(strings.TrimSpace(key.Public), "\n") != 0 {
		t.Errorf("public key spans lines: %q", key.Public)
	}
	// A comment naming the site is how an operator with six deploy keys in a
	// GitHub account tells which one this is.
	if !strings.Contains(key.Public, "example.com") {
		t.Errorf("public key %q does not name the site", key.Public)
	}
}

// A private key is a credential, so it lives where every other servlo
// credential does, and never inside a site directory a deploy could publish.
func TestEnsure_WritesThePrivateKey0600OutsideAnySite(t *testing.T) {
	isolate(t)

	key, err := Ensure("example.com")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	info, err := os.Stat(key.PrivatePath)
	if err != nil {
		t.Fatalf("stat private key: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
	dir, err := os.Stat(filepath.Dir(key.PrivatePath))
	if err != nil {
		t.Fatal(err)
	}
	if perm := dir.Mode().Perm(); perm != 0o700 {
		t.Errorf("directory mode = %o, want 700", perm)
	}
}

// The operator pastes the key into GitHub, then comes back and clicks test. If
// the second call minted a new key the pasted one would be the wrong one, and
// the failure would look like a GitHub problem.
func TestEnsure_IsIdempotent(t *testing.T) {
	isolate(t)

	first, err := Ensure("example.com")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Ensure("example.com")
	if err != nil {
		t.Fatal(err)
	}

	if first.Public != second.Public {
		t.Error("a second call minted a different key")
	}
}

func TestEnsure_KeepsSitesApart(t *testing.T) {
	isolate(t)

	a, err := Ensure("a.example.com")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Ensure("b.example.com")
	if err != nil {
		t.Fatal(err)
	}

	if a.Public == b.Public {
		t.Error("two sites share one deploy key")
	}
	if a.PrivatePath == b.PrivatePath {
		t.Errorf("two sites share one key file: %s", a.PrivatePath)
	}
}

// A domain reaches this from a form, so it must not be able to name a path.
func TestEnsure_RefusesADomainThatWouldEscapeTheKeyDirectory(t *testing.T) {
	isolate(t)

	for _, bad := range []string{"../../etc/passwd", "a/b", "", "."} {
		if _, err := Ensure(bad); err == nil {
			t.Errorf("Ensure(%q) was accepted", bad)
		}
	}
}

func TestRemove_DeletesTheKey(t *testing.T) {
	isolate(t)
	key, err := Ensure("example.com")
	if err != nil {
		t.Fatal(err)
	}

	if err := Remove("example.com"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(key.PrivatePath); !os.IsNotExist(err) {
		t.Error("the private key survived Remove")
	}
	// Removing a key that is not there is the ordinary state after a site was
	// added from a folder rather than a clone.
	if err := Remove("example.com"); err != nil {
		t.Errorf("removing an absent key errored: %v", err)
	}
}

// "Connection failure gives a specific reason, not a generic error" is the
// story's acceptance criterion, and this is where the specific reason comes
// from: ssh says one of a handful of things, and each means something the
// operator can act on.
func TestExplainSSHFailure(t *testing.T) {
	for _, tc := range []struct {
		name, output, want string
	}{
		{
			name:   "key not on the repository",
			output: "git@github.com: Permission denied (publickey).",
			want:   "deploy key",
		},
		{
			name:   "host key not trusted",
			output: "Host key verification failed.",
			want:   "host key",
		},
		{
			name:   "no route to the host",
			output: "ssh: connect to host github.com port 22: Connection timed out",
			want:   "reach",
		},
		{
			name:   "name does not resolve",
			output: "ssh: Could not resolve hostname github.com: Name or service not known",
			want:   "resolve",
		},
		{
			name:   "repository exists but the key has no access to it",
			output: "ERROR: Repository not found.",
			want:   "repository",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ExplainSSHFailure(tc.output)
			if !strings.Contains(strings.ToLower(got), tc.want) {
				t.Errorf("ExplainSSHFailure(%q) = %q, want it to mention %q", tc.output, got, tc.want)
			}
			// A generic error is exactly what the story says not to give.
			if got == "" || strings.EqualFold(got, "connection failed") {
				t.Errorf("ExplainSSHFailure(%q) = %q, which is the generic error", tc.output, got)
			}
		})
	}
}

// Something nobody anticipated still has to say something, and the most useful
// thing it can say is what ssh actually printed.
func TestExplainSSHFailure_FallsBackToWhatSSHSaid(t *testing.T) {
	got := ExplainSSHFailure("ssh: something nobody has seen before")
	if !strings.Contains(got, "something nobody has seen before") {
		t.Errorf("ExplainSSHFailure = %q, want it to carry ssh's own words", got)
	}
}

// GitHub answers a successful auth on stderr with exit status 1, so treating a
// non-zero exit as failure would report every working key as broken.
func TestAuthenticated_ReadsGitHubsSuccessBanner(t *testing.T) {
	if !Authenticated("Hi realrashid/servlo! You've successfully authenticated, but GitHub does not provide shell access.") {
		t.Error("GitHub's success banner was not recognised")
	}
	if Authenticated("git@github.com: Permission denied (publickey).") {
		t.Error("a permission denial was read as success")
	}
}
