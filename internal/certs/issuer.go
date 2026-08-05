package certs

import "fmt"

// Issuer produces a certificate and its key for a set of domains.
//
// Everything around it is issuer-agnostic and stays that way: the 30-day
// reissue window, the atomic swap that keeps a complete certificate at the live
// path at every instant, the rollback when a key rename fails, the expiry
// warnings and the nginx reload. Only the step that mints the bytes differs
// between a local CA and an ACME authority, so only that step is behind this
// interface.
//
// Issue writes PEM to certPath and keyPath, which are temporary paths the
// caller renames into place. An issuer that fails must leave them absent or
// incomplete rather than half-written: the caller treats any error as "keep the
// previous certificate", and losing a good certificate is worse than failing to
// renew, since a missing one trips the vhost repair into flipping the site to
// plain HTTP.
//
// primary is domains[0] and names the files; domains is every name the
// certificate must cover. An issuer decides for itself what SANs that implies.
// A local CA can mint wildcards for free, an ACME authority cannot over HTTP-01,
// so the expansion belongs to the implementation and not to this contract.
type Issuer interface {
	Issue(primary string, domains []string, certPath, keyPath string) error
	// Name identifies the issuer in errors and in doctor output.
	Name() string
}

// activeIssuer returns the issuer in force. A func rather than a value so tests
// can swap it, and so the choice can later come from config once there is more
// than one to choose between.
var activeIssuer = func() Issuer { return unavailableIssuer{} }

// unavailableIssuer is what ships until ACME lands. The locally trusted CA
// Servlo inherited went with the local DNS stack it belonged to: a certificate
// only this machine trusts is exactly wrong for a site on a real domain that
// real browsers visit.
//
// It refuses rather than falling back to a self-signed certificate. A
// self-signed certificate on a real domain is indistinguishable from an
// interception to a browser, and shipping one would teach operators to click
// through the warning that is supposed to protect them.
type unavailableIssuer struct{}

func (unavailableIssuer) Name() string { return "none" }

func (unavailableIssuer) Issue(primary string, _ []string, _, _ string) error {
	return fmt.Errorf("cannot issue a certificate for %s: no certificate issuer is configured. "+
		"A locally trusted CA is meaningless for a real domain, so it was removed, "+
		"and ACME issuance arrives in S3.2. Serve the site over http until then", primary)
}

// IssuerName reports which issuer is in force, for doctor and status output.
func IssuerName() string { return activeIssuer().Name() }
