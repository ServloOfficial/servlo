package config

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Where a site's outgoing mail goes.
//
// Servlo runs no mail server and never will (CLAUDE.md §3.7). A site that sends
// mail sends it through somebody else's SMTP, and this is where the account for
// that is kept: one entry per site, plus the panel's own (S13.2).
//
// In their own file rather than in the site registry, for the same reason the
// deploy webhook secrets are: the registry is 0644 and readable by anything on
// the box, and an SMTP password is a credential that can send mail as the
// operator's domain. This file is 0600 and holds nothing else.

// SMTP transport security. Named rather than a bare bool because "off" and
// "upgrade after connecting" are different enough to get wrong: a provider on
// 465 speaks TLS from the first byte, one on 587 speaks plaintext until
// STARTTLS, and connecting the wrong way looks like a hang, not an error.
const (
	SMTPNone        = "none"
	SMTPStartTLS    = "starttls"
	SMTPImplicitTLS = "tls"
)

var smtpEncryptions = map[string]bool{
	SMTPNone: true, SMTPStartTLS: true, SMTPImplicitTLS: true,
}

// SMTPSettings is one SMTP account.
type SMTPSettings struct {
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	// Encryption is one of the SMTP* constants above.
	Encryption string `json:"encryption,omitempty"`
	// FromAddress and FromName are the envelope sender and the display name
	// mail goes out as. The provider usually insists the address is one it has
	// verified, so it is a setting rather than something servlo derives.
	FromAddress string `json:"from_address,omitempty"`
	FromName    string `json:"from_name,omitempty"`
}

// RedactedSMTP is SMTPSettings as the panel is allowed to see them: everything
// except the password, plus whether one is stored, which is all the form needs
// to render "leave blank to keep the current password".
type RedactedSMTP struct {
	Host        string `json:"host,omitempty"`
	Port        int    `json:"port,omitempty"`
	Username    string `json:"username,omitempty"`
	Password    string `json:"-"`
	HasPassword bool   `json:"has_password"`
	Encryption  string `json:"encryption,omitempty"`
	FromAddress string `json:"from_address,omitempty"`
	FromName    string `json:"from_name,omitempty"`
}

// Redacted drops the password and records that there was one.
func (s SMTPSettings) Redacted() RedactedSMTP {
	return RedactedSMTP{
		Host: s.Host, Port: s.Port, Username: s.Username,
		HasPassword: s.Password != "", Encryption: s.Encryption,
		FromAddress: s.FromAddress, FromName: s.FromName,
	}
}

// Configured reports whether there is enough here to send with.
func (s SMTPSettings) Configured() bool { return s.Host != "" && s.Port != 0 }

