package appstore

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const withSetup = `
name: example
label: Example CMS
framework: example-framework
source:
  version: "1.0"
  url: https://example.org/example-1.0.zip
  sha256: 0000000000000000000000000000000000000000000000000000000000000000
setup:
  path: /install.php?step=2
  success_contains: successfully
  fields:
    site_title: "{{site_title}}"
    user_name: "{{admin_user}}"
    admin_password: "{{admin_password}}"
    admin_password2: "{{admin_password}}"
    admin_email: "{{admin_email}}"
    weak_ok: "1"
`

func TestParse_ReadsASetupBlock(t *testing.T) {
	app, err := Parse([]byte(withSetup))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if app.Setup.Path != "/install.php?step=2" {
		t.Errorf("Setup.Path = %q", app.Setup.Path)
	}
	if len(app.Setup.Fields) != 6 {
		t.Errorf("Setup.Fields has %d entries, want 6", len(app.Setup.Fields))
	}
}

// The setup request goes to the site servlo just created and nowhere else. A
// definition naming a host would make the panel post an admin password,
// generated seconds earlier, to a server of the definition's choosing.
func TestParse_RefusesASetupPathThatNamesSomewhereElse(t *testing.T) {
	for _, bad := range []string{
		"https://evil.example/install.php",
		"//evil.example/install.php",
		"http://evil.example/install.php",
		"install.php",
	} {
		src := strings.Replace(withSetup, "path: /install.php?step=2", "path: "+bad, 1)
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("setup path %q was accepted", bad)
		}
	}
}

// Without something to look for, any response at all reads as success, and a
// setup that silently did not happen leaves a site anybody can claim by
// visiting the installer themselves.
func TestParse_RefusesASetupWithNothingToCheck(t *testing.T) {
	src := strings.Replace(withSetup, "  success_contains: successfully\n", "", 1)
	if _, err := Parse([]byte(src)); err == nil {
		t.Fatal("a setup block with no success condition was accepted")
	}
}

func TestRunSetup_PostsTheRenderedFormAndReadsTheAnswer(t *testing.T) {
	app, err := Parse([]byte(withSetup))
	if err != nil {
		t.Fatal(err)
	}

	var gotPath, gotUser, gotPassword string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm() //nolint:errcheck
		gotPath = r.URL.RequestURI()
		gotUser = r.PostForm.Get("user_name")
		gotPassword = r.PostForm.Get("admin_password")
		w.Write([]byte("<p>Installed successfully.</p>")) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)

	err = app.Setup.Run(t.Context(), srv.URL, map[string]string{
		"site_title": "Example", "admin_user": "admin",
		"admin_password": "generated-one", "admin_email": "a@example.com",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotPath != "/install.php?step=2" {
		t.Errorf("posted to %q", gotPath)
	}
	if gotUser != "admin" || gotPassword != "generated-one" {
		t.Errorf("form carried user %q password %q", gotUser, gotPassword)
	}
}

// A response that does not say it worked is a setup that did not happen, and
// reporting it as done leaves an uninstalled application on a live domain for
// the first passer-by to claim.
func TestRunSetup_RefusesAResponseThatDoesNotSaySoWorked(t *testing.T) {
	app, _ := Parse([]byte(withSetup))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("<p>Already installed. Please delete the tables first.</p>")) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)

	err := app.Setup.Run(t.Context(), srv.URL, map[string]string{
		"site_title": "x", "admin_user": "x", "admin_password": "x", "admin_email": "x",
	})
	if err == nil {
		t.Fatal("a response with no success marker was read as success")
	}
}

// The generated admin password goes in this request. It must not come back out
// in an error the panel shows and the audit log keeps.
func TestRunSetup_KeepsThePasswordOutOfItsErrors(t *testing.T) {
	app, _ := Parse([]byte(withSetup))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm() //nolint:errcheck
		// An application that echoes the form back is not hypothetical.
		w.Write([]byte("failed for " + r.PostForm.Get("admin_password"))) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)

	const secret = "s3cret-generated-password"
	err := app.Setup.Run(t.Context(), srv.URL, map[string]string{
		"site_title": "x", "admin_user": "x", "admin_password": secret, "admin_email": "x",
	})
	if err == nil {
		t.Fatal("expected a failure")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("the error carries the admin password: %v", err)
	}
}

func TestRunSetup_RefusesAPlaceholderItCannotFill(t *testing.T) {
	src := strings.Replace(withSetup, "{{site_title}}", "{{nothing_servlo_has}}", 1)
	app, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}

	err = app.Setup.Run(t.Context(), "http://127.0.0.1:1", map[string]string{"admin_user": "x"})
	if err == nil || !strings.Contains(err.Error(), "nothing_servlo_has") {
		t.Errorf("error = %v, want it to name the placeholder", err)
	}
}

// The setup POST carries a generated admin password, and the domain it is for
// does not resolve to this server until the operator repoints its DNS, which
// servlo's own order of operations puts after the site exists. Sent to the
// public address, the password goes to whoever answers for the domain today.
//
// The domain here is under .invalid, which resolves nowhere by definition. A
// request that arrives at all is one that was dialled at this server.
func TestRunSetup_TalksToThisServerRatherThanTheDomain(t *testing.T) {
	app, err := Parse([]byte(withSetup))
	if err != nil {
		t.Fatal(err)
	}

	var gotHost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		w.Write([]byte("<p>Installed successfully.</p>")) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)

	_, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}

	err = app.Setup.Run(t.Context(), "http://not-pointed-here.invalid:"+port, map[string]string{
		"site_title": "Example", "admin_user": "admin",
		"admin_password": "generated-one", "admin_email": "a@example.com",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The domain still has to reach nginx's server_name, and without the port:
	// an application that records the host it was installed through would
	// otherwise hand every visitor a link to servlo's internal port.
	if gotHost != "not-pointed-here.invalid" {
		t.Errorf("the site saw Host %q, want the bare domain", gotHost)
	}
}
