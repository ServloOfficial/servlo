package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func smtpTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

// The settings hold a password, so the file they live in is 0600 from the
// moment it exists. The site registry is 0644 and is the wrong place for this.
func TestSaveSiteSMTP_writesAPrivateFile(t *testing.T) {
	smtpTestHome(t)

	if err := SaveSiteSMTP("acme", SMTPSettings{
		Host: "smtp.postmarkapp.com", Port: 587, Username: "acme",
		Password: "hunter2", Encryption: SMTPStartTLS, FromAddress: "hello@acme.test",
	}); err != nil {
		t.Fatalf("SaveSiteSMTP: %v", err)
	}

	info, err := os.Stat(SMTPFile())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %o, want 600", got)
	}
	if dir := filepath.Dir(SMTPFile()); dir != ConfigDir() {
		t.Errorf("SMTP settings live in %s, want the config dir", dir)
	}
}

func TestSiteSMTP_roundTrips(t *testing.T) {
	smtpTestHome(t)

	want := SMTPSettings{
		Host: "smtp.example.com", Port: 465, Username: "u", Password: "p",
		Encryption: SMTPImplicitTLS, FromAddress: "a@b.test", FromName: "Acme Supply",
	}
	if err := SaveSiteSMTP("acme", want); err != nil {
		t.Fatalf("SaveSiteSMTP: %v", err)
	}
	got, ok, err := SiteSMTP("acme")
	if err != nil || !ok {
		t.Fatalf("SiteSMTP: ok=%v err=%v", ok, err)
	}
	if got != want {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
	if _, ok, _ := SiteSMTP("other"); ok {
		t.Error("a site that was never configured reported settings")
	}
}

// One site's settings must not disturb another's: the store is one file, so a
// save that rewrites the whole map is the way to lose the other rows.
func TestSaveSiteSMTP_keepsOtherSites(t *testing.T) {
	smtpTestHome(t)

	base := SMTPSettings{Host: "a.example.com", Port: 587, Encryption: SMTPStartTLS, FromAddress: "a@a.test"}
	if err := SaveSiteSMTP("one", base); err != nil {
		t.Fatal(err)
	}
	base.Host = "b.example.com"
	if err := SaveSiteSMTP("two", base); err != nil {
		t.Fatal(err)
	}
	got, ok, _ := SiteSMTP("one")
	if !ok || got.Host != "a.example.com" {
		t.Errorf("first site's settings = %+v, want a.example.com", got)
	}
}

func TestDeleteSiteSMTP_forgetsTheCredential(t *testing.T) {
	smtpTestHome(t)

	if err := SaveSiteSMTP("acme", SMTPSettings{
		Host: "smtp.example.com", Port: 587, Password: "hunter2",
		Encryption: SMTPStartTLS, FromAddress: "a@b.test",
	}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteSiteSMTP("acme"); err != nil {
		t.Fatalf("DeleteSiteSMTP: %v", err)
	}
	if _, ok, _ := SiteSMTP("acme"); ok {
		t.Error("settings survived their deletion")
	}
	data, err := os.ReadFile(SMTPFile())
	if err != nil {
		t.Fatalf("reading the store: %v", err)
	}
	if strings.Contains(string(data), "hunter2") {
		t.Error("the password is still on disk after the settings were deleted")
	}
}

// A blank password on a save that names an already-configured site means "leave
// it alone", because the panel never sends the stored password back to the
// browser and so cannot send it again on the next save.
func TestSaveSiteSMTP_blankPasswordKeepsTheStoredOne(t *testing.T) {
	smtpTestHome(t)

	first := SMTPSettings{Host: "smtp.example.com", Port: 587, Username: "u", Password: "hunter2", Encryption: SMTPStartTLS, FromAddress: "a@b.test"}
	if err := SaveSiteSMTP("acme", first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.Password = ""
	second.FromName = "Acme"
	if err := SaveSiteSMTP("acme", second); err != nil {
		t.Fatal(err)
	}
	got, _, _ := SiteSMTP("acme")
	if got.Password != "hunter2" {
		t.Errorf("password = %q, want the stored one kept", got.Password)
	}
	if got.FromName != "Acme" {
		t.Errorf("FromName = %q, want the new value", got.FromName)
	}
}

func TestSMTPSettings_Validate(t *testing.T) {
	ok := SMTPSettings{Host: "smtp.example.com", Port: 587, Encryption: SMTPStartTLS, FromAddress: "a@b.test"}
	if err := ok.Validate(); err != nil {
		t.Errorf("a complete setting was refused: %v", err)
	}
	cases := map[string]SMTPSettings{
		"no host":          {Port: 587, Encryption: SMTPStartTLS, FromAddress: "a@b.test"},
		"port zero":        {Host: "h", Encryption: SMTPStartTLS, FromAddress: "a@b.test"},
		"port too high":    {Host: "h", Port: 70000, Encryption: SMTPStartTLS, FromAddress: "a@b.test"},
		"unknown crypto":   {Host: "h", Port: 587, Encryption: "ssl", FromAddress: "a@b.test"},
		"no from address":  {Host: "h", Port: 587, Encryption: SMTPStartTLS},
		"from not address": {Host: "h", Port: 587, Encryption: SMTPStartTLS, FromAddress: "nobody"},
		// A newline in a header value is header injection: the value lands in a
		// From: line, and a second line after it is a header the operator did
		// not write.
		"newline in from name": {Host: "h", Port: 587, Encryption: SMTPStartTLS, FromAddress: "a@b.test", FromName: "Acme\r\nBcc: x@y.z"},
		"newline in host":      {Host: "h\nX", Port: 587, Encryption: SMTPStartTLS, FromAddress: "a@b.test"},
	}
	for name, s := range cases {
		if err := s.Validate(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// Redacted is what the panel is allowed to see. A response that carries the
// password is a password in a browser cache, a proxy log and a screenshot.
func TestSMTPSettings_Redacted(t *testing.T) {
	s := SMTPSettings{Host: "h", Port: 587, Username: "u", Password: "hunter2", Encryption: SMTPStartTLS, FromAddress: "a@b.test"}
	r := s.Redacted()
	if r.Password != "" {
		t.Errorf("Redacted kept the password: %q", r.Password)
	}
	if !r.HasPassword {
		t.Error("Redacted must still say a password is stored, or the form cannot tell")
	}
	if r.Host != "h" || r.Username != "u" {
		t.Errorf("Redacted dropped a non-secret field: %+v", r)
	}
}

// The sender name is written into a quoted env value and a quoted From: display
// name, so a quote in it ends the value early and everything after it becomes
// something the framework, or the mail server, reads as its own.
func TestSMTPSettings_Validate_refusesAQuotedSenderName(t *testing.T) {
	for _, name := range []string{`Acme" MAIL_HOST="evil.test`, `Acme\`} {
		s := SMTPSettings{Host: "h", Port: 587, Encryption: SMTPStartTLS, FromAddress: "a@b.test", FromName: name}
		if err := s.Validate(); err == nil {
			t.Errorf("sender name %q was accepted", name)
		}
	}
}

// The panel's own account lives in the same 0600 file as the sites'. It is a
// separate setting: the panel emails the operator, a site emails its customers,
// and they are usually not the same provider.
func TestPanelSMTP_roundTripsAlongsideTheSites(t *testing.T) {
	smtpTestHome(t)

	if _, ok, _ := PanelSMTP(); ok {
		t.Fatal("a fresh install reported a panel account")
	}
	if err := SaveSiteSMTP("acme", SMTPSettings{
		Host: "site.example.com", Port: 587, Encryption: SMTPStartTLS, FromAddress: "a@b.test",
	}); err != nil {
		t.Fatal(err)
	}
	panel := SMTPSettings{
		Host: "smtp.panel.example", Port: 465, Username: "ops", Password: "hunter2",
		Encryption: SMTPImplicitTLS, FromAddress: "alerts@panel.example", FromName: "Servlo",
	}
	if err := SavePanelSMTP(panel); err != nil {
		t.Fatalf("SavePanelSMTP: %v", err)
	}

	got, ok, err := PanelSMTP()
	if err != nil || !ok || got != panel {
		t.Errorf("panel account = %+v ok=%v err=%v", got.Redacted(), ok, err)
	}
	if site, ok, _ := SiteSMTP("acme"); !ok || site.Host != "site.example.com" {
		t.Errorf("saving the panel account disturbed a site's: %+v", site.Redacted())
	}
}

func TestSavePanelSMTP_blankPasswordKeepsTheStoredOne(t *testing.T) {
	smtpTestHome(t)

	first := SMTPSettings{Host: "h", Port: 587, Username: "u", Password: "hunter2", Encryption: SMTPStartTLS, FromAddress: "a@b.test"}
	if err := SavePanelSMTP(first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.Password = ""
	if err := SavePanelSMTP(second); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := PanelSMTP(); got.Password != "hunter2" {
		t.Errorf("password = %q, want the stored one kept", got.Password)
	}
}

func TestDeletePanelSMTP_forgetsTheCredential(t *testing.T) {
	smtpTestHome(t)

	if err := SavePanelSMTP(SMTPSettings{
		Host: "h", Port: 587, Password: "hunter2", Encryption: SMTPStartTLS, FromAddress: "a@b.test",
	}); err != nil {
		t.Fatal(err)
	}
	if err := DeletePanelSMTP(); err != nil {
		t.Fatalf("DeletePanelSMTP: %v", err)
	}
	if _, ok, _ := PanelSMTP(); ok {
		t.Error("the panel account survived its deletion")
	}
	data, err := os.ReadFile(SMTPFile())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "hunter2") {
		t.Error("the password is still on disk after the account was deleted")
	}
}
