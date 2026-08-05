package siteops

import (
	"strings"
	"testing"
)

// A site's handle is internal: it names units, containers and config keys. It
// stays derived from the directory. What must never happen again is a domain
// falling out of it.
func TestSiteName_DerivesAHandleAndNoDomain(t *testing.T) {
	// The exhaustive table of directory shapes lives in naming_test.go; these
	// are the two cases that matter here, that a handle is never empty and
	// never a domain.
	cases := map[string]string{
		"":                   "site",
		"admin.astrolov.com": "admin-astrolov",
	}
	for in, want := range cases {
		if got := SiteName(in); got != want {
			t.Errorf("SiteName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeDomain_AcceptsRealFQDNs(t *testing.T) {
	cases := map[string]string{
		"example.com":          "example.com",
		"  Example.COM  ":      "example.com",
		"shop.example.com":     "shop.example.com",
		"example.co.uk":        "example.co.uk",
		"xn--80ak6aa92e.com":   "xn--80ak6aa92e.com",
		"a-b.example.com":      "a-b.example.com",
		"example.com.":         "example.com",
		"http://example.com":   "example.com",
		"https://example.com":  "example.com",
		"https://example.com/": "example.com",
	}
	for in, want := range cases {
		got, err := NormalizeDomain(in)
		if err != nil {
			t.Errorf("NormalizeDomain(%q) errored: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("NormalizeDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

// A bare label is the exact mistake the old behaviour hid: "myapp" used to
// silently become "myapp.test". Now it has to be rejected, because guessing a
// suffix is what this story removes.
func TestNormalizeDomain_RejectsAnythingThatIsNotAnFQDN(t *testing.T) {
	cases := []string{
		"",
		"myapp",
		"localhost",
		"example..com",
		".example.com",
		"-example.com",
		"example-.com",
		"exa mple.com",
		"example.com:8080",
		"192.168.0.1",
		strings.Repeat("a", 64) + ".com",
	}
	for _, in := range cases {
		if got, err := NormalizeDomain(in); err == nil {
			t.Errorf("NormalizeDomain(%q) = %q, want an error", in, got)
		}
	}
}

// The refusal has to name the thing it wants, since the operator's next move is
// to supply one.
func TestNormalizeDomain_ErrorNamesWhatIsMissing(t *testing.T) {
	_, err := NormalizeDomain("myapp")
	if err == nil {
		t.Fatal("a bare label was accepted")
	}
	if !strings.Contains(err.Error(), "myapp") || !strings.Contains(err.Error(), "domain") {
		t.Errorf("error %q does not name the input and what is wanted", err)
	}
}

// A directory already named for the site's domain is the common case on a
// server, so it is offered as the default rather than making the operator
// retype it. Anything else has no default at all.
func TestDomainFromDirName(t *testing.T) {
	cases := map[string]string{
		"example.com":      "example.com",
		"shop.example.com": "shop.example.com",
		"myapp":            "",
		"":                 "",
		"example.com.":     "example.com",
	}
	for in, want := range cases {
		if got := DomainFromDirName(in); got != want {
			t.Errorf("DomainFromDirName(%q) = %q, want %q", in, got, want)
		}
	}
}
