// Package imgledger records the container image refs servlo itself pulled. Podman
// has no way to label an image in place, so the ledger is servlo's provenance
// marker: cleanup reclaims servlo's own catalog leftovers while leaving an image
// the user pulled independently that happens to share a catalog repo untouched.
package imgledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/ServloOfficial/servlo/internal/atomicfile"
	"github.com/ServloOfficial/servlo/internal/config"
)

var mu sync.Mutex

// pathFn is the seam tests override to redirect the ledger file.
var pathFn = defaultPath

func defaultPath() string {
	return filepath.Join(config.DataDir(), "pulled-images.json")
}

// Record notes that servlo has pulled ref. Best-effort: a write failure only keeps
// cleanup conservative (an unrecorded image is never reaped as servlo's), so the
// pull path ignores the outcome.
func Record(ref string) {
	if ref == "" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	set := load()
	if set[ref] {
		return
	}
	set[ref] = true
	save(set)
}

// Load returns the set of refs servlo has recorded pulling. A missing or unreadable
// ledger yields an empty set, so cleanup reaps nothing it can't prove is servlo's.
func Load() map[string]bool {
	mu.Lock()
	defer mu.Unlock()
	return load()
}

func load() map[string]bool {
	set := map[string]bool{}
	b, err := os.ReadFile(pathFn())
	if err != nil {
		return set
	}
	var refs []string
	if json.Unmarshal(b, &refs) != nil {
		return set
	}
	for _, r := range refs {
		set[r] = true
	}
	return set
}

// save writes the ledger atomically so a concurrent reader or a mid-write crash
// can never see a truncated file.
//
// Through atomicfile rather than a fixed <path>.tmp, because the mutex above
// orders writers inside one process and the ledger has three: whichever of the
// panel, the watcher or a CLI run pulled the image. Two of them staging at one
// filename write through each other, and what lands is whichever bytes finished
// last over whichever finished first. A ledger that will not parse reads as
// empty, which keeps cleanup conservative rather than reaping something it
// should not, and costs images servlo pulled never being reclaimed.
func save(set map[string]bool) {
	refs := make([]string, 0, len(set))
	for r := range set {
		refs = append(refs, r)
	}
	sort.Strings(refs)
	b, err := json.Marshal(refs)
	if err != nil {
		return
	}
	_ = atomicfile.Write(pathFn(), b, 0o644)
}
