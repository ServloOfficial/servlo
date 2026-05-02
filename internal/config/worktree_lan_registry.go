package config

import (
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

// WorktreeLANEntry tracks one LAN share proxy bound to a worktree subdomain.
// LAN ports are machine-local (don't travel with the branch), so this registry
// lives under ~/.local/share/lerd, separate from the worktree's .lerd.yaml.
type WorktreeLANEntry struct {
	Site   string `yaml:"site"`
	Branch string `yaml:"branch"`
	Port   int    `yaml:"port"`
}

type worktreeLANRegistry struct {
	Entries []WorktreeLANEntry `yaml:"worktree_lan_shares"`
}

var worktreeLANRegistryMu sync.Mutex

// WorktreeLANRegistryPath returns the path to the per-user LAN-share registry.
func WorktreeLANRegistryPath() string {
	return filepath.Join(DataDir(), "worktree-lan-shares.yaml")
}

// LoadWorktreeLANRegistry returns persisted entries, empty when the file is missing.
func LoadWorktreeLANRegistry() ([]WorktreeLANEntry, error) {
	worktreeLANRegistryMu.Lock()
	defer worktreeLANRegistryMu.Unlock()
	return loadWorktreeLANRegistryLocked()
}

func loadWorktreeLANRegistryLocked() ([]WorktreeLANEntry, error) {
	path := WorktreeLANRegistryPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var reg worktreeLANRegistry
	if err := yaml.Unmarshal(data, &reg); err != nil {
		return nil, err
	}
	return reg.Entries, nil
}

func saveWorktreeLANRegistryLocked(entries []WorktreeLANEntry) error {
	path := WorktreeLANRegistryPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if len(entries) == 0 {
		return os.WriteFile(path, []byte("worktree_lan_shares: []\n"), 0644)
	}
	data, err := yaml.Marshal(worktreeLANRegistry{Entries: entries})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// AddWorktreeLAN inserts or updates an entry by (site, branch).
func AddWorktreeLAN(e WorktreeLANEntry) error {
	worktreeLANRegistryMu.Lock()
	defer worktreeLANRegistryMu.Unlock()
	entries, err := loadWorktreeLANRegistryLocked()
	if err != nil {
		return err
	}
	for i, existing := range entries {
		if existing.Site == e.Site && existing.Branch == e.Branch {
			entries[i] = e
			return saveWorktreeLANRegistryLocked(entries)
		}
	}
	entries = append(entries, e)
	return saveWorktreeLANRegistryLocked(entries)
}

// RemoveWorktreeLAN deletes an entry and returns it along with whether one was found.
func RemoveWorktreeLAN(site, branch string) (WorktreeLANEntry, bool, error) {
	worktreeLANRegistryMu.Lock()
	defer worktreeLANRegistryMu.Unlock()
	entries, err := loadWorktreeLANRegistryLocked()
	if err != nil {
		return WorktreeLANEntry{}, false, err
	}
	var removed WorktreeLANEntry
	found := false
	kept := entries[:0]
	for _, e := range entries {
		if !found && e.Site == site && e.Branch == branch {
			removed = e
			found = true
			continue
		}
		kept = append(kept, e)
	}
	if !found {
		return WorktreeLANEntry{}, false, nil
	}
	return removed, true, saveWorktreeLANRegistryLocked(kept)
}

// FindWorktreeLAN returns the entry for (site, branch).
func FindWorktreeLAN(site, branch string) (WorktreeLANEntry, bool, error) {
	entries, err := LoadWorktreeLANRegistry()
	if err != nil {
		return WorktreeLANEntry{}, false, err
	}
	for _, e := range entries {
		if e.Site == site && e.Branch == branch {
			return e, true, nil
		}
	}
	return WorktreeLANEntry{}, false, nil
}

// WorktreeLANsForSite returns all entries belonging to a given site.
func WorktreeLANsForSite(site string) ([]WorktreeLANEntry, error) {
	entries, err := LoadWorktreeLANRegistry()
	if err != nil {
		return nil, err
	}
	var out []WorktreeLANEntry
	for _, e := range entries {
		if e.Site == site {
			out = append(out, e)
		}
	}
	return out, nil
}
