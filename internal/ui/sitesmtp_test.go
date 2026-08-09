package ui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/stores"
)

// smtpSite registers one Laravel site, whose store definition declares the mail
// keys this card writes.
func smtpSite(t *testing.T) *config.Site {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	// The shipped definition, not a fixture: this card is only as good as what
	// the store actually says about Laravel's mail keys.
	definition, ok := stores.Read(stores.Frameworks, "laravel/12.yaml")
	if !ok {
		t.Fatal("the embedded store has no laravel/12.yaml")
	}
	if err := os.MkdirAll(config.StoreFrameworksDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config.StoreFrameworksDir(), "laravel.yaml"), definition, 0o644); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "acme")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, ".env"), []byte("APP_NAME=Acme\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	site := config.Site{Name: "acme", Domains: []string{"acme.example"}, Path: path, Framework: "laravel"}
	if err := config.SaveSites(&config.SiteRegistry{Sites: []config.Site{site}}); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadSites()
	if err != nil {
		t.Fatal(err)
	}
	return &loaded.Sites[0]
}

func smtpCall(t *testing.T, method, domain string, rest []string, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	url := "/api/sites/" + domain + "/" + strings.Join(rest, "/")
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, url, nil)
	} else {
		req = httptest.NewRequest(method, url, strings.NewReader(body))
	}
	rec := httptest.NewRecorder()
	if !smtpRoute(rec, req, domain, rest) {
		t.Fatalf("the route did not claim %s %s", method, url)
	}
	out := map[string]any{}
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decoding %q: %v", rec.Body.String(), err)
		}
	}
	return rec, out
}

// A site with no account renders the empty form, and says which keys a save
// would write so the operator knows what is about to change.
func TestSMTPStatus_reportsAnUnconfiguredSite(t *testing.T) {
	site := smtpSite(t)

	_, body := smtpCall(t, http.MethodGet, site.Domains[0], []string{"smtp"}, "")
	if body["configured"] != false {
		t.Errorf("configured = %v, want false", body["configured"])
	}
	keys, _ := body["env_keys"].([]any)
	if len(keys) == 0 {
		t.Error("the card does not say which env keys a save writes")
	}
}

