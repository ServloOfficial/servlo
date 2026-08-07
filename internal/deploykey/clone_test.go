package deploykey

import (
	"strings"
	"testing"
)

// The form takes what the operator copied off GitHub, which is whichever of the
// three spellings that page offered them. All three name the same repository
// and only one of them can be cloned with a deploy key.
func TestNormalizeCloneURL(t *testing.T) {
	for _, tc := range []struct {
		in, wantURL, wantHost string
	}{
		{"git@github.com:realrashid/servlo.git", "git@github.com:realrashid/servlo.git", "github.com"},
		{"https://github.com/realrashid/servlo.git", "git@github.com:realrashid/servlo.git", "github.com"},
		{"https://github.com/realrashid/servlo", "git@github.com:realrashid/servlo.git", "github.com"},
		{"ssh://git@github.com/realrashid/servlo.git", "git@github.com:realrashid/servlo.git", "github.com"},
		{"  git@gitlab.com:group/sub/app.git  ", "git@gitlab.com:group/sub/app.git", "gitlab.com"},
	} {
		got, err := NormalizeCloneURL(tc.in)
		if err != nil {
			t.Errorf("NormalizeCloneURL(%q): %v", tc.in, err)
			continue
		}
		if got.SSH != tc.wantURL {
			t.Errorf("NormalizeCloneURL(%q).SSH = %q, want %q", tc.in, got.SSH, tc.wantURL)
		}
		if got.Host != tc.wantHost {
			t.Errorf("NormalizeCloneURL(%q).Host = %q, want %q", tc.in, got.Host, tc.wantHost)
		}
	}
}

// A clone URL becomes an argv and an ssh destination, so anything that could
// smuggle a second option or a local path is refused rather than sanitised.
func TestNormalizeCloneURL_Refuses(t *testing.T) {
	for _, bad := range []string{
		"",
		"not a url",
		"/etc/passwd",
		"file:///etc/passwd",
		"--upload-pack=/bin/sh",
		"ext::sh -c whoami",
		"git@github.com:realrashid/servlo.git --config=core.sshCommand=id",
		// Each of these reaches a different guard, and every one of them was
		// accepted at some point while this was being written.
		"-x@github.com:realrashid/servlo.git",
		"https://github.com/onlyanowner",
		"ssh://git@github.com/",
		"git@github.com:../../etc/passwd",
		"https://token@github.com/realrashid/servlo.git",
	} {
		if got, err := NormalizeCloneURL(bad); err == nil {
			t.Errorf("NormalizeCloneURL(%q) was accepted as %+v", bad, got)
		}
	}
}

// A password in an https URL would end up in the audit log, the site config and
// the operator's clipboard. Deploy keys are the whole point of this story.
func TestNormalizeCloneURL_RefusesEmbeddedCredentials(t *testing.T) {
	if _, err := NormalizeCloneURL("https://user:token@github.com/realrashid/servlo.git"); err == nil {
		t.Error("a URL carrying credentials was accepted")
	}
}

func TestCloneURL_SuggestsADirectoryName(t *testing.T) {
	got, err := NormalizeCloneURL("git@github.com:realrashid/My-App.git")
	if err != nil {
		t.Fatal(err)
	}
	if got.Repo != "My-App" {
		t.Errorf("Repo = %q, want My-App", got.Repo)
	}
}

// The clone runs with the site's key and no other identity. Without
// IdentitiesOnly, ssh offers every key in the agent first, and a clone that
// succeeds under the wrong identity looks fine until that key is revoked.
func TestSSHCommand_PinsTheIdentity(t *testing.T) {
	cmd := SSHCommand("/home/servlo/.local/share/servlo/deploy-keys/example.com")

	for _, want := range []string{"IdentitiesOnly=yes", "BatchMode=yes", "-i "} {
		if !strings.Contains(cmd, want) {
			t.Errorf("SSHCommand = %q, missing %q", cmd, want)
		}
	}
}

// The path is interpolated into a shell string git runs, so a quote in it must
// not become the end of an argument.
func TestSSHCommand_QuotesThePath(t *testing.T) {
	cmd := SSHCommand("/tmp/it's here/key")
	if !strings.Contains(cmd, `'/tmp/it'\''s here/key'`) {
		t.Errorf("SSHCommand = %q, does not quote the path safely", cmd)
	}
}
