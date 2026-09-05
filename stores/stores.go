// Package stores carries the framework, service and app definitions Servlo
// serves, and embeds them into the binary.
//
// The definitions are data, not code (CLAUDE.md §2): a new framework's workers,
// env wiring, deploy template or doctor checks are a YAML change here, never a
// Go change. The layout mirrors what the store client fetches over HTTP, so the
// same path resolves either way:
//
//	frameworks/index.json          services/index.json      apps/index.json
//	frameworks/<name>/<ver>.yaml   services/<name>.yaml     apps/<name>.yaml
//
// Embedding is what makes the store reachable at all. An installed binary
// fetches definitions from raw.githubusercontent.com, and making that the only
// way in would tie a fresh install to the network answering and to the
// repository staying public. The embedded copy is the floor: every
// binary already holds every definition it shipped with, and the network fetch
// is how a definition published since that build reaches an existing install.
package stores

import (
	"embed"
	"io/fs"
)

//go:embed frameworks services apps
var files embed.FS

// Kind names one of the three stores. It is the subdirectory here and the last
// path segment of the corresponding base URL.
type Kind string

const (
	Frameworks Kind = "frameworks"
	Services   Kind = "services"
	Apps       Kind = "apps"
)

// Read returns the bytes of a path within a store, e.g. Read(Frameworks,
// "laravel/13.yaml"). ok is false when this binary shipped no such file, which
// is the ordinary answer for a definition published after it was built.
func Read(kind Kind, path string) (data []byte, ok bool) {
	b, err := files.ReadFile(string(kind) + "/" + path)
	if err != nil {
		return nil, false
	}
	return b, true
}

// FS returns the embedded tree, for callers that want to walk a store rather
// than read one known path.
func FS() fs.FS { return files }
