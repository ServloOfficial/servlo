package appstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ServloOfficial/servlo/internal/nginx"
	"github.com/ServloOfficial/servlo/internal/sitehttp"
)

// Driving an application's own installer.
//
// Every application worth installing already has a setup flow that creates the
// first account, and reimplementing it means writing that application's user
// table from Go, which is both app-specific and wrong the first time the schema
// changes. So the definition describes the form instead: where it lives on the
// site and what to put in it.
//
// Two things this refuses. The path is site-relative and may not name a host,
// because the request carries an admin password generated seconds earlier and a
// definition that could redirect it would be posting that password wherever it
// liked. And a response has to say it worked: without something to check, any
// answer at all reads as success, and a setup that silently did not happen
// leaves an uninstalled application on a live domain for the first passer-by to
// claim.

// ErrNotTheSite says the setup request reached servlo rather than the site.
//
// Servlo answers a domain no site is linked to with a branded page, which is
// also what a domain whose vhost nginx has not reloaded yet gets. A POST to
// that server lands on a static file and nginx refuses it with an error page of
// its own, and nothing in that response says whose it is except the header the
// catch-all sets. Without this the install read it as the application declining
// and stopped there, having sent a generated admin password to servlo's own
// placeholder and left the site sitting on its setup form.
var ErrNotTheSite = errors.New("the request reached servlo's catch-all rather than the site, so nginx had not picked up the site's own vhost")

// Setup is the application's own install form.
type Setup struct {
	// Path is where the form posts, relative to the site's own URL. It may
	// carry a query string.
	Path string `yaml:"path"`
	// Fields are the form values, with placeholders substituted the same way
	// the config template's are.
	Fields map[string]string `yaml:"fields"`
	// SuccessContains is what the response must carry for the setup to count.
	SuccessContains string `yaml:"success_contains"`
}

// Declared reports whether the definition has a setup step at all.
func (s Setup) Declared() bool { return s.Path != "" || len(s.Fields) > 0 }

const setupTimeout = 2 * time.Minute

func (s Setup) validate(app string) error {
	if !s.Declared() {
		return nil
	}
	// One check, not two. A single leading slash is what makes the path
	// relative: "https://host/x" fails it, and so does "//host/x", which is a
	// scheme-relative URL and a host in disguise. Anything that survives it is
	// appended to the site's own URL, and Go's parser keeps the host as the
	// site's whatever the path spells, backslashes and encoded slashes
	// included. A second guard against a host looked prudent and was
	// unreachable, which a mutation showed by surviving.
	if !strings.HasPrefix(s.Path, "/") || strings.HasPrefix(s.Path, "//") {
		return fmt.Errorf("app %q setup path %q must start with a single / and be relative to the site: servlo posts a generated admin password to it", app, s.Path)
	}
	if strings.TrimSpace(s.SuccessContains) == "" {
		return fmt.Errorf("app %q setup declares nothing to check for: without it any response reads as success, and a setup that did not happen leaves the site claimable", app)
	}
	if len(s.Fields) == 0 {
		return fmt.Errorf("app %q setup posts no fields", app)
	}
	return nil
}

// Run posts the setup form to siteURL and checks the answer.
func (s Setup) Run(ctx context.Context, siteURL string, values map[string]string) error {
	ctx, cancel := context.WithTimeout(ctx, setupTimeout)
	defer cancel()

	form := url.Values{}
	var missing []string
	for field, template := range s.Fields {
		rendered := placeholder.ReplaceAllStringFunc(template, func(match string) string {
			key := placeholder.FindStringSubmatch(match)[1]
			v, ok := values[key]
			if !ok {
				missing = append(missing, key)
				return match
			}
			return v
		})
		form.Set(field, rendered)
	}
	if len(missing) > 0 {
		return fmt.Errorf("the setup form names %s, which servlo has no value for", strings.Join(missing, ", "))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimSuffix(siteURL, "/")+s.Path, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("preparing the setup request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Dialled at this server's own nginx rather than at whatever the domain
	// resolves to. The request carries a generated admin password, and until the
	// operator repoints DNS the public address for the domain is not this
	// machine.
	resp, err := sitehttp.Client(setupTimeout).Do(req)
	if err != nil {
		return fmt.Errorf("running the setup: %w", err)
	}
	defer resp.Body.Close()

	// Before the body, because the body of this one is nginx's error page and
	// reading it as the application's answer is the mistake this closes.
	if resp.Header.Get(nginx.CatchAllHeader) == nginx.CatchAllHeaderValue {
		return ErrNotTheSite
	}

	// Bounded: this is an install page, and an application that answers with a
	// hundred megabytes is one servlo should stop reading rather than hold.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("reading the setup response: %w", err)
	}
	if !strings.Contains(string(body), s.SuccessContains) {
		// Redacted, because the request carried a generated admin password and
		// an application echoing its own form back is not hypothetical. This
		// error reaches the panel and the audit log.
		return fmt.Errorf("the setup did not report success: %s", redact(summarise(string(body)), values))
	}
	return nil
}

// summarise trims a response to something readable in a modal.
func summarise(body string) string {
	body = strings.Join(strings.Fields(body), " ")
	if len(body) > 300 {
		return body[:300] + "…"
	}
	return body
}

// redact removes every value servlo put into the request from anything servlo
// reports about it.
func redact(s string, values map[string]string) string {
	for _, v := range values {
		// Short values are not secrets and blanking them would eat ordinary
		// words out of the message.
		if len(v) < 8 {
			continue
		}
		s = strings.ReplaceAll(s, v, "[redacted]")
	}
	return s
}
