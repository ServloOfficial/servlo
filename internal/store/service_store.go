package store

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/origin"
	"github.com/ServloOfficial/servlo/stores"
)

// The service-preset store mirrors the framework store, under stores/services/:
// an index.json plus one <name>.yaml per preset (service presets carry their
// versions inline, so unlike frameworks there is no per-version file). It reuses
// the Client fetch/fallback machinery so the two stores can never drift.

// ServiceIndex is the top-level index of the service-preset store.
type ServiceIndex struct {
	Services []ServiceIndexEntry `json:"services"`
}

// ServiceIndexEntry describes one preset available in the store, carrying enough
// to render the install picker (name, description, versions) without fetching
// every preset file. The full definition is fetched on install.
type ServiceIndexEntry struct {
	Name           string                 `json:"name"`
	Description    string                 `json:"description"`
	Family         string                 `json:"family,omitempty"`
	EnvRole        string                 `json:"env_role,omitempty"`
	Dashboard      string                 `json:"dashboard,omitempty"`
	DependsOn      []string               `json:"depends_on,omitempty"`
	Image          string                 `json:"image,omitempty"`
	Versions       []config.PresetVersion `json:"versions,omitempty"`
	DefaultVersion string                 `json:"default_version,omitempty"`
	Category       string                 `json:"category,omitempty"`
	Icon           string                 `json:"icon,omitempty"`
	AdminFor       []string               `json:"admin_for,omitempty"`
	// Digest is the sha256 of the preset file this entry names, in the same
	// "sha256:<hex>" form the framework index uses. One per preset rather than
	// one per version, because a service preset is a single file carrying every
	// version it offers.
	Digest string `json:"digest,omitempty"`
}

func init() {
	config.RegisterPresetFetchHook(autoFetchPreset)
}

// autoFetchPreset downloads a service preset from the store into the local cache.
// Registered as config's preset-fetch hook so EnsurePreset can pull a store-only
// preset the first time it is installed and refresh a stale cached one.
func autoFetchPreset(name string) error {
	_, err := NewServiceClient().FetchServicePreset(name)
	return err
}

// NewServiceClient returns a store client pointed at the service-preset store.
func NewServiceClient() *Client {
	urls := origin.ServiceStoreBaseURLs()
	return &Client{BaseURL: urls[0], Fallbacks: urls[1:], Embedded: stores.Services}
}

// FetchServiceIndex downloads and parses the service-preset store index.
func (c *Client) FetchServiceIndex() (*ServiceIndex, error) {
	data, err := c.fetch("index.json")
	if err != nil {
		return nil, fmt.Errorf("fetching service store index: %w", err)
	}
	var idx ServiceIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parsing service store index: %w", err)
	}
	return &idx, nil
}

// FetchServicePreset downloads a preset's YAML, checks it against the digest the
// index records, and saves it verbatim into the store-cache dir. It returns the
// raw bytes on success.
//
// Verified before it is saved, for the reason FetchFramework verifies before it
// parses, and more so: a service preset names the image servlo runs, what it
// mounts and what it is given on its command line, so a swapped one is a
// container of somebody else's choosing on the droplet. A framework definition
// has been checked this way since digests existed and this one had not been,
// which left the higher-stakes half of the store taken on trust.
func (c *Client) FetchServicePreset(name string) ([]byte, error) {
	idx, idxErr := c.FetchServiceIndex()
	data, err := c.fetch(name + ".yaml")
	if err != nil {
		return nil, fmt.Errorf("fetching service preset %q: %w", name, err)
	}
	// An unreachable index leaves nothing to check against. That is the offline
	// case, where the fetch above came from the embedded copy, which no network
	// party can influence.
	if idxErr == nil {
		if entry, ok := idx.find(name); ok {
			if err := verifyDigest(data, entry.Digest, name+".yaml"); err != nil {
				return nil, err
			}
		}
	}
	if err := config.SaveStorePreset(name, data); err != nil {
		return nil, fmt.Errorf("saving service preset %q: %w", name, err)
	}
	return data, nil
}

// find returns the index entry for a preset by name.
func (i *ServiceIndex) find(name string) (ServiceIndexEntry, bool) {
	for _, e := range i.Services {
		if e.Name == name {
			return e, true
		}
	}
	return ServiceIndexEntry{}, false
}

// SearchServices filters the store index by a case-insensitive substring match
// on name, description, or family.
func (c *Client) SearchServices(query string) ([]ServiceIndexEntry, error) {
	idx, err := c.FetchServiceIndex()
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(query)
	var out []ServiceIndexEntry
	for _, e := range idx.Services {
		if strings.Contains(strings.ToLower(e.Name), q) ||
			strings.Contains(strings.ToLower(e.Description), q) ||
			strings.Contains(strings.ToLower(e.Family), q) {
			out = append(out, e)
		}
	}
	return out, nil
}
