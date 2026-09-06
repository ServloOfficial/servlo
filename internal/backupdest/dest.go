// Package backupdest sends a finished archive somewhere that is not this
// server.
//
// A backup that only exists on the machine it is a backup of is not a backup.
// The two destinations that matter are S3-compatible storage, which covers
// DigitalOcean Spaces and Amazon S3 with one driver, and SFTP to another
// server.
//
// Neither is spoken here. rclone is run in a container on the servlo network,
// the same way the database clients are, because it already speaks both
// protocols correctly and the alternative is hand-rolling request signing whose
// first failure would be a backup that silently never arrived.
package backupdest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/ServloOfficial/servlo/internal/podman"
)

// The kinds of destination servlo can send to.
const (
	KindS3   = "s3"
	KindSFTP = "sftp"
)

// image is the client. Pinned rather than latest, because a backup destination
// silently changing behaviour on a pull is the kind of surprise that is only
// discovered during a restore.
const image = "docker.io/rclone/rclone:1.68"

// remoteName is what the destination is called inside the container. It never
// leaves this process, so one fixed name is simpler than deriving one and
// cannot collide with anything.
const remoteName = "dest"

// Timeout bounds one transfer. Long, because a multi-gigabyte archive over a
// slow link legitimately takes a while, and cutting it off would leave a
// truncated object that looks like a backup.
const Timeout = 6 * time.Hour

