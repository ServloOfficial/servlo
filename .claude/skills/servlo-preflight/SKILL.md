---
name: servlo-preflight
description: Run the full local verification gate for Servlo before committing — format, build, test, vet, UI tests, installer tests, surface scan, then install and smoke-test on a real droplet. Use before every commit or PR, and whenever asked to "verify", "check CI locally", or "make sure it's green".
---

# Servlo preflight

Run the checks CI runs, locally. Stop at the first failure, report it plainly with
the output, and fix before proceeding. **Do not report "green" unless every step
below actually passed**, and name every step you skipped.

## 1. Format (fastest signal)

```bash
gofmt -l .            # must print nothing
```

If it lists files, run `gofmt -w .`, then re-check.

## 2. Build the UI only if it changed

If anything under `internal/ui/web/` changed:

```bash
make build-ui
make test-ui          # Vitest
```

Skip both if you only touched Go — the embedded `dist/` is reused.

## 3. Go build, test, vet

```bash
go build ./cmd/servlo
go test ./...
go vet ./...
```

Servlo is a server-only, CGO-free build. There is no tray, no appindicator and no
`nogui` tag to work around — if you find yourself reaching for one, something
deleted has come back.

## 4. Installer tests, if install.sh changed

```bash
bats tests/installer/installer.bats
```

## 5. Surface scan

```bash
make surface-scan     # deleted-feature + permission audit
```

This is the gate that keeps the fork honest: it enumerates binary symbols and API
routes and fails if any deleted development feature reappears, or if any
state-changing route lacks a declared permission.

## 6. Install and smoke-test

```bash
make build-server     # CGO-free production binary
make install          # → ~/.local/bin/servlo, restarts servlo-panel / servlo-watcher
```

Then actually drive the affected surface — a CLI command, a panel page — and
observe the behaviour. A change with a runtime surface is not verified by tests
alone.

## Targets that do not exist yet

`make build-server` arrives in S0.2 and `make surface-scan` during Phase 0. Until
then the Makefile has `build`, `build-ui`, `build-tray`, `test`, `test-ui`,
`test-installer`, `test-all`, `install`, `release`, `release-snapshot`, and the
entrypoint is still `./cmd/lerd`. Report a missing target as missing. Never
substitute a different command and call the step passed.

## Rules

- Never `sudo`. If a step needs privilege, print the exact command for the human.
- Never install anywhere but `~/.local/bin/servlo`.
- **Step 6 needs a real Ubuntu 24.04 droplet or VM and cannot run in a browser
  session.** When it cannot run, say that the smoke test did not run. Tests do not
  catch runtime-surface bugs, so a green gate without step 6 is a partial result,
  not a pass.
- If any step is skipped, say which and why, rather than implying full coverage.
