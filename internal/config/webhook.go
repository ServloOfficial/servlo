package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Per-site deploy webhook credentials.
//
// In their own file rather than in the site registry, because the registry is
// written 0644: every site's configuration is readable by anything running on
// the box, which is right for ports and paths and wrong for a token that
// deploys code. This file is 0600 and holds nothing else.
//
// Two values per site, and the split matters. The ID is public: it is in the
// URL, it gets pasted into a repository's settings page, and it will appear in
// somebody's screenshot. The secret never leaves the server except as the key
// to a signature. Using one value for both would mean the URL an operator
// shares is the credential that authorises a deploy.

const (
	webhookIDBytes     = 16
	webhookSecretBytes = 32
)

var webhookMu sync.Mutex

// SiteWebhookConfig is one site's webhook.
type SiteWebhookConfig struct {
	Enabled bool `json:"enabled"`
	// ID is the public half, the last path segment of the endpoint.
	ID string `json:"id,omitempty"`
	// Secret keys the signature. It is never sent to the panel in full.
	Secret string `json:"secret,omitempty"`
	// Branch is the only branch a push may deploy. Empty means any branch,
	// which is a choice an operator has to make deliberately.
	Branch string `json:"branch,omitempty"`
}

// WebhookFile is where the credentials live.
func WebhookFile() string {
	return filepath.Join(ConfigDir(), "deploy-webhooks.json")
}

type webhookStore map[string]SiteWebhookConfig

func readWebhookStore() (webhookStore, error) {
	data, err := os.ReadFile(WebhookFile())
	if err != nil {
		if os.IsNotExist(err) {
			return webhookStore{}, nil
		}
		return nil, err
	}
	store := webhookStore{}
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("reading %s: %w", WebhookFile(), err)
	}
	return store, nil
}

func writeWebhookStore(store webhookStore) error {
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(WebhookFile()), 0o700); err != nil {
		return err
	}
	return writeFileAtomic(WebhookFile(), append(data, '\n'), 0o600)
}

// SiteWebhook returns a site's webhook, disabled when it has none.
func SiteWebhook(site *Site) (SiteWebhookConfig, error) {
	webhookMu.Lock()
	defer webhookMu.Unlock()

	store, err := readWebhookStore()
	if err != nil {
		return SiteWebhookConfig{}, err
	}
	return store[site.Name], nil
}

// EnableSiteWebhook turns a site's webhook on, minting credentials if it has
// none, and sets the branch a push must be on to deploy.
func EnableSiteWebhook(site *Site, branch string) (SiteWebhookConfig, error) {
	webhookMu.Lock()
	defer webhookMu.Unlock()

	store, err := readWebhookStore()
	if err != nil {
		return SiteWebhookConfig{}, err
	}

	hook := store[site.Name]
	hook.Enabled = true
	hook.Branch = branch
	if hook.ID == "" {
		if hook.ID, err = randomToken(webhookIDBytes); err != nil {
			return SiteWebhookConfig{}, err
		}
	}
	if hook.Secret == "" {
		if hook.Secret, err = randomToken(webhookSecretBytes); err != nil {
			return SiteWebhookConfig{}, err
		}
	}

	store[site.Name] = hook
	if err := writeWebhookStore(store); err != nil {
		return SiteWebhookConfig{}, err
	}
	return hook, nil
}

// RegenerateSiteWebhookSecret replaces the secret and keeps the endpoint.
//
// Keeping the URL is the point. After a secret leaks the operator wants every
// old signature to stop working without having to go and re-paste an endpoint
// into a repository they may not administer.
func RegenerateSiteWebhookSecret(site *Site) (SiteWebhookConfig, error) {
	webhookMu.Lock()
	defer webhookMu.Unlock()

	store, err := readWebhookStore()
	if err != nil {
		return SiteWebhookConfig{}, err
	}
	hook, ok := store[site.Name]
	if !ok || hook.ID == "" {
		return SiteWebhookConfig{}, fmt.Errorf("this site has no webhook to regenerate")
	}
	if hook.Secret, err = randomToken(webhookSecretBytes); err != nil {
		return SiteWebhookConfig{}, err
	}

	store[site.Name] = hook
	if err := writeWebhookStore(store); err != nil {
		return SiteWebhookConfig{}, err
	}
	return hook, nil
}

// DisableSiteWebhook turns it off and forgets the credentials.
//
// Forgetting rather than keeping them for later: the reason to turn a webhook
// off is usually that it should not have been on, and an off switch that
// preserves the token means re-enabling quietly resurrects the one that leaked.
func DisableSiteWebhook(site *Site) error {
	webhookMu.Lock()
	defer webhookMu.Unlock()

	store, err := readWebhookStore()
	if err != nil {
		return err
	}
	if _, ok := store[site.Name]; !ok {
		return nil
	}
	delete(store, site.Name)
	return writeWebhookStore(store)
}

// SiteNameByWebhookID finds the site an endpoint belongs to.
func SiteNameByWebhookID(id string) (string, bool, error) {
	if id == "" {
		return "", false, nil
	}
	webhookMu.Lock()
	defer webhookMu.Unlock()

	store, err := readWebhookStore()
	if err != nil {
		return "", false, err
	}
	for name, hook := range store {
		if hook.Enabled && hook.ID == id {
			return name, true, nil
		}
	}
	return "", false, nil
}

// randomToken is URL-safe so it can be a path segment and a header value
// without escaping.
func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating a webhook token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
