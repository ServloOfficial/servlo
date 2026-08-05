package certs

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/acme"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/nginx"
)

// The two directories Let's Encrypt publishes. Staging issues from an untrusted
// root, which is exactly what makes it useful: its rate limits are generous, so
// a misconfigured domain can be retried without spending the production quota
// that would then lock the operator out for a week.
const (
	LetsEncryptProduction = "https://acme-v02.api.letsencrypt.org/directory"
	LetsEncryptStaging    = "https://acme-staging-v02.api.letsencrypt.org/directory"
)

// issueTimeout bounds a whole issuance. HTTP-01 usually settles in seconds, but
// an authorization that never leaves pending would otherwise hang the CLI or a
// panel request forever, and a hung request is worse than a clear failure.
const issueTimeout = 3 * time.Minute

// ACMEConfig is what servlo needs to talk to an ACME authority.
type ACMEConfig struct {
	// DirectoryURL is the ACME directory. Empty means Let's Encrypt production.
	DirectoryURL string
	// Email receives the authority's expiry notices. Optional: Let's Encrypt
	// registers an account without one, it just cannot warn anybody.
	Email string
}

type acmeIssuer struct {
	cfg ACMEConfig
}

// NewACMEIssuer returns an Issuer that obtains certificates over HTTP-01.
func NewACMEIssuer(cfg ACMEConfig) Issuer {
	if cfg.DirectoryURL == "" {
		cfg.DirectoryURL = LetsEncryptProduction
	}
	return &acmeIssuer{cfg: cfg}
}

func (a *acmeIssuer) Name() string {
	switch a.cfg.DirectoryURL {
	case LetsEncryptProduction:
		return "letsencrypt"
	case LetsEncryptStaging:
		return "letsencrypt-staging"
	}
	if host, err := directoryHost(a.cfg.DirectoryURL); err == nil {
		return "acme:" + host
	}
	return "acme"
}

// directoryHost reduces a directory URL to a filesystem-safe name, used to keep
// one account per authority.
func directoryHost(directoryURL string) (string, error) {
	u, err := url.Parse(directoryURL)
	if err != nil {
		return "", fmt.Errorf("parsing the ACME directory URL %q: %w", directoryURL, err)
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("ACME directory URL %q has no host", directoryURL)
	}
	if port := u.Port(); port != "" {
		host += "_" + port
	}
	return host, nil
}

// Issue runs one HTTP-01 order to completion and writes the chain and key to
// the paths the caller will rename into place.
func (a *acmeIssuer) Issue(primary string, domains []string, certPath, keyPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), issueTimeout)
	defer cancel()

	client, err := a.client()
	if err != nil {
		return fmt.Errorf("preparing the ACME account for %s: %w", primary, err)
	}
	if err := a.register(ctx, client); err != nil {
		return fmt.Errorf("registering with %s for %s: %w", a.Name(), primary, err)
	}

	order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(domains...))
	if err != nil {
		return fmt.Errorf("ordering a certificate for %s: %w", primary, err)
	}
	if err := a.satisfy(ctx, client, order); err != nil {
		return err
	}
	if _, err := client.WaitOrder(ctx, order.URI); err != nil {
		return fmt.Errorf("waiting for the order for %s: %w", primary, err)
	}
	return a.finalize(ctx, client, order, primary, domains, certPath, keyPath)
}

// client builds an ACME client on the account key, creating the key on first
// use. The key is the account: losing it means the next issuance registers a
// fresh account and starts over on rate limits, so it is written once and read
// thereafter.
func (a *acmeIssuer) client() (*acme.Client, error) {
	host, err := directoryHost(a.cfg.DirectoryURL)
	if err != nil {
		return nil, err
	}
	dir := config.ACMEAccountDir(host)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	key, err := loadOrCreateAccountKey(filepath.Join(dir, "account.key"))
	if err != nil {
		return nil, err
	}
	return &acme.Client{Key: key, DirectoryURL: a.cfg.DirectoryURL}, nil
}

