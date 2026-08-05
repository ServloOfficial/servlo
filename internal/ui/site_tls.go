package ui

import (
	"net/http"

	"github.com/realrashid/servlo/internal/certs"
	"github.com/realrashid/servlo/internal/config"
)

// TLSStatus is what the panel needs to decide whether Get SSL can be clicked,
// and to say why not when it cannot.
type TLSStatus struct {
	// Ready is whether every domain resolves to this server, which is the
	// precondition for an HTTP-01 challenge succeeding.
	Ready bool `json:"ready"`
	// Message is the mismatch in the operator's terms, empty when ready. It
	// names the addresses on both sides, because "DNS not ready" sends someone
	// to their registrar with nothing to compare against.
	Message string `json:"message"`
	// Server is this server's public addresses, so the panel can show what the
	// records should point at without the operator parsing the message.
	Server []string `json:"server"`
	// Domains is the per-domain detail, so the panel can mark which alias is
	// the one holding the certificate up.
	Domains []domainDNS `json:"domains"`
	// Issuer names the authority in force, so an operator on staging is never
	// surprised by an untrusted certificate.
	Issuer string `json:"issuer"`
	// Progress is the most recent issuance attempt's steps. The panel polls
	// this while its own POST is in flight, which is the only way to show what
	// is happening inside a request it is already waiting on.
	Progress []string `json:"progress,omitempty"`
}

type domainDNS struct {
	Domain    string   `json:"domain"`
	Resolved  []string `json:"resolved"`
	Elsewhere []string `json:"elsewhere,omitempty"`
	Matched   bool     `json:"matched"`
	Error     string   `json:"error,omitempty"`
}

// tlsRoute serves GET /api/sites/{domain}/tls.
func tlsRoute(w http.ResponseWriter, r *http.Request, domain string, rest []string) bool {
	if len(rest) != 1 || rest[0] != "tls" {
		return false
	}
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return true
	}
	// A read, but one that performs live DNS lookups on names the caller chose,
	// so it sits behind the same authority as the actions it gates.
	if !hasHostActionAuthority(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return true
	}
	site, err := config.FindSiteByDomain(domain)
	if err != nil {
		writeJSON(w, map[string]any{"error": "site not found: " + domain})
		return true
	}

	report := certs.DNSReport(r.Context(), site.Domains)
	status := TLSStatus{
		Ready:    report.Ready(),
		Message:  report.Summary(),
		Server:   report.Server,
		Issuer:   certs.IssuerName(),
		Progress: certs.Progress(site.PrimaryDomain()),
	}
	for _, d := range report.Domains {
		status.Domains = append(status.Domains, domainDNS{
			Domain: d.Domain, Resolved: d.Resolved, Elsewhere: d.Elsewhere,
			Matched: d.Matched, Error: d.Error,
		})
	}
	writeJSON(w, status)
	return true
}
