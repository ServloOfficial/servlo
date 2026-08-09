package uptime

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

// serve stands a handler up where nginx would be, so a check runs through the
// real client: the real redirect policy, the real timeouts, the real status
// handling. Only the address dialled is replaced, which is the same
// substitution production makes for loopback.
func serve(t *testing.T, h http.HandlerFunc) *[]*http.Request {
	t.Helper()
	var seen []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Clone(r.Context()))
		h(w, r)
	}))
	t.Cleanup(srv.Close)

	at := strings.TrimPrefix(srv.URL, "http://")
	prev := dialTarget
	t.Cleanup(func() { dialTarget = prev })
	dialTarget = func(string) string { return at }
	return &seen
}

func site(name string) *config.Site {
	return &config.Site{Name: name, Domains: []string{name + ".example"}}
}

// The path comes from the framework's definition, not from servlo. A framework
// that ships a real health endpoint is checked against it.
func TestCheck_AsksTheFrameworksOwnHealthPath(t *testing.T) {
	seen := serve(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })

	fw := &config.Framework{Deploy: &config.FrameworkDeploy{Health: "/up"}}
	res := Check(site("acme"), fw)

	if !res.Up {
		t.Fatalf("a site answering 200 was reported down: %+v", res)
	}
	if len(*seen) != 1 || (*seen)[0].URL.Path != "/up" {
		t.Errorf("asked for %v, want /up", *seen)
	}
	// The request has to carry the site's own hostname or it lands on whichever
	// vhost nginx serves by default rather than on the site being checked.
	if host, _, _ := strings.Cut((*seen)[0].Host, ":"); host != "acme.example" {
		t.Errorf("the request went out as %q, so it would not match the site's vhost", (*seen)[0].Host)
	}
}

// A framework that names no health path is still checked, against its home
// page. Skipping it would leave those sites unmonitored for no reason.
func TestCheck_FallsBackToTheHomePage(t *testing.T) {
	seen := serve(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })

	if res := Check(site("acme"), nil); !res.Up {
		t.Fatalf("reported down: %+v", res)
	}
	if (*seen)[0].URL.Path != "/" {
		t.Errorf("asked for %q, want /", (*seen)[0].URL.Path)
	}
}

// A check that looked like a browser would be logged as traffic and counted in
// the site's analytics.
func TestCheck_NamesItself(t *testing.T) {
	seen := serve(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	Check(site("acme"), nil)

	if ua := (*seen)[0].UserAgent(); !strings.Contains(ua, "servlo") {
		t.Errorf("the check went out as %q, which a site cannot tell from a visitor", ua)
	}
}

// 502 and 503 are the shape of a dead PHP-FPM pool behind a live nginx, which
// is the commonest way a site goes down without the server going anywhere.
func TestCheck_ServerErrorIsDown(t *testing.T) {
	serve(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(502) })

	res := Check(site("acme"), nil)
	if res.Up {
		t.Error("a site answering 502 was reported up")
	}
	if res.Status != 502 || !strings.Contains(res.Reason, "502") {
		t.Errorf("the reason does not say what happened: %+v", res)
	}
}

// A 404 on a health path is a wrong path, not a down site. Reporting it as an
// outage would have an operator chasing the wrong thing at three in the morning.
func TestCheck_ClientErrorIsStillServing(t *testing.T) {
	serve(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(404) })

	if res := Check(site("acme"), nil); !res.Up {
		t.Errorf("a site answering 404 was reported down: %+v", res)
	}
}

// A redirect is a site that is serving. Following it would turn one check into
// several and let a redirect loop hang the monitor.
func TestCheck_RedirectIsUpAndNotFollowed(t *testing.T) {
	seen := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere", http.StatusMovedPermanently)
	})

	res := Check(site("acme"), nil)
	if !res.Up {
		t.Errorf("a site answering 301 was reported down: %+v", res)
	}
	if len(*seen) != 1 {
		t.Errorf("the checker followed the redirect: %d requests", len(*seen))
	}
}

// Nothing listening is the plain case, and the reason has to read as something
// other than a raw Go dial error.
func TestCheck_NothingAnsweringIsDown(t *testing.T) {
	// A port that was listening a moment ago and is not now: closing the
	// listener leaves an address nothing will answer on.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	at := ln.Addr().String()
	_ = ln.Close()

	prev := dialTarget
	t.Cleanup(func() { dialTarget = prev })
	dialTarget = func(string) string { return at }

	res := Check(site("acme"), nil)
	if res.Up {
		t.Error("a site nothing answered for was reported up")
	}
	if res.Status != 0 {
		t.Errorf("a site that never answered has status %d", res.Status)
	}
	if !strings.Contains(res.Reason, "could not be reached") {
		t.Errorf("reason %q does not say the site was unreachable", res.Reason)
	}
}

// A timeout reads differently from a refusal: one is a site that is gone, the
// other a site that is drowning, and they are fixed differently.
func TestDownReason_SeparatesATimeoutFromARefusal(t *testing.T) {
	timeout := downReason(&net.OpError{Op: "dial", Err: timeoutErr{}})
	if !strings.Contains(timeout, "did not answer within") {
		t.Errorf("a timeout reads as %q", timeout)
	}
	refused := downReason(&net.OpError{Op: "dial", Err: errors.New("connection refused")})
	if !strings.Contains(refused, "could not be reached") {
		t.Errorf("a refusal reads as %q", refused)
	}
	if timeout == refused {
		t.Error("a timeout and a refusal read the same, so neither says what to do about it")
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// A site with no domain cannot be asked anything, and saying so is better than
// building a URL with an empty host and reporting the site down.
func TestCheck_NoDomainSaysSoRatherThanReportingAnOutage(t *testing.T) {
	res := Check(&config.Site{Name: "acme"}, nil)
	if res.Up {
		t.Error("a site with no domain was reported up")
	}
	if !strings.Contains(res.Reason, "no domain") {
		t.Errorf("reason %q does not explain that there is no domain", res.Reason)
	}
}

// A secured site is checked over HTTPS, so the check exercises the certificate
// a visitor gets rather than a plaintext port the vhost only redirects from.
func TestURL_SecuredSiteIsCheckedOverHTTPS(t *testing.T) {
	s := site("acme")
	s.Secured = true
	if got := URL(s, nil); got != "https://acme.example:443/" {
		t.Errorf("checking %q, want https on the HTTPS port", got)
	}
	s.Secured = false
	if got := URL(s, nil); got != "http://acme.example:80/" {
		t.Errorf("checking %q, want plain http", got)
	}
}

// A server that took the nftables fallback has nginx on 8080 and 8443, and a
// check aimed at 443 there would report every site down.
func TestPorts_FollowTheServersActualListenPorts(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	cfg.SetPortStrategy("nftables", 8080, 8443)
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatal(err)
	}

	if got := Ports(); got.HTTP != 8080 || got.HTTPS != 8443 {
		t.Errorf("checking %+v, want the ports nginx is actually on", got)
	}
	if got := URL(site("acme"), nil); !strings.Contains(got, ":8080") {
		t.Errorf("checking %q, want the port nginx is actually on", got)
	}
}
