package sitetpl

import (
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

// The SMTP placeholders exist for the same reason the database ones do: which
// keys carry the mail transport is a framework's business, and what goes in
// them is the site's, so no Go here names a framework key.
func TestForSite_ResolvesTheSitesOwnSMTPAccount(t *testing.T) {
	site := registerSite(t, config.Site{Name: "shop", Domains: []string{"shop.example"}})

	if err := config.SaveSiteSMTP("shop", config.SMTPSettings{
		Host: "smtp.postmarkapp.com", Port: 587, Username: "token-user",
		Password: "s3cret", Encryption: config.SMTPStartTLS,
		FromAddress: "hello@shop.example", FromName: "Shop",
	}); err != nil {
		t.Fatal(err)
	}

	got := Apply(
		"MAIL_HOST={{smtp_host}} MAIL_PORT={{smtp_port}} MAIL_USERNAME={{smtp_user}} "+
			"MAIL_PASSWORD={{smtp_password}} MAIL_ENCRYPTION={{smtp_encryption}} "+
			"MAIL_FROM_ADDRESS={{smtp_from_address}} MAIL_FROM_NAME={{smtp_from_name}}",
		ForSite(site))
	want := "MAIL_HOST=smtp.postmarkapp.com MAIL_PORT=587 MAIL_USERNAME=token-user " +
		"MAIL_PASSWORD=s3cret MAIL_ENCRYPTION=tls " +
		"MAIL_FROM_ADDRESS=hello@shop.example MAIL_FROM_NAME=Shop"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// starttls and implicit TLS are both "tls" to a framework: Laravel, Symfony and
// the rest have one encryption key and no way to say which handshake. The
// distinction is servlo's, for its own dialer, and must not leak into a value
// the framework will not understand.
func TestApply_SMTPEncryptionSpeaksTheFrameworksVocabulary(t *testing.T) {
	site := registerSite(t, config.Site{Name: "shop", Domains: []string{"shop.example"}})
	for encryption, want := range map[string]string{
		config.SMTPStartTLS:    "tls",
		config.SMTPImplicitTLS: "tls",
		config.SMTPNone:        "null",
	} {
		if err := config.SaveSiteSMTP("shop", config.SMTPSettings{
			Host: "h", Port: 25, Encryption: encryption, FromAddress: "a@b.test",
		}); err != nil {
			t.Fatal(err)
		}
		if got := Apply("{{smtp_encryption}}", ForSite(site)); got != want {
			t.Errorf("%s rendered as %q, want %q", encryption, got, want)
		}
	}
}

// A site with no account leaves the placeholders alone, the same as every other
// empty value here: an env file with {{smtp_host}} still in it is visible, and
// one wired to a hostname nobody chose is not.
func TestForSite_LeavesSMTPPlaceholdersWhenThereIsNoAccount(t *testing.T) {
	site := registerSite(t, config.Site{Name: "shop", Domains: []string{"shop.example"}})

	got := Apply("MAIL_HOST={{smtp_host}}", ForSite(site))
	if !strings.Contains(got, "{{smtp_host}}") {
		t.Errorf("got %q, want the placeholder left in place", got)
	}
}

// The username is legitimately empty on a relay that authenticates by IP, and
// the sender name is optional. Both have to render as empty rather than being
// left as a literal placeholder, or the env file carries {{smtp_user}} to a
// mail server.
func TestForSite_RendersTheOptionalSMTPFieldsEmpty(t *testing.T) {
	site := registerSite(t, config.Site{Name: "shop", Domains: []string{"shop.example"}})

	if err := config.SaveSiteSMTP("shop", config.SMTPSettings{
		Host: "relay.internal", Port: 25, Encryption: config.SMTPNone, FromAddress: "a@b.test",
	}); err != nil {
		t.Fatal(err)
	}
	got := Apply("u=[{{smtp_user}}] p=[{{smtp_password}}] n=[{{smtp_from_name}}]", ForSite(site))
	if got != "u=[] p=[] n=[]" {
		t.Errorf("got %q, want the optional fields rendered empty", got)
	}
}

// The DSN placeholders are the reason a Symfony or CakePHP definition can build
// a URL at all: a password with an @ in it truncates the host out of an
// unencoded smtp://user:pass@host.
func TestApply_URLEncodesTheSMTPCredentialForDSNs(t *testing.T) {
	site := registerSite(t, config.Site{Name: "shop", Domains: []string{"shop.example"}})

	if err := config.SaveSiteSMTP("shop", config.SMTPSettings{
		Host: "smtp.example.com", Port: 587, Username: "user@acme.test",
		Password: "p@ss:w/rd", Encryption: config.SMTPStartTLS, FromAddress: "a@b.test",
	}); err != nil {
		t.Fatal(err)
	}
	got := Apply("smtp://{{smtp_user_urlencoded}}:{{smtp_password_urlencoded}}@{{smtp_host}}:{{smtp_port}}", ForSite(site))
	want := "smtp://user%40acme.test:p%40ss%3Aw%2Frd@smtp.example.com:587"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// The bare and boolean spellings of the same setting, for the frameworks whose
// keys do not understand Laravel's literal "null".
func TestApply_SMTPCryptoAndTLSSpellings(t *testing.T) {
	site := registerSite(t, config.Site{Name: "shop", Domains: []string{"shop.example"}})

	for encryption, want := range map[string]string{
		config.SMTPStartTLS: "enc=tls crypto=[tls] tls=true",
		config.SMTPNone:     "enc=null crypto=[] tls=false",
	} {
		if err := config.SaveSiteSMTP("shop", config.SMTPSettings{
			Host: "h", Port: 25, Encryption: encryption, FromAddress: "a@b.test",
		}); err != nil {
			t.Fatal(err)
		}
		got := Apply("enc={{smtp_encryption}} crypto=[{{smtp_crypto}}] tls={{smtp_tls}}", ForSite(site))
		if got != want {
			t.Errorf("%s rendered as %q, want %q", encryption, got, want)
		}
	}
}