func TestSMTPSave_storesAndWritesTheEnvFile(t *testing.T) {
	site := smtpSite(t)

	rec, body := smtpCall(t, http.MethodPost, site.Domains[0], []string{"smtp"},
		`{"host":"smtp.postmarkapp.com","port":587,"username":"token","password":"s3cret",
		  "encryption":"starttls","from_address":"hello@acme.example","from_name":"Acme"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if body["ok"] != true {
		t.Fatalf("body = %v", body)
	}
	env, err := os.ReadFile(filepath.Join(site.Path, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env), "MAIL_HOST=smtp.postmarkapp.com") {
		t.Errorf(".env was not wired:\n%s", env)
	}
	stored, ok, _ := config.SiteSMTP("acme")
	if !ok || stored.Password != "s3cret" {
		t.Errorf("the account was not stored: %+v ok=%v", stored.Redacted(), ok)
	}
}

// The password never comes back out. A GET that returns it is a credential in
// the browser's memory, in a proxy log, and in whatever the operator screenshots.
func TestSMTPStatus_neverReturnsThePassword(t *testing.T) {
	site := smtpSite(t)
	smtpCall(t, http.MethodPost, site.Domains[0], []string{"smtp"},
		`{"host":"smtp.example.com","port":587,"username":"u","password":"hunter2",
		  "encryption":"starttls","from_address":"hello@acme.example"}`)

	rec, body := smtpCall(t, http.MethodGet, site.Domains[0], []string{"smtp"}, "")
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatalf("the response carries the password: %s", rec.Body.String())
	}
	settings, _ := body["settings"].(map[string]any)
	if settings["has_password"] != true {
		t.Error("the form cannot tell a password is stored")
	}
	if settings["username"] != "u" {
		t.Errorf("username = %v, want the non-secret field kept", settings["username"])
	}
}

func TestSMTPSave_refusesSettingsThatWouldNotSend(t *testing.T) {
	site := smtpSite(t)

	rec, body := smtpCall(t, http.MethodPost, site.Domains[0], []string{"smtp"},
		`{"host":"smtp.example.com","port":0,"encryption":"starttls","from_address":"hello@acme.example"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if body["error"] == nil {
		t.Error("a refused save said nothing about why")
	}
	if _, ok, _ := config.SiteSMTP("acme"); ok {
		t.Error("the refused settings were stored anyway")
	}
}

func TestSMTPDelete_forgetsTheAccount(t *testing.T) {
	site := smtpSite(t)
	smtpCall(t, http.MethodPost, site.Domains[0], []string{"smtp"},
		`{"host":"smtp.example.com","port":587,"password":"hunter2",
		  "encryption":"starttls","from_address":"hello@acme.example"}`)

	rec, _ := smtpCall(t, http.MethodDelete, site.Domains[0], []string{"smtp"}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if _, ok, _ := config.SiteSMTP("acme"); ok {
		t.Error("the account survived its deletion")
	}
}

// The test button's whole value is the server's own words coming back.
func TestSMTPTest_reportsTheServersRefusal(t *testing.T) {
	site := smtpSite(t)
	host, port := refusingSMTP(t)
	smtpCall(t, http.MethodPost, site.Domains[0], []string{"smtp"}, fmt.Sprintf(
		`{"host":%q,"port":%d,"encryption":"none","from_address":"hello@acme.example"}`, host, port))

	rec, body := smtpCall(t, http.MethodPost, site.Domains[0], []string{"smtp", "test"},
		`{"to":"ops@acme.example"}`)
	if rec.Code == http.StatusOK {
		t.Fatalf("a refused send reported success: %s", rec.Body.String())
	}
	msg, _ := body["error"].(string)
	if !strings.Contains(msg, "Sender address not verified") {
		t.Errorf("error %q does not carry the server's own reply", msg)
	}
}

func TestSMTPTest_refusesARecipientThatIsNotAnAddress(t *testing.T) {
	site := smtpSite(t)
	smtpCall(t, http.MethodPost, site.Domains[0], []string{"smtp"},
		`{"host":"smtp.example.com","port":587,"encryption":"starttls","from_address":"hello@acme.example"}`)

	rec, _ := smtpCall(t, http.MethodPost, site.Domains[0], []string{"smtp", "test"},
		`{"to":"nobody"}`)
	if rec.Code == http.StatusOK {
		t.Fatal("a recipient that is not an address was accepted")
	}
}

// refusingSMTP answers the greeting and then refuses the sender, the way a
// provider does when the from address is not one it has verified.
func refusingSMTP(t *testing.T) (string, int) {
	t.Helper()
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
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		r := bufio.NewReader(conn)
		fmt.Fprint(conn, "220 fake ESMTP\r\n")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
				fmt.Fprint(conn, "250 fake\r\n")
			case strings.HasPrefix(line, "MAIL FROM"):
				fmt.Fprint(conn, "550 5.7.1 Sender address not verified\r\n")
			case strings.HasPrefix(line, "QUIT"):
				fmt.Fprint(conn, "221 bye\r\n")
				return
			default:
				fmt.Fprint(conn, "250 ok\r\n")
			}
		}
	}()
	return "127.0.0.1", ln.Addr().(*net.TCPAddr).Port
}

// An empty recipient sends to the account's own sender address, so the press an
// operator makes right after filling the form in is not a dead end.
func TestSMTPTest_defaultsToTheSenderAddress(t *testing.T) {
	site := smtpSite(t)
	host, port := refusingSMTP(t)
	smtpCall(t, http.MethodPost, site.Domains[0], []string{"smtp"}, fmt.Sprintf(
		`{"host":%q,"port":%d,"encryption":"none","from_address":"hello@acme.example"}`, host, port))

	rec, body := smtpCall(t, http.MethodPost, site.Domains[0], []string{"smtp", "test"}, `{}`)
	if rec.Code == http.StatusOK {
		t.Fatalf("the refusing server reported success: %s", rec.Body.String())
	}
	// The refusal proves the exchange got as far as naming the sender, which is
	// only possible if the empty recipient resolved to something.
	msg, _ := body["error"].(string)
	if !strings.Contains(msg, "hello@acme.example") {
		t.Errorf("error %q does not name the sender the test fell back to", msg)
	}
}
