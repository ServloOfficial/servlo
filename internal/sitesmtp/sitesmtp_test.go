package sitesmtp

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ServloOfficial/servlo/internal/config"
)

// laravelish is a framework definition that declares its mail keys the way the
// store does. Nothing in this package knows those names; it reads them here.
const laravelish = `name: mailtest
version: "1"
label: Mail Test
detect:
  - file: artisan
env:
  file: .env
  format: dotenv
  smtp:
    vars:
      - MAIL_MAILER=smtp
      - MAIL_HOST={{smtp_host}}
      - MAIL_PORT={{smtp_port}}
      - MAIL_USERNAME={{smtp_user}}
      - MAIL_PASSWORD={{smtp_password}}
      - MAIL_ENCRYPTION={{smtp_encryption}}
      - MAIL_FROM_ADDRESS={{smtp_from_address}}
`

// wordpressish writes into wp-config.php through the php-const format, which is
// the other half of the story: the same declaration, a different file shape.
const wordpressish = `name: wptest
version: "1"
label: WP Test
detect:
  - file: wp-config.php
env:
  fallback_file: wp-config.php
  fallback_format: php-const
  smtp:
    vars:
      - SMTP_HOST={{smtp_host}}
      - SMTP_PORT={{smtp_port}}
      - SMTP_PASS={{smtp_password}}
`

// site registers one site with a framework definition on disk and an SMTP
// account, and returns it.
func site(t *testing.T, definition, envName, envBody string, smtp *config.SMTPSettings) *config.Site {
	t.Helper()
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	fwDir := config.FrameworksDir()
	if err := os.MkdirAll(fwDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var name string
	for _, line := range strings.Split(definition, "\n") {
		if strings.HasPrefix(line, "name: ") {
			name = strings.TrimPrefix(line, "name: ")
		}
	}
	if err := os.WriteFile(filepath.Join(fwDir, name+".yaml"), []byte(definition), 0o644); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "acme")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, envName), []byte(envBody), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := &config.SiteRegistry{Sites: []config.Site{{
		Name: "acme", Domains: []string{"acme.test"}, Path: path, Framework: name,
	}}}
	if err := config.SaveSites(reg); err != nil {
		t.Fatal(err)
	}
	if smtp != nil {
		if err := config.SaveSiteSMTP("acme", *smtp); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := config.LoadSites()
	if err != nil {
		t.Fatal(err)
	}
	return &loaded.Sites[0]
}

func account() config.SMTPSettings {
	return config.SMTPSettings{
		Host: "smtp.postmarkapp.com", Port: 587, Username: "token", Password: "s3cret",
		Encryption: config.SMTPStartTLS, FromAddress: "hello@acme.test", FromName: "Acme",
	}
}

func TestWriteEnv_writesTheKeysTheDefinitionDeclares(t *testing.T) {
	acct := account()
	s := site(t, laravelish, ".env", "APP_NAME=Acme\nDB_HOST=127.0.0.1\n", &acct)

	keys, err := WriteEnv(s)
	if err != nil {
		t.Fatalf("WriteEnv: %v", err)
	}
	if len(keys) != 7 {
		t.Errorf("wrote %d keys, want the 7 the definition declares: %v", len(keys), keys)
	}
	body, err := os.ReadFile(filepath.Join(s.Path, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"MAIL_MAILER=smtp", "MAIL_HOST=smtp.postmarkapp.com", "MAIL_PORT=587",
		"MAIL_USERNAME=token", "MAIL_PASSWORD=s3cret", "MAIL_ENCRYPTION=tls",
		"MAIL_FROM_ADDRESS=hello@acme.test",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf(".env is missing %q:\n%s", want, body)
		}
	}
	if !strings.Contains(string(body), "APP_NAME=Acme") {
		t.Error("writing the mail keys dropped an unrelated key")
	}
}