// Destination is one place archives are copied to.
//
// The secrets are here because this is what the operator configured, and the
// file it lives in is 0600 for exactly that reason. They never reach an
// argument list.
type Destination struct {
	Name string `yaml:"name" json:"name"`
	Kind string `yaml:"kind" json:"kind"`

	// S3.
	Bucket    string `yaml:"bucket,omitempty" json:"bucket,omitempty"`
	Prefix    string `yaml:"prefix,omitempty" json:"prefix,omitempty"`
	Endpoint  string `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	Region    string `yaml:"region,omitempty" json:"region,omitempty"`
	AccessKey string `yaml:"access_key,omitempty" json:"access_key,omitempty"`
	SecretKey string `yaml:"secret_key,omitempty" json:"-"`

	// SFTP.
	Host    string `yaml:"host,omitempty" json:"host,omitempty"`
	Port    int    `yaml:"port,omitempty" json:"port,omitempty"`
	User    string `yaml:"user,omitempty" json:"user,omitempty"`
	Path    string `yaml:"path,omitempty" json:"path,omitempty"`
	KeyFile string `yaml:"key_file,omitempty" json:"key_file,omitempty"`
}

// Validate refuses a destination that could not work, where it is configured
// rather than at three in the morning when the first scheduled upload runs.
func (d Destination) Validate() error {
	if strings.TrimSpace(d.Name) == "" {
		return fmt.Errorf("a destination needs a name")
	}
	switch d.Kind {
	case KindS3:
		switch {
		case d.Bucket == "":
			return fmt.Errorf("%s: an S3 destination needs a bucket", d.Name)
		case d.Endpoint == "":
			return fmt.Errorf("%s: an S3 destination needs an endpoint, for example https://fra1.digitaloceanspaces.com", d.Name)
		case d.AccessKey == "" || d.SecretKey == "":
			return fmt.Errorf("%s: an S3 destination needs an access key and a secret key", d.Name)
		}
	case KindSFTP:
		switch {
		case d.Host == "":
			return fmt.Errorf("%s: an SFTP destination needs a host", d.Name)
		case d.User == "":
			return fmt.Errorf("%s: an SFTP destination needs a user", d.Name)
		case d.Path == "":
			return fmt.Errorf("%s: an SFTP destination needs a path to write into", d.Name)
		}
	default:
		return fmt.Errorf("%s: %q is not a destination servlo knows: use %s or %s", d.Name, d.Kind, KindS3, KindSFTP)
	}
	return nil
}

// run is the seam. Every test asserts on the argv and the environment, because
// what matters about sending a backup somewhere is where it went and whether
// the credentials stayed out of the process list.
var run = func(ctx context.Context, args, env []string, stdin io.Reader, stdout io.Writer) ([]byte, error) {
	cmd := podman.CmdContext(ctx, args...)
	cmd.Env = append(cmd.Environ(), env...)
	cmd.Stdin = stdin
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if stdout != nil {
		cmd.Stdout = stdout
	}
	err := cmd.Run()
	return stderr.Bytes(), err
}

// Upload streams one archive to the destination under its own name.
func Upload(d Destination, name string, r io.Reader) error {
	if err := d.Validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()

	args := containerArgs(d, true)
	args = append(args, "rcat", remoteName+":"+remotePath(d, name))
	out, err := run(ctx, args, environment(d), r, nil)
	if err != nil {
		return fmt.Errorf("sending %s to %s: %w\n%s", name, d.Name, err, redact(string(out), d.SecretKey))
	}
	return nil
}

// Fetch streams one archive back from the destination, which is what a restore
// on a rebuilt server needs: the archives are there and this machine is not the
// one that wrote them.
func Fetch(d Destination, name string, w io.Writer) error {
	if err := d.Validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()

	args := containerArgs(d, false)
	args = append(args, "cat", remoteName+":"+remotePath(d, name))
	out, err := run(ctx, args, environment(d), nil, w)
	if err != nil {
		return fmt.Errorf("fetching %s from %s: %w\n%s", name, d.Name, err, redact(string(out), d.SecretKey))
	}
	return nil
}

// List is what is actually at the destination. A schedule says what was meant
// to be sent; this says what a restore could reach.
func List(d Destination) ([]string, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()

	args := containerArgs(d, false)
	args = append(args, "lsf", remoteName+":"+strings.TrimSuffix(remotePath(d, ""), "/"))
	var listing bytes.Buffer
	out, err := run(ctx, args, environment(d), nil, &listing)
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w\n%s", d.Name, err, redact(string(out), d.SecretKey))
	}
	var names []string
	for _, line := range strings.Split(listing.String(), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasSuffix(line, "/") {
			names = append(names, line)
		}
	}
	return names, nil
}

// containerArgs runs rclone on the servlo network. An SFTP key is a file rather
// than a value, so it is mounted read-only where the configuration points.
func containerArgs(d Destination, stdin bool) []string {
	args := []string{"run", "--rm"}
	if stdin {
		args = append(args, "-i")
	}
	args = append(args, "--network", "servlo")
	for _, name := range envNames(d) {
		args = append(args, "-e", name)
	}
	if d.Kind == KindSFTP && d.KeyFile != "" {
		args = append(args, "-v", d.KeyFile+":"+d.KeyFile+":ro")
	}
	return append(args, image)
}

// remotePath is where an archive lives at the destination.
func remotePath(d Destination, name string) string {
	var base string
	if d.Kind == KindS3 {
		base = path(d.Bucket, d.Prefix)
	} else {
		base = strings.TrimSuffix(d.Path, "/")
	}
	if name == "" {
		return base + "/"
	}
	return base + "/" + name
}

func path(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.Trim(p, "/"); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "/")
}

// environment is rclone's configuration, passed as variables so the secret is
// never in the argument list. rclone reads RCLONE_CONFIG_<REMOTE>_<KEY> for a
// remote defined entirely from the environment, which is why no config file is
// written anywhere.
func environment(d Destination) []string {
	p := "RCLONE_CONFIG_" + strings.ToUpper(remoteName) + "_"
	switch d.Kind {
	case KindS3:
		env := []string{
			p + "TYPE=s3",
			p + "PROVIDER=Other",
			p + "ENDPOINT=" + d.Endpoint,
			p + "ACCESS_KEY_ID=" + d.AccessKey,
			p + "SECRET_ACCESS_KEY=" + d.SecretKey,
		}
		if d.Region != "" {
			env = append(env, p+"REGION="+d.Region)
		}
		return env
	default:
		env := []string{
			p + "TYPE=sftp",
			p + "HOST=" + d.Host,
			p + "USER=" + d.User,
		}
		if d.Port != 0 {
			env = append(env, p+"PORT="+strconv.Itoa(d.Port))
		}
		if d.KeyFile != "" {
			env = append(env, p+"KEY_FILE="+d.KeyFile)
		}
		return env
	}
}

// envNames are the variables to forward into the container, by name, so the
// values travel in this process's environment rather than in an argv.
func envNames(d Destination) []string {
	var names []string
	for _, kv := range environment(d) {
		if name, _, ok := strings.Cut(kv, "="); ok {
			names = append(names, name)
		}
	}
	return names
}

// redact keeps a tool that echoes its configuration out of the error text.
func redact(text, secret string) string {
	text = strings.TrimSpace(text)
	if len(secret) >= 8 {
		text = strings.ReplaceAll(text, secret, "****")
	}
	return text
}
