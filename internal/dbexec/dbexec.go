// Package dbexec runs a statement an engine's definition declared, against a
// connection, wherever that connection lives.
//
// Three things need this and each needed it slightly differently: issuing a
// site's database account, provisioning a schema on a managed server, and
// dumping a database for a backup. What they share is the whole hard part —
// where the client runs, how the administrator's password reaches it without
// appearing in an argument list, how a managed connection's TLS is spelled in
// that engine's own flags, and which values a declared statement is allowed to
// be handed. Writing that three times would mean three places for the
// injection guard to drift.
//
// Nothing here knows any SQL. The statements are the store's.
package dbexec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/dbconn"
	"github.com/ServloOfficial/servlo/internal/podman"
)

// ContainerCACert is where a managed connection's CA certificate is mounted
// inside the container running the client, so a declared flag has a fixed path
// to name.
const ContainerCACert = "/etc/servlo/db-ca.crt"

// DefaultTimeout bounds one statement. Creating a user or granting on a schema
// is quick; this is here so an unreachable managed host reports rather than
// holding a panel request open. A dump overrides it, because a large database
// legitimately takes longer than anything else here.
const DefaultTimeout = 60 * time.Second

// Run is the seam every test in the packages above asserts through: what matters
// about a declared statement is the argv it became and where it was aimed.
var Run = func(ctx context.Context, args []string, env []string, stdout io.Writer) ([]byte, error) {
	cmd := podman.CmdContext(ctx, args...)
	cmd.Env = append(cmd.Environ(), env...)
	if stdout != nil {
		var stderr bytes.Buffer
		cmd.Stdout = stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		return stderr.Bytes(), err
	}
	return cmd.CombinedOutput()
}

// Vars is everything a declared statement can ask for.
//
// A local database is addressed as 127.0.0.1 because the client runs inside the
// engine's own container; a managed one by the host the provider gave. The TLS
// flags are the preset's, keyed by the mode the connection asks for, so the
// engine's own spelling stays in the store.
//
// extra carries whatever the caller's own statements need on top — the account
// name and its password for dbuser, the database for a dump — and is checked
// against the same whitelist as everything else.
func Vars(spec *config.EntitySpec, conn dbconn.Connection, extra map[string]string) (map[string]string, error) {
	host := conn.Host
	if conn.Local() {
		host = "127.0.0.1"
	}
	vars := map[string]string{
		"host":       host,
		"port":       strconv.Itoa(conn.Port),
		"admin_user": conn.User,
		"ca_cert":    ContainerCACert,
		"tls_flags":  "",
	}
	for k, v := range extra {
		vars[k] = v
	}
	if !conn.Local() && conn.TLSMode != dbconn.TLSOff {
		flags, declared := spec.TLS[conn.TLSMode]
		if !declared {
			return nil, fmt.Errorf("connection %q asks for TLS mode %q, which this engine's definition does not say how to spell", conn.Name, conn.TLSMode)
		}
		// The flags are the one value that may itself name another: verify-ca
		// has to point the client at where the certificate was mounted.
		vars["tls_flags"] = strings.ReplaceAll(flags, "{{ca_cert}}", ContainerCACert)
	}
	return vars, nil
}

// CommandArgs is where the client runs: inside the engine's container for a
// database servlo runs, and ephemerally on the servlo network for one it does
// not. The administrator's password goes in through the environment either way,
// so it is never in an argument list the rest of the machine can read.
// The credentials are named, not spelled: podman reads `-e NAME` from its own
// environment, so the value travels in this process's env rather than in an
// argv that anything running as the same user can read out of ps.
func CommandArgs(spec *config.EntitySpec, conn dbconn.Connection, shellCmd string) ([]string, error) {
	if conn.Local() {
		args := []string{"exec"}
		for _, name := range envNames(conn) {
			args = append(args, "--env", name)
		}
		return append(args, "servlo-"+conn.Service, "sh", "-c", shellCmd), nil
	}
	if strings.TrimSpace(spec.Image) == "" {
		return nil, fmt.Errorf("this engine's definition names no client image, so there is nothing to run the statement in for a database servlo does not host")
	}
	// --entrypoint sh for the same reason the entity runner does it: an engine
	// image makes its server or its client the entrypoint, which would swallow
	// the command as its own arguments.
	args := []string{"run", "--rm", "--network", "servlo", "--entrypoint", "sh"}
	for _, name := range envNames(conn) {
		args = append(args, "-e", name)
	}
	if conn.TLSMode == dbconn.TLSVerifyCA && conn.CACert != "" {
		args = append(args, "-v", conn.CACert+":"+ContainerCACert+":ro")
	}
	return append(args, spec.Image, "-c", shellCmd), nil
}

