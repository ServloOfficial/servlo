# Building from Source

## Prerequisites

Go is required to build from source. Servlo is a CGO-free build, so the released binary has no runtime dependencies.

**Web UI**: the `servlo-ui` dashboard is built from Svelte sources under `internal/ui/web/` and bundled into the Go binary via `//go:embed`. Node.js (20+) and npm are required to rebuild it. `make build` runs `npm install` (once) and `npm run build` automatically before the Go build, so a single `make` command still produces a self-contained binary. If you only change Go code, you can skip the JS build by running `go build` directly against a previously-built `internal/ui/web/dist/` tree.

## Build commands

```bash
make build       # → ./build/servlo  (builds UI first)
make build-server # → ./build/servlo-linux-amd64  (static production build)
make build-ui    # rebuild only the web UI (internal/ui/web/dist/)
make install     # build + install to ~/.local/bin/servlo
make test        # go test ./...
make test-ui     # run Vitest suite for the web UI
make surface-scan # deleted-feature gate
make test-all    # test + test-ui + test-installer (bats) + surface-scan
make clean       # remove ./build/ and internal/ui/web/dist/
```

## The surface scan

Servlo removes a set of upstream features outright rather than hiding them behind a flag, so the only durable check is one that reads the tree and fails when a name comes back. `make surface-scan` is that check, and CI runs it on every push.

Each deleted feature is one rule in `internal/surfacescan/rules.go`, naming the feature, the story that owns its deletion, and the patterns that would betray its return. A rule marked `Enforced` fails the scan. A rule that is not enforced names a feature a later Phase 0 story still has to delete; those are reported as pending so the gate is green today and the remaining work stays visible. Deleting a feature therefore ends with turning its own rule on, which is what makes the deletion stick.

Patterns are matched against a file's path and each of its lines, and against a camel-split copy of each line, so a rule can be written as a plain word and still catch the same word buried inside a camel-case identifier. Without that, a deleted feature comes back simply by living inside a longer name. The specification documents that describe the deletions on purpose, and the rules file itself, are exempt.

Tests must isolate servlo's state before they touch it, with `t.Setenv("XDG_CONFIG_HOME", t.TempDir())` and `t.Setenv("XDG_DATA_HOME", t.TempDir())`. Anything writing or deleting a real config file, systemd unit or quadlet panics with the path it tried to touch, because a test that skipped this once removed a developer's servlo-dns quadlet and left the container running under a unit systemd no longer knew about.

## Cross-compile for arm64

```bash
make build-server SERVER_ARCH=arm64   # → ./build/servlo-linux-arm64
```

The UI only needs to be built once per source-tree state; the emitted `dist/` is architecture-independent.

## Developing the web UI

```bash
cd internal/ui/web
npm install            # once
npm run dev            # Vite dev server at http://localhost:5173 (proxies /api/* to 7073)
npm run check          # svelte-check + tsc
npm test               # Vitest
```

The Vite dev server proxies `/api/*`, `/icons/*`, `/manifest.webmanifest`, `/sw.js`, and `/offline.html` to a running `servlo-ui` on `:7073`, so you get hot-reload on the Svelte side while the Go backend handles the data. Run `servlo start` (or `make install` once) first so the backend is up.

## Installing a local build

To test a local build end-to-end using the installer:

```bash
make build
bash install.sh --local ./build/servlo
```

This runs the full installer flow (prerequisite checks, PATH setup, `servlo install`) using your locally built binary instead of downloading from GitHub.