func loadOrCreateAccountKey(path string) (*ecdsa.PrivateKey, error) {
	if data, err := os.ReadFile(path); err == nil {
		block, _ := pem.Decode(data)
		if block == nil {
			return nil, fmt.Errorf("%s is not a PEM key; move it aside to register a new account", path)
		}
		return x509.ParseECPrivateKey(block.Bytes)
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	if err := writeECKey(path, key); err != nil {
		return nil, err
	}
	return key, nil
}

// writeECKey writes a PEM private key readable only by its owner. The file is
// created 0600 rather than chmodded afterwards, so it is never briefly
// world-readable between the two calls.
func writeECKey(path string, key *ecdsa.PrivateKey) error {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if err := pem.Encode(f, &pem.Block{Type: "EC PRIVATE KEY", Bytes: der}); err != nil {
		f.Close() //nolint:errcheck
		return err
	}
	return f.Close()
}

// register creates the account, or accepts that it already exists. Registering
// an existing key is the ordinary case on every issuance after the first.
func (a *acmeIssuer) register(ctx context.Context, client *acme.Client) error {
	acct := &acme.Account{}
	if a.cfg.Email != "" {
		acct.Contact = []string{"mailto:" + a.cfg.Email}
	}
	_, err := client.Register(ctx, acct, acme.AcceptTOS)
	if err == nil || errors.Is(err, acme.ErrAccountAlreadyExists) {
		return nil
	}
	return err
}

// satisfy answers the HTTP-01 challenge for every authorization the order still
// needs. Tokens are removed on the way out whatever happens: a token left in the
// webroot is a public file that outlives the attempt that created it.
func (a *acmeIssuer) satisfy(ctx context.Context, client *acme.Client, order *acme.Order) error {
	if err := nginx.EnsureChallengeDir(); err != nil {
		return fmt.Errorf("preparing the challenge webroot: %w", err)
	}
	for _, authzURL := range order.AuthzURLs {
		authz, err := client.GetAuthorization(ctx, authzURL)
		if err != nil {
			return fmt.Errorf("reading an authorization: %w", err)
		}
		if authz.Status == acme.StatusValid {
			continue
		}
		if err := a.satisfyOne(ctx, client, authz); err != nil {
			return err
		}
	}
	return nil
}

func (a *acmeIssuer) satisfyOne(ctx context.Context, client *acme.Client, authz *acme.Authorization) error {
	name := authz.Identifier.Value
	var chal *acme.Challenge
	for _, c := range authz.Challenges {
		if c.Type == "http-01" {
			chal = c
			break
		}
	}
	if chal == nil {
		return fmt.Errorf("%s: the authority offered no http-01 challenge, which is the only kind servlo can answer today", name)
	}

	keyAuth, err := client.HTTP01ChallengeResponse(chal.Token)
	if err != nil {
		return fmt.Errorf("%s: building the challenge response: %w", name, err)
	}
	if err := nginx.WriteChallengeToken(chal.Token, keyAuth); err != nil {
		return fmt.Errorf("%s: publishing the challenge: %w", name, err)
	}
	defer nginx.RemoveChallengeToken(chal.Token)

	if _, err := client.Accept(ctx, chal); err != nil {
		return fmt.Errorf("%s: %w", name, validationHint(err))
	}
	if _, err := client.WaitAuthorization(ctx, authz.URI); err != nil {
		return fmt.Errorf("%s: %w", name, validationHint(err))
	}
	return nil
}

// validationHint turns an authorization failure into something an operator can
// act on. The overwhelmingly common cause is that the domain does not resolve
// to this server, or resolves to something else that answered port 80 first,
// and the raw ACME problem document does not say so.
func validationHint(err error) error {
	var problem *acme.Error
	if errors.As(err, &problem) {
		return fmt.Errorf("the authority could not validate this domain (%s). "+
			"Check that it resolves to this server's public address and that nothing else is answering port 80 for it: %w",
			problem.ProblemType, err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("the authority did not finish validating within %s; it usually could not reach this server on port 80: %w", issueTimeout, err)
	}
	return err
}

// finalize generates the leaf key, sends the CSR and writes what comes back.
// The key is new on every issuance rather than reused: a renewal that keeps the
// old key gains nothing and means one compromise covers every certificate the
// site has ever had.
func (a *acmeIssuer) finalize(ctx context.Context, client *acme.Client, order *acme.Order, primary string, domains []string, certPath, keyPath string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generating a key for %s: %w", primary, err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: primary},
		DNSNames: domains,
	}, key)
	if err != nil {
		return fmt.Errorf("building the request for %s: %w", primary, err)
	}

	chain, _, err := client.CreateOrderCert(ctx, order.FinalizeURL, csrDER, true)
	if err != nil {
		return fmt.Errorf("finalizing the certificate for %s: %w", primary, err)
	}
	if len(chain) == 0 {
		return fmt.Errorf("the authority returned no certificate for %s", primary)
	}

	// Leaf first, then the issuers, all in one file: that is what nginx's
	// ssl_certificate wants, and a client missing the intermediate fails
	// without it even though the leaf itself is fine.
	var buf strings.Builder
	for _, der := range chain {
		if err := pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
			return err
		}
	}
	if err := os.WriteFile(certPath, []byte(buf.String()), 0644); err != nil {
		return err
	}
	if err := writeECKey(keyPath, key); err != nil {
		os.Remove(certPath) //nolint:errcheck — never leave a cert without its key
		return err
	}
	return nil
}
