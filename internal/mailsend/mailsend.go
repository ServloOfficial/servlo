// Package mailsend is the one place servlo talks SMTP.
//
// It sits below both callers on purpose. A site's mail settings live in
// internal/sitesmtp, which reaches half the tree to resolve a framework's env
// keys, and the panel's own alerts are raised from internal/certs, which that
// half already depends on. One shared dialer here is what lets both send
// without either importing the other.
//
// Servlo runs no mail server (CLAUDE.md §3.7): everything here is a client
// talking to somebody else's.
package mailsend

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/ServloOfficial/servlo/internal/config"
)

// Timeout bounds the whole SMTP exchange. A provider that accepts the
// connection and then stops talking is common enough (a greylist, a firewall
// mid-handshake) that without this the worker sending the message blocks until
// the operating system gives up, which is minutes.
var Timeout = 15 * time.Second

// Send delivers one message through acct.
//
// Every error carries what the mail server itself said. "Could not send" tells
// an operator nothing they can act on; "550 5.7.1 Sender address not verified"
// tells them exactly which setting is wrong.
func Send(acct config.SMTPSettings, to, subject, body string) error {
	if err := acct.Validate(); err != nil {
		return err
	}
	recipient, err := mail.ParseAddress(to)
	if err != nil {
		return fmt.Errorf("%q is not an email address", to)
	}
	if strings.ContainsAny(subject, "\r\n") {
		return fmt.Errorf("the subject must not contain a line break")
	}

	addr := net.JoinHostPort(acct.Host, strconv.Itoa(acct.Port))
	dialer := &net.Dialer{Timeout: Timeout}
	tlsConf := &tls.Config{ServerName: acct.Host, MinVersion: tls.VersionTLS12}

	var conn net.Conn
	if acct.Encryption == config.SMTPImplicitTLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsConf)
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("connecting to %s: %w", addr, err)
	}
	defer conn.Close()
	// One deadline over the whole exchange rather than per read: a server that
	// answers each command slowly enough is otherwise a hang with extra steps.
	if err := conn.SetDeadline(time.Now().Add(Timeout)); err != nil {
		return err
	}

	client, err := smtp.NewClient(conn, acct.Host)
	if err != nil {
		return fmt.Errorf("%s did not answer as a mail server: %w", addr, err)
	}
	defer client.Close()

	if acct.Encryption == config.SMTPStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("%s does not offer STARTTLS; use implicit TLS or no encryption", addr)
		}
		if err := client.StartTLS(tlsConf); err != nil {
			return fmt.Errorf("starting TLS with %s: %w", addr, err)
		}
	}
	if acct.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", acct.Username, acct.Password, acct.Host)); err != nil {
			return fmt.Errorf("the mail server refused the login: %w", err)
		}
	}
	if err := client.Mail(acct.FromAddress); err != nil {
		return fmt.Errorf("the mail server refused the sender %s: %w", acct.FromAddress, err)
	}
	if err := client.Rcpt(recipient.Address); err != nil {
		return fmt.Errorf("the mail server refused the recipient %s: %w", recipient.Address, err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("the mail server refused the message: %w", err)
	}
	if _, err := w.Write(message(acct, recipient.Address, subject, body)); err != nil {
		return fmt.Errorf("sending the message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("the mail server rejected the message: %w", err)
	}
	return client.Quit()
}

// message builds the RFC 5322 bytes. Every value in it has already been through
// Validate or ParseAddress, so none of them can carry the line break that would
// turn one header into two.
func message(acct config.SMTPSettings, to, subject, body string) []byte {
	from := (&mail.Address{Name: acct.FromName, Address: acct.FromAddress}).String()
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
	b.WriteString("\r\n")
	return []byte(b.String())
}

// SendPanelAlert emails through the panel's own SMTP account.
//
// No account configured is not an error: SMTP is optional, the alert is already
// in the panel, and treating a server whose operator reads the dashboard as a
// failure would put a permanent complaint beside every alert.
func SendPanelAlert(subject, body string) error {
	acct, ok, err := config.PanelSMTP()
	if err != nil {
		return err
	}
	if !ok || !acct.Configured() {
		return nil
	}
	return Send(acct, acct.FromAddress, subject, body)
}
