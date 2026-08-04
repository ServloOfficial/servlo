# Building from Source

## Prerequisites

The tray binary requires CGO and `libayatana-appindicator`. See System Tray: Build requirements for per-distro package names.

Go is required to build from source. The released binary has no runtime dependencies.

**Web UI**: the `servlo-ui` dashboard is built from Svelte sources under `internal/ui/web/` and bundled into the Go binary via `//go:embed`. Node.js (20+) and npm are required to rebuild it. `make build` runs `npm install` (once) and `npm run build` automatically before the Go build, so a single `make` command still produces a self-contained binary. If you only change Go code, you can skip the JS build by running `go build` directly against a previously-built `internal/ui/web/dist/` tree.

## Build commands

```bash
make build       # → ./build/servlo  (CGO, with tray support; builds UI first)
make build-nogui # → ./build/servlo-nogui  (no CGO, no tray)
make build-ui    # rebuild only the web UI (internal/ui/web/dist/)
make install     # build + install to ~/.local/bin/servlo
make test        # go test ./...
make test-ui     # run Vitest suite for the web UI
make test-all    # test + test-ui + test-installer (bats)
make clean       # remove ./build/ and internal/ui/web/dist/
```

Tests must isolate servlo's state before they touch it, with `t.Setenv("XDG_CONFIG_HOME", t.TempDir())` and `t.Setenv("XDG_DATA_HOME", t.TempDir())`. Anything writing or deleting a real config file, systemd unit or quadlet panics with the path it tried to touch, because a test that skipped this once removed a developer's servlo-dns quadlet and left the container running under a unit systemd no longer knew about.

## Cross-compile for arm64

Without tray (no CGO required):

```bash
CGO_ENABLED=0 GOARCH=arm64 GOOS=linux go build -tags nogui -o ./build/servlo-arm64 ./cmd/servlo
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