// The file holds a password once servlo has written one into it, so it comes
// out of this at 0600 whatever it went in as (CLAUDE.md §3.7).
func TestWriteEnv_leavesTheEnvFilePrivate(t *testing.T) {
	acct := account()
	s := site(t, laravelish, ".env", "APP_NAME=Acme\n", &acct)

	if _, err := WriteEnv(s); err != nil {
		t.Fatalf("WriteEnv: %v", err)
	}
	info, err := os.Stat(filepath.Join(s.Path, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf(".env mode = %o, want 600", got)
	}
}

func TestWriteEnv_writesWpConfigThroughTheDeclaredFormat(t *testing.T) {
	acct := account()
	s := site(t, wordpressish, "wp-config.php",
		"<?php\ndefine( 'DB_NAME', 'acme' );\n", &acct)

	if _, err := WriteEnv(s); err != nil {
		t.Fatalf("WriteEnv: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(s.Path, "wp-config.php"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"'SMTP_HOST'", "smtp.postmarkapp.com", "'SMTP_PASS'"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("wp-config.php is missing %q:\n%s", want, body)
		}
	}
}

// A framework whose definition says nothing about mail cannot be wired, and
// saying so is more use than writing MAIL_HOST into a file that ignores it.
func TestWriteEnv_refusesAFrameworkThatDeclaresNoMailKeys(t *testing.T) {
	acct := account()
	quiet := strings.SplitN(laravelish, "  smtp:", 2)[0]
	s := site(t, quiet, ".env", "APP_NAME=Acme\n", &acct)

	_, err := WriteEnv(s)
	if err == nil {
		t.Fatal("a framework with no declared mail keys was written anyway")
	}
	if !strings.Contains(err.Error(), "mailtest") {
		t.Errorf("error %q does not name the framework whose definition is silent", err)
	}
}

func TestWriteEnv_refusesASiteWithNoAccount(t *testing.T) {
	s := site(t, laravelish, ".env", "APP_NAME=Acme\n", nil)

	if _, err := WriteEnv(s); err == nil {
		t.Fatal("a site with no SMTP account had mail keys written for it")
	}
}

// EnvKeys is what the card lists before the operator presses save, so it has to
// name the keys without ever handing back the value of the password one.
func TestEnvKeys_namesTheKeysWithoutTheSecret(t *testing.T) {
	acct := account()
	s := site(t, laravelish, ".env", "APP_NAME=Acme\n", &acct)

	keys, err := EnvKeys(s)
	if err != nil {
		t.Fatalf("EnvKeys: %v", err)
	}
	joined := strings.Join(keys, " ")
	if !strings.Contains(joined, "MAIL_PASSWORD") {
		t.Errorf("keys = %v, want the password key named", keys)
	}
	if strings.Contains(joined, "s3cret") {
		t.Errorf("EnvKeys leaked the password: %v", keys)
	}
	for i := 1; i < len(keys); i++ {
		if keys[i-1] > keys[i] {
			t.Errorf("keys are not sorted, so the card's list reshuffles on every load: %v", keys)
			break
		}
	}
}

// ── SendTest ────────────────────────────────────────────────────────────────
//
// The transport itself is internal/mailsend's, and tested there. What is this
// package's is picking up the site's own account and refusing to send without
// one.

// acceptingSMTP is a one-connection server that accepts everything.
func acceptingSMTP(t *testing.T) (string, int, *strings.Builder) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	transcript := &strings.Builder{}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		r := bufio.NewReader(conn)
		fmt.Fprint(conn, "220 fake ESMTP\r\n")
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			transcript.WriteString(line)
			if inData {
				if strings.TrimRight(line, "\r\n") == "." {
					inData = false
					fmt.Fprint(conn, "250 queued\r\n")
				}
				continue
			}
			switch {
			case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
				fmt.Fprint(conn, "250 fake\r\n")
			case strings.HasPrefix(line, "DATA"):
				inData = true
				fmt.Fprint(conn, "354 go ahead\r\n")
			case strings.HasPrefix(line, "QUIT"):
				fmt.Fprint(conn, "221 bye\r\n")
				return
			default:
				fmt.Fprint(conn, "250 ok\r\n")
			}
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port, transcript
}

// The test goes out as the site, to whoever the operator named, and says which
// site it is about: an operator testing four sites in a row needs to know which
// message is which.
func TestSendTest_sendsAsTheSiteThroughItsOwnAccount(t *testing.T) {
	host, port, transcript := acceptingSMTP(t)
	acct := config.SMTPSettings{
		Host: host, Port: port, Encryption: config.SMTPNone,
		FromAddress: "hello@acme.test", FromName: "Acme",
	}
	s := site(t, laravelish, ".env", "APP_NAME=Acme\n", &acct)

	if err := SendTest(s, "ops@acme.test"); err != nil {
		t.Fatalf("SendTest: %v", err)
	}
	sent := transcript.String()
	for _, want := range []string{
		"MAIL FROM:<hello@acme.test>", "RCPT TO:<ops@acme.test>", "Subject: Servlo test message from acme.test",
	} {
		if !strings.Contains(sent, want) {
			t.Errorf("the transcript is missing %q:\n%s", want, sent)
		}
	}
}

func TestSendTest_refusesASiteWithNoAccount(t *testing.T) {
	s := site(t, laravelish, ".env", "APP_NAME=Acme\n", nil)

	if err := SendTest(s, "ops@acme.test"); err == nil {
		t.Fatal("a site with no SMTP account sent a test")
	}
}

func TestSendTest_refusesARecipientThatIsNotAnAddress(t *testing.T) {
	acct := account()
	s := site(t, laravelish, ".env", "APP_NAME=Acme\n", &acct)

	if err := SendTest(s, "ops@acme.test\r\nBcc: attacker@evil.test"); err == nil {
		t.Fatal("a recipient carrying a second header line was accepted")
	}
}
