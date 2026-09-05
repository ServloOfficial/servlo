package certs

import (
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/dnsprovider"
)

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
// can swap it, and so the choice is read fresh on every issuance: an operator
// who fixes their authority settings should not have to restart the panel for
// the next renewal to use them.
var activeIssuer = func() Issuer { return issuerFromConfig() }

// issuerFromConfig builds the ACME issuer from the global config. An explicit
// directory URL wins over the staging flag, which wins over Let's Encrypt
// production, so a private ACME server is reachable without the staging switch
// silently overriding it.
func issuerFromConfig() Issuer {
	cfg, _ := config.LoadGlobal()
	email, directoryURL, staging := cfg.ACMESettings()
	if directoryURL == "" && staging {
		directoryURL = LetsEncryptStaging
	}
	acmeCfg := ACMEConfig{DirectoryURL: directoryURL, Email: email}
	if challenge, provider := cfg.ACMEChallenge(); Challenge(challenge) == ChallengeDNS01 {
		acmeCfg.Challenge = ChallengeDNS01
		acmeCfg.DNSProvider = dnsprovider.Name(provider)
	}
	return NewACMEIssuer(acmeCfg)
}

// IssuerName reports which issuer is in force, for doctor and status output.
func IssuerName() string { return activeIssuer().Name() }