// envNames are the credential variables to forward by name, in a stable order
// so the argv a test asserts on does not depend on map iteration.
func envNames(conn dbconn.Connection) []string {
	names := make([]string, 0, 2)
	for _, kv := range conn.ClientEnv() {
		if name, _, ok := strings.Cut(kv, "="); ok {
			names = append(names, name)
		}
	}
	return names
}

// valuePatterns says what each value a declared statement may be handed is
// allowed to contain.
//
// This is the injection guard, and it is a whitelist rather than an escape:
// every value here is either generated by servlo or read from a connection the
// operator configured, and none has any business carrying a quote, a backtick
// or a shell metacharacter. A value that does is a bug somewhere earlier, and
// running it would be running whatever the bug wrote.
var valuePatterns = map[string]*regexp.Regexp{
	"name":          regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`),
	"user_password": regexp.MustCompile(`^[A-Za-z0-9]{24,128}$`),
	"database":      regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$`),
	"host":          regexp.MustCompile(`^[A-Za-z0-9._-]{1,253}$`),
	"port":          regexp.MustCompile(`^[1-9][0-9]{0,4}$`),
	"admin_user":    regexp.MustCompile(`^[A-Za-z0-9_.-]{1,63}$`),
	"ca_cert":       regexp.MustCompile(`^[A-Za-z0-9._/-]{0,255}$`),
	// The TLS flags come from the preset itself rather than from anything a
	// user typed, so what is checked is that expansion left nothing behind.
	"tls_flags": regexp.MustCompile(`^[A-Za-z0-9=?&_.:/ -]*$`),
}

// Expand fills a declared statement's placeholders, refusing any value that is
// not what its placeholder is allowed to hold, and refusing a statement that
// still has a placeholder left in it.
func Expand(command string, vars map[string]string) (string, error) {
	out := strings.TrimSpace(command)
	for key, value := range vars {
		placeholder := "{{" + key + "}}"
		// A value whose placeholder is not in this statement never reaches a
		// shell, so it is not checked. Checking it anyway would refuse a local
		// dump for having no port, on a statement that does not ask for one.
		if !strings.Contains(out, placeholder) {
			continue
		}
		pattern, known := valuePatterns[key]
		if !known {
			return "", fmt.Errorf("%q is not a value a declared database statement can take", key)
		}
		if !pattern.MatchString(value) {
			return "", fmt.Errorf("%q is not a usable %s for a declared database statement", value, key)
		}
		out = strings.ReplaceAll(out, placeholder, value)
	}
	if i := strings.Index(out, "{{"); i >= 0 {
		return "", fmt.Errorf("this engine's definition asks for %s, which servlo does not supply", out[i:min(i+40, len(out))])
	}
	return out, nil
}

// Redact keeps a client that echoes its arguments out of the error, the audit
// log and every screenshot of either.
//
// The length floor is not caution about short passwords, it is about short
// strings: replacing every occurrence of a two-character secret would turn the
// engine's message into asterisks and lose what it was trying to say.
func Redact(text string, secrets ...string) string {
	for _, s := range secrets {
		if len(s) >= 8 {
			text = strings.ReplaceAll(text, s, "****")
		}
	}
	return text
}

// WithStdin marks a command as one that reads its input, which podman needs
// told before it will connect a pipe.
func WithStdin(args []string) []string {
	if len(args) == 0 {
		return args
	}
	// The flag has to sit with the subcommand, not at the end where the shell
	// command already is.
	return append([]string{args[0], "-i"}, args[1:]...)
}

// RunWithInput runs a declared statement that reads a stream, which is what
// loading a dump is. Only what the engine said comes back, so a multi-gigabyte
// dump is never held in memory on either side.
var RunWithInput = func(ctx context.Context, args []string, env []string, stdin io.Reader) ([]byte, error) {
	cmd := podman.CmdContext(ctx, args...)
	cmd.Env = append(cmd.Environ(), env...)
	cmd.Stdin = stdin
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.Bytes(), err
}
