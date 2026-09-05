package backupdest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ServloOfficial/servlo/internal/config"
	"gopkg.in/yaml.v3"
)

// storeFile holds the destinations. 0600 because an S3 secret key is in it, the
// same reason the connection registry beside it is.
const storeFile = "backup-destinations.yaml"

var storeMu sync.Mutex

// Registry is every destination archives are copied to.
type Registry struct {
	Destinations []Destination `yaml:"destinations"`
}

func storePath() string { return filepath.Join(config.ConfigDir(), storeFile) }

// Load reads the destinations, treating a missing file as none configured
// rather than as an error: a server with no offsite copy is the state every
// install starts in.
func Load() (Registry, error) {
	raw, err := os.ReadFile(storePath())
	if os.IsNotExist(err) {
		return Registry{}, nil
	}
	if err != nil {
		return Registry{}, fmt.Errorf("reading the backup destinations: %w", err)
	}
	var reg Registry
	if err := yaml.Unmarshal(raw, &reg); err != nil {
		return Registry{}, fmt.Errorf("parsing the backup destinations: %w", err)
	}
	return reg, nil
}

func save(reg Registry) error {
	raw, err := yaml.Marshal(reg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(config.ConfigDir(), 0700); err != nil {
		return err
	}
	return os.WriteFile(storePath(), raw, 0600)
}

// Add stores a destination, refusing one that could not work and one whose name
// is taken. Replacing silently would leave an operator believing archives were
// going to two places when they were going to one.
func Add(d Destination) error {
	if err := d.Validate(); err != nil {
		return err
	}
	storeMu.Lock()
	defer storeMu.Unlock()

	reg, err := Load()
	if err != nil {
		return err
	}
	for _, existing := range reg.Destinations {
		if strings.EqualFold(existing.Name, d.Name) {
			return fmt.Errorf("a destination called %q is already configured: remove it first", d.Name)
		}
	}
	reg.Destinations = append(reg.Destinations, d)
	return save(reg)
}

// Remove forgets a destination. The archives already there are left alone,
// because deleting somebody's offsite copies as a side effect of editing a
// setting is never what was meant.
func Remove(name string) error {
	storeMu.Lock()
	defer storeMu.Unlock()

	reg, err := Load()
	if err != nil {
		return err
	}
	var kept []Destination
	var found bool
	for _, d := range reg.Destinations {
		if strings.EqualFold(d.Name, name) {
			found = true
			continue
		}
		kept = append(kept, d)
	}
	if !found {
		return fmt.Errorf("no destination called %q", name)
	}
	reg.Destinations = kept
	return save(reg)
}

// Named finds one destination.
func Named(name string) (Destination, error) {
	reg, err := Load()
	if err != nil {
		return Destination{}, err
	}
	for _, d := range reg.Destinations {
		if strings.EqualFold(d.Name, name) {
			return d, nil
		}
	}
	return Destination{}, fmt.Errorf("no destination called %q", name)
}