// Validate refuses settings that would not survive contact with a mail server,
// and values that would inject a header on the way there.
func (s SMTPSettings) Validate() error {
	for name, v := range map[string]string{
		"host": s.Host, "username": s.Username, "password": s.Password,
		"encryption": s.Encryption, "sender address": s.FromAddress, "sender name": s.FromName,
	} {
		if strings.ContainsAny(v, "\r\n") {
			return fmt.Errorf("the %s must not contain a line break", name)
		}
	}
	if strings.TrimSpace(s.Host) == "" {
		return fmt.Errorf("an SMTP server hostname is required")
	}
	if s.Port < 1 || s.Port > 65535 {
		return fmt.Errorf("the port %d is not a port", s.Port)
	}
	if !smtpEncryptions[s.Encryption] {
		return fmt.Errorf("%q is not a transport security setting; use none, starttls or tls", s.Encryption)
	}
	if strings.TrimSpace(s.FromAddress) == "" {
		return fmt.Errorf("a sender address is required, and most providers only accept one they have verified")
	}
	if _, err := mail.ParseAddress(s.FromAddress); err != nil {
		return fmt.Errorf("the sender address %q is not an email address", s.FromAddress)
	}
	// The sender name lands inside a quoted value in the site's env file and
	// inside a quoted display name in the From: header. A double quote or a
	// backslash ends one of those early.
	if strings.ContainsAny(s.FromName, `"\`) {
		return fmt.Errorf("the sender name must not contain a quote or a backslash")
	}
	return nil
}

// SMTPFile is where the accounts live.
func SMTPFile() string { return filepath.Join(ConfigDir(), "smtp.json") }

var smtpMu sync.Mutex

// smtpStore is the whole file. Panel is the panel's own account (S13.2) and is
// nil until one is set.
type smtpStore struct {
	Sites map[string]SMTPSettings `json:"sites,omitempty"`
	Panel *SMTPSettings           `json:"panel,omitempty"`
}

func readSMTPStore() (smtpStore, error) {
	data, err := os.ReadFile(SMTPFile())
	if err != nil {
		if os.IsNotExist(err) {
			return smtpStore{Sites: map[string]SMTPSettings{}}, nil
		}
		return smtpStore{}, err
	}
	store := smtpStore{}
	if err := json.Unmarshal(data, &store); err != nil {
		return smtpStore{}, fmt.Errorf("reading %s: %w", SMTPFile(), err)
	}
	if store.Sites == nil {
		store.Sites = map[string]SMTPSettings{}
	}
	return store, nil
}

func writeSMTPStore(store smtpStore) error {
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(SMTPFile()), 0o700); err != nil {
		return err
	}
	return writeFileAtomic(SMTPFile(), append(data, '\n'), 0o600)
}

// SiteSMTP returns a site's account. ok is false when it has none.
func SiteSMTP(siteName string) (SMTPSettings, bool, error) {
	smtpMu.Lock()
	defer smtpMu.Unlock()

	store, err := readSMTPStore()
	if err != nil {
		return SMTPSettings{}, false, err
	}
	s, ok := store.Sites[siteName]
	return s, ok, nil
}

// SaveSiteSMTP writes a site's account. An empty password on a site that
// already has one keeps the stored password: the panel is never sent it, so it
// has nothing to send back, and asking for it again on every unrelated edit is
// how an operator ends up pasting a credential into a form for the tenth time.
func SaveSiteSMTP(siteName string, s SMTPSettings) error {
	smtpMu.Lock()
	defer smtpMu.Unlock()

	store, err := readSMTPStore()
	if err != nil {
		return err
	}
	if s.Password == "" {
		s.Password = store.Sites[siteName].Password
	}
	store.Sites[siteName] = s
	return writeSMTPStore(store)
}

// DeleteSiteSMTP forgets a site's account, credential included.
func DeleteSiteSMTP(siteName string) error {
	smtpMu.Lock()
	defer smtpMu.Unlock()

	store, err := readSMTPStore()
	if err != nil {
		return err
	}
	if _, ok := store.Sites[siteName]; !ok {
		return nil
	}
	delete(store.Sites, siteName)
	return writeSMTPStore(store)
}

// PanelSMTP returns the panel's own account, the one alerts go out through. ok
// is false until an operator sets one, and every alert path treats that as
// "there is nowhere to send this" rather than an error: a panel with no mail
// configured is the ordinary case, not a broken one.
func PanelSMTP() (SMTPSettings, bool, error) {
	smtpMu.Lock()
	defer smtpMu.Unlock()

	store, err := readSMTPStore()
	if err != nil {
		return SMTPSettings{}, false, err
	}
	if store.Panel == nil {
		return SMTPSettings{}, false, nil
	}
	return *store.Panel, true, nil
}

// SavePanelSMTP writes the panel's account. An empty password keeps the stored
// one, the same as a site's.
func SavePanelSMTP(s SMTPSettings) error {
	smtpMu.Lock()
	defer smtpMu.Unlock()

	store, err := readSMTPStore()
	if err != nil {
		return err
	}
	if s.Password == "" && store.Panel != nil {
		s.Password = store.Panel.Password
	}
	store.Panel = &s
	return writeSMTPStore(store)
}

// DeletePanelSMTP forgets it, credential included. Alerts then stay in the
// panel and the audit log, which is where they were before.
func DeletePanelSMTP() error {
	smtpMu.Lock()
	defer smtpMu.Unlock()

	store, err := readSMTPStore()
	if err != nil {
		return err
	}
	if store.Panel == nil {
		return nil
	}
	store.Panel = nil
	return writeSMTPStore(store)
}
