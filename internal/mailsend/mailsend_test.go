package mailsend

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/realrashid/servlo/internal/config"
)

// fakeSMTP is a one-connection SMTP server. reject, when set, is the reply it
// gives to the first MAIL FROM, which is how a real provider refuses an
// unverified sender.
func fakeSMTP(t *testing.T, reject string) (host string, port int, got *strings.Builder) {
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
			case strings.HasPrefix(line, "EHLO"):
				fmt.Fprint(conn, "250-fake\r\n250 AUTH PLAIN LOGIN\r\n")
			case strings.HasPrefix(line, "HELO"):
				fmt.Fprint(conn, "250 fake\r\n")
			case strings.HasPrefix(line, "AUTH"):
				fmt.Fprint(conn, "235 ok\r\n")
			case strings.HasPrefix(line, "MAIL FROM") && reject != "":
				fmt.Fprint(conn, reject+"\r\n")
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

// A host that accepts the connection and then says nothing is the failure mode
// a missing timeout turns into a hung panel worker.
func TestSend_givesUpRatherThanHanging(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		// Accept and never speak: no greeting, no close.
		<-time.After(30 * time.Second)
		conn.Close()
	}()

	old := Timeout
	Timeout = 300 * time.Millisecond
	t.Cleanup(func() { Timeout = old })

	acct := config.SMTPSettings{
		Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port,
		Encryption: config.SMTPNone, FromAddress: "hello@acme.test",
	}

	done := make(chan error, 1)
	go func() { done <- Send(acct, "ops@acme.test", "hello", "body") }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a silent server reported a successful send")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SendTest hung on a server that never replied")
	}
}

// Kept so the TLS branch is exercised at all: an implicit-TLS account must
// present a TLS ClientHello rather than plaintext.
func TestSend_speaksTLSFirstOnAnImplicitTLSAccount(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	sawTLS := make(chan bool, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		first := make([]byte, 1)
		if _, err := conn.Read(first); err != nil {
			sawTLS <- false
			return
		}
		// 0x16 is the TLS handshake record type.
		sawTLS <- first[0] == 0x16
	}()

	old := Timeout
	Timeout = time.Second
	t.Cleanup(func() { Timeout = old })

	acct := config.SMTPSettings{
		Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port,
		Encryption: config.SMTPImplicitTLS, FromAddress: "hello@acme.test",
	}
	_ = Send(acct, "ops@acme.test", "hello", "body")

	select {
	case ok := <-sawTLS:
		if !ok {
			t.Error("an implicit-TLS account sent plaintext to the server")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the server never saw a connection")
	}
}

// Send delivers through whatever the account names, which is the one thing the
// per-site card and the panel's own alerts share.
func TestSend_deliversThroughTheConfiguredServer(t *testing.T) {
	host, port, transcript := fakeSMTP(t, "")
	acct := config.SMTPSettings{
		Host: host, Port: port, Username: "token", Password: "s3cret",
		Encryption: config.SMTPNone, FromAddress: "alerts@acme.test", FromName: "Servlo",
	}

	if err := Send(acct, "ops@acme.test", "Renewal failed", "acme.test could not renew."); err != nil {
		t.Fatalf("Send: %v", err)
	}
	sent := transcript.String()
	for _, want := range []string{
		"MAIL FROM:<alerts@acme.test>", "RCPT TO:<ops@acme.test>",
		"Subject: Renewal failed", `From: "Servlo" <alerts@acme.test>`,
	} {
		if !strings.Contains(sent, want) {
			t.Errorf("the transcript is missing %q:\n%s", want, sent)
		}
	}
}

// The provider's own words are the whole value of a test send. "Could not send"
// tells an operator nothing; "550 sender not verified" tells them what to fix.
func TestSend_reportsTheServersOwnRefusal(t *testing.T) {
	host, port, _ := fakeSMTP(t, "550 5.7.1 Sender address not verified")
	acct := config.SMTPSettings{
		Host: host, Port: port, Encryption: config.SMTPNone, FromAddress: "hello@acme.test",
	}

	err := Send(acct, "ops@acme.test", "hello", "body")
	if err == nil {
		t.Fatal("a refused send reported success")
	}
	if !strings.Contains(err.Error(), "Sender address not verified") {
		t.Errorf("error %q does not carry the server's own reply", err)
	}
}

// A subject or recipient carrying a line break is header injection: the second
// line becomes a header the operator did not write.
//
// Pointed at a server that would happily accept, so "it returned an error" is
// not enough: the message must be refused before anything is dialled, and the
// error has to name the field rather than a connection that never happened.
func TestSend_refusesHeaderInjection(t *testing.T) {
	host, port, transcript := fakeSMTP(t, "")
	acct := config.SMTPSettings{
		Host: host, Port: port, Encryption: config.SMTPNone, FromAddress: "hello@acme.test",
	}

	err := Send(acct, "ops@acme.test\r\nBcc: attacker@evil.test", "hello", "body")
	if err == nil || !strings.Contains(err.Error(), "not an email address") {
		t.Errorf("a recipient carrying a second header line gave %v", err)
	}
	err = Send(acct, "ops@acme.test", "hello\r\nBcc: attacker@evil.test", "body")
	if err == nil || !strings.Contains(err.Error(), "line break") {
		t.Errorf("a subject carrying a second header line gave %v", err)
	}
	if transcript.Len() != 0 {
		t.Errorf("the server was contacted anyway:\n%s", transcript.String())
	}
}

// An account servlo would refuse to store must not be dialled either: Send is
// reachable with settings that never went through the panel's form.
func TestSend_refusesAnInvalidAccount(t *testing.T) {
	if err := Send(config.SMTPSettings{Host: "h", Port: 0}, "ops@acme.test", "hi", "body"); err == nil {
		t.Error("an account with no port was dialled")
	}
}
