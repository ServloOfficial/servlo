package backupdest

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func record(t *testing.T, out string, fail error) (*[][]string, *[][]string) {
	t.Helper()
	var runs, envs [][]string
	prev := run
	t.Cleanup(func() { run = prev })
	run = func(_ context.Context, args, env []string, _ io.Reader, stdout io.Writer) ([]byte, error) {
		runs = append(runs, append([]string{}, args...))
		envs = append(envs, append([]string{}, env...))
		if stdout != nil && out != "" {
			_, _ = io.WriteString(stdout, out)
		}
		if fail != nil {
			// rclone prints the remote's configuration on some failures, which
			// is exactly how a secret ends up in an error. The fake does too.
			return []byte("rclone: directory not found (secret_access_key=s3cret-access-key)"), fail
		}
		return nil, nil
	}
	return &runs, &envs
}

func spaces() Destination {
	return Destination{
		Name: "spaces", Kind: KindS3,
		Bucket: "acme-backups", Prefix: "servers/lon1",
		Endpoint: "https://fra1.digitaloceanspaces.com", Region: "fra1",
		AccessKey: "DO00EXAMPLE", SecretKey: "s3cret-access-key",
	}
}

func remote() Destination {
	return Destination{
		Name: "offsite", Kind: KindSFTP,
		Host: "backups.example.net", Port: 22, User: "servlo",
		Path: "/srv/backups/lon1", KeyFile: "/home/deploy/.ssh/backup_ed25519",
	}
}

// The credentials go in the environment, never the argument list. rclone will
// take either, and an argv is readable by anything running as this user.
func TestUpload_KeepsSecretsOutOfTheArgumentList(t *testing.T) {
	runs, envs := record(t, "", nil)

	if err := Upload(spaces(), "acme-20260809.servlobak", strings.NewReader("archive")); err != nil {
		t.Fatal(err)
	}
	argv := strings.Join((*runs)[0], " ")
	if strings.Contains(argv, "s3cret-access-key") {
		t.Errorf("the secret key is in the argument list:\n%s", argv)
	}
	env := strings.Join((*envs)[0], " ")
	if !strings.Contains(env, "s3cret-access-key") {
		t.Errorf("the secret key never reached rclone: env was %s", env)
	}
}

// The archive lands under the destination's own prefix, so one bucket can hold
// several servers without them overwriting each other.
func TestUpload_WritesUnderThePrefix(t *testing.T) {
	runs, _ := record(t, "", nil)

	if err := Upload(spaces(), "acme-20260809.servlobak", strings.NewReader("archive")); err != nil {
		t.Fatal(err)
	}
	argv := strings.Join((*runs)[0], " ")
	if !strings.Contains(argv, ":acme-backups/servers/lon1/acme-20260809.servlobak") {
		t.Errorf("the archive did not land under the prefix:\n%s", argv)
	}
}

// SFTP is the same operation aimed somewhere else, and the key is a file rather
// than a secret in the environment.
func TestUpload_SFTPUsesTheKeyFileAndThePath(t *testing.T) {
	runs, envs := record(t, "", nil)

	if err := Upload(remote(), "acme-20260809.servlobak", strings.NewReader("archive")); err != nil {
		t.Fatal(err)
	}
	argv := strings.Join((*runs)[0], " ")
	if !strings.Contains(argv, ":/srv/backups/lon1/acme-20260809.servlobak") {
		t.Errorf("the archive did not land at the destination's path:\n%s", argv)
	}
	env := strings.Join((*envs)[0], " ")
	for _, want := range []string{"backups.example.net", "servlo", "/home/deploy/.ssh/backup_ed25519"} {
		if !strings.Contains(env, want) {
			t.Errorf("the destination is missing %q from its configuration: %s", want, env)
		}
	}
}

// A failed upload is reported, and what the tool said travels, with anything
// secret taken out of it first.
func TestUpload_ReportsAFailureWithoutLeakingTheSecret(t *testing.T) {
	record(t, "", errors.New("exit status 1"))

	err := Upload(spaces(), "acme.servlobak", strings.NewReader("archive"))
	if err == nil {
		t.Fatal("a failed upload reported success")
	}
	if !strings.Contains(err.Error(), "directory not found") {
		t.Errorf("error = %q, does not carry what the tool said", err)
	}
	if strings.Contains(err.Error(), "s3cret-access-key") {
		t.Errorf("the secret key is in the error text: %q", err)
	}
}

// A destination servlo cannot make sense of is refused where it is configured,
// not at three in the morning when the first scheduled upload runs.
func TestValidate_RefusesAHalfConfiguredDestination(t *testing.T) {
	for _, tc := range []struct {
		name string
		d    Destination
	}{
		{"no name", Destination{Kind: KindS3, Bucket: "b", AccessKey: "a", SecretKey: "s", Endpoint: "https://x"}},
		{"s3 with no bucket", Destination{Name: "d", Kind: KindS3, AccessKey: "a", SecretKey: "s", Endpoint: "https://x"}},
		{"s3 with no credentials", Destination{Name: "d", Kind: KindS3, Bucket: "b", Endpoint: "https://x"}},
		{"sftp with no host", Destination{Name: "d", Kind: KindSFTP, User: "u", Path: "/p"}},
		{"sftp with no path", Destination{Name: "d", Kind: KindSFTP, Host: "h", User: "u"}},
		{"an unknown kind", Destination{Name: "d", Kind: "dropbox"}},
	} {
		if err := tc.d.Validate(); err == nil {
			t.Errorf("%s was accepted", tc.name)
		}
	}
	if err := spaces().Validate(); err != nil {
		t.Errorf("a complete S3 destination was refused: %v", err)
	}
	if err := remote().Validate(); err != nil {
		t.Errorf("a complete SFTP destination was refused: %v", err)
	}
}

// A destination that could not work is refused before anything runs, so a
// mistyped one fails where it was typed rather than by launching a container
// that will not connect.
func TestUpload_RefusesAnInvalidDestinationWithoutRunningAnything(t *testing.T) {
	runs, _ := record(t, "", nil)

	broken := spaces()
	broken.Bucket = ""
	if err := Upload(broken, "acme.servlobak", strings.NewReader("archive")); err == nil {
		t.Fatal("an S3 destination with no bucket was accepted")
	}
	if len(*runs) != 0 {
		t.Errorf("something was run for a destination that cannot work: %v", *runs)
	}
}
