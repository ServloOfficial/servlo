// Package dnsprovider publishes the TXT records an ACME DNS-01 challenge needs,
// and holds the credentials that let it.
//
// Those credentials deserve more care than anything else servlo stores. A DNS
// API token can create and delete any record in the zone, which for most
// operators is the whole domain: mail, subdomains, and the ability to obtain a
// certificate for any of them. It is a bigger secret than a database password,
// so it lives owner-only in the config directory and never near a site tree.
package dnsprovider

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/ServloOfficial/servlo/internal/config"
)

// Name identifies a DNS provider.
type Name string

const (
	Cloudflare   Name = "cloudflare"
	DigitalOcean Name = "digitalocean"
	Route53      Name = "route53"
)

// Supported is every provider servlo can drive, in the order the panel offers
// them.
func Supported() []Name { return []Name{Cloudflare, DigitalOcean, Route53} }

// Credentials is one provider's authentication. Cloudflare and DigitalOcean use
// a bearer token; Route53 uses an access key pair, because its API is signed
// rather than bearer-authenticated.
type Credentials struct {
	Provider Name   `yaml:"-"`
	APIToken string `yaml:"api_token,omitempty"`

	AccessKeyID     string `yaml:"access_key_id,omitempty"`
	SecretAccessKey string `yaml:"secret_access_key,omitempty"`
	Region          string `yaml:"region,omitempty"`
}

// String renders the credential without its secrets, so a log line, an error or
// an API response cannot leak one. A token written into a deploy log outlives
// the moment it was written, which is what makes this worth a method rather
// than care at each call site.
func (c Credentials) String() string {
	switch c.Provider {
	case Route53:
		region := c.Region
		if region == "" {
			region = "default region"
		}
		return fmt.Sprintf("route53 (%s, key %s)", region, redact(c.AccessKeyID))
	default:
		return fmt.Sprintf("%s (token %s)", c.Provider, redact(c.APIToken))
	}
}

// redact keeps enough to tell two credentials apart and not enough to use one.
func redact(secret string) string {
	if secret == "" {
		return "unset"
	}
	if len(secret) <= 4 {
		return "****"
	}
	return "****" + secret[len(secret)-4:]
}

// Validate reports whether these credentials are complete enough to try. A
// half-filled credential fails at the registrar as an opaque 401, which reads
// like a wrong token rather than a missing field.
func (c Credentials) Validate() error {
	switch c.Provider {
	case Cloudflare, DigitalOcean:
		if c.APIToken == "" {
			return fmt.Errorf("%s needs an API token", c.Provider)
		}
	case Route53:
		if c.AccessKeyID == "" || c.SecretAccessKey == "" {
			return fmt.Errorf("route53 needs both an access key ID and a secret access key")
		}
	default:
		return fmt.Errorf("unknown DNS provider %q, servlo supports %v", c.Provider, Supported())
	}
	return nil
}

// credentialsFile is the on-disk shape: one block per provider.
type credentialsFile struct {
	Providers map[string]Credentials `yaml:"providers"`
}

// credentialsPath is in the config directory, never under a site. A site tree is
// served by nginx, cloned from git and readable by the app that runs there, so a
// token in one is a single misconfigured location block from being downloadable.
func credentialsPath() string {
	return filepath.Join(config.ConfigDir(), "dns-providers.yaml")
}

func readCredentialsFile() (credentialsFile, error) {
	out := credentialsFile{Providers: map[string]Credentials{}}
	data, err := os.ReadFile(credentialsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, err
	}
	if err := yaml.Unmarshal(data, &out); err != nil {
		return out, fmt.Errorf("reading %s: %w", credentialsPath(), err)
	}
	if out.Providers == nil {
		out.Providers = map[string]Credentials{}
	}
	return out, nil
}

// SaveCredentials records one provider's credentials, leaving the others alone:
// an operator may hold domains across two registrars, and configuring the
// second must not silently drop the first.
func SaveCredentials(c Credentials) error {
	if err := c.Validate(); err != nil {
		return err
	}
	// The config directory is owner-only so a credential is never briefly
	// readable through a permissive parent.
	if err := os.MkdirAll(config.ConfigDir(), 0700); err != nil {
		return err
	}
	if err := os.Chmod(config.ConfigDir(), 0700); err != nil {
		return err
	}

	file, err := readCredentialsFile()
	if err != nil {
		return err
	}
	file.Providers[string(c.Provider)] = c

	data, err := yaml.Marshal(file)
	if err != nil {
		return err
	}
	// Written through a temp file created 0600, so the secret is never on disk
	// at a wider mode even for an instant, and a crash mid-write cannot leave a
	// truncated credentials file behind.
	tmp := credentialsPath() + ".new"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, credentialsPath())
}

// LoadCredentials returns one provider's credentials.
func LoadCredentials(name Name) (Credentials, error) {
	file, err := readCredentialsFile()
	if err != nil {
		return Credentials{}, err
	}
	c, ok := file.Providers[string(name)]
	if !ok {
		return Credentials{}, fmt.Errorf("no credentials configured for %s: add them before issuing a wildcard certificate", name)
	}
	c.Provider = name
	if err := c.Validate(); err != nil {
		return Credentials{}, err
	}
	return c, nil
}

// Configured lists the providers that have credentials, without their secrets.
// This is what the panel asks to decide whether to offer DNS-01 at all.
func Configured() []Name {
	file, err := readCredentialsFile()
	if err != nil {
		return nil
	}
	var out []Name
	for name := range file.Providers {
		out = append(out, Name(name))
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// RemoveCredentials forgets a provider.
func RemoveCredentials(name Name) error {
	file, err := readCredentialsFile()
	if err != nil {
		return err
	}
	if _, ok := file.Providers[string(name)]; !ok {
		return nil
	}
	delete(file.Providers, string(name))
	data, err := yaml.Marshal(file)
	if err != nil {
		return err
	}
	tmp := credentialsPath() + ".new"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, credentialsPath())
}
