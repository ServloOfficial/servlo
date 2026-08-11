# MIGRATION.md — moving to `servlo/servlo` and going public

One-time operation. Delete this file when it is done.

## Why now

Because it is free now and expensive later. There are no releases, no module
consumers and no published container images, so nothing exists that a rename
could break. After `v1.0.0` the same move means either breaking `go install` for
everyone or carrying a `/v2` import path forever, plus republishing images
people already pull.

Going public in the same move buys two things that are otherwise blocked:

- **The store update path switches on.** While the repository is private,
  `raw.githubusercontent.com` returns 404 for every store fetch and each install
  runs on the copy embedded in its binary. That works and is the documented
  fallback, but it means a definition published today does not reach an
  installed binary until that binary is rebuilt.
- **CI comes back.** GitHub gives public repositories standard runners at no
  cost. Actions has never once provisioned a runner on this account, so no
  commit in this project's history has been verified by CI. That ends here.

That second point sets the order below. **Transfer and publish first, sweep
second**, so the 713-file import-path change is the first thing in this project
ever checked by a real Ubuntu runner. Doing the sweep first would waste exactly
the safety net the move hands you.

---

## Phase A — yours, on github.com

GitHub installs redirects from the old location automatically, so nothing breaks
between A and B.

1. **Transfer the repository.** Settings → General → Danger Zone → Transfer, to
   the `servlo` organisation. Keep the name `servlo`, giving `servlo/servlo`.
2. **Make it public.** Settings → General → Danger Zone → Change visibility.
   Read the checklist GitHub shows: history becomes public too. That is fine
   here, `git log` carries no secrets, and `SECURITY.md` is already in place.
   The generated service passwords live on installed machines, never in the
   repository (that was fixed at S4.3 when `embed/` was deleted).
3. **Confirm Actions provisions a runner.** Push anything, or re-run a workflow.
   A job that starts and reports non-zero billable time is the signal. If jobs
   still fail in seconds with zero billable time, stop here and tell me: the
   rest of the plan assumes CI works.
4. **Re-point GitHub Pages** at the new location if the docs site is published
   from it, and check the custom domain if there is one.
5. **Check secrets and variables** survived the transfer. `GITHUB_TOKEN` is
   automatic, but anything you added by hand needs re-adding.

Tell me when A is done. Nothing below needs you until Phase C.

---

## Phase B — mine, one PR

The whole of B is one branch and one PR, and CI verifies it.

**The import path**, 713 Go files plus `go.mod`:

```bash
go mod edit -module github.com/servlo/servlo
grep -rl 'github.com/realrashid/servlo' --include='*.go' . \
  | xargs sed -i 's|github.com/realrashid/servlo|github.com/servlo/servlo|g'
gofmt -l .          # expect empty
go build ./... && go vet ./... && go test ./...
```

The compiler is the check here: a missed import does not build. This is the
least risky large diff in the project, which is another reason to do it while
there is a runner to prove it.

**The one line that moves everything at runtime.** `mainRepo` in
`internal/origin/origin.go` becomes `servlo/servlo`, and with it the release
feed, the release API, the installer URL, all three store fetches, the changelog
and the GHCR image namespace. They all derive from it, deliberately.

**Everything else**, about 35 references:

| File | What |
|---|---|
| `.goreleaser.yml` | 6 ldflags paths, `release.github.owner`, the install line in the release notes, the compare URL |
| `install.sh` | `REPO` default, two raw URLs in the header, the closing project URL |
| `internal/ui/web/src/stores/dashboard.ts` + its test | the docs link the panel opens |
| `internal/ui/web/demo/stubs.ts` | the host pattern the demo lets through |
| `docs/.vitepress/config.ts` | social and edit links |
| `docs/getting-started/installation.md`, `docs/usage/frameworks.md`, `docs/troubleshooting.md`, `docs/contributing/stores.md` | install commands and store URLs |
| `README.md`, `SECURITY.md`, `PRD.md`, `STORY.md`, `CLAUDE.md`, `HANDOVER.md` | project URLs |
| `scripts/publish-base-images.sh` | the `OWNER` default |

**What does not change**, and should be left alone:

- The `servlo-` prefix on units, containers and paths. That is the product name,
  not the repository owner, and it was never `realrashid`.
- The fork statement and link in `README.md`, and the upstream copyright in
  `LICENSE`. The MIT terms require the second one.
- Anything in `stores/`. Definitions carry no repository URL.

**Then the gate**, which for the first time includes CI:

```bash
make build-ui && go build ./... && go test ./... && go vet ./...
test -z "$(gofmt -l .)" && make test-ui && make surface-scan
cd internal/ui/web && npx svelte-check --threshold error   # expect 0
```

Plus, now that runners exist: the installer bats suite, the reboot-resilience
job and the rebuild-from-backup job, which have never run. **Expect these three
to find something.** They are the only checks that exercise a real machine, and
the last two are the backup and rebuild guarantees. If they are green first try,
be suspicious rather than pleased.

---

## Phase C — after B merges

1. **Publish the base images.** With Actions alive this is automatic:
   `base-images.yml` fires on a push to `main` touching the Containerfile, and
   the merge of B touches it via `mainRepo`. If it does not fire, dispatch it by
   hand, which now works because the workflow is on the default branch. See
   `HANDOVER.md` §2.
2. **Run the droplet pass.** `HANDOVER.md` §3. Still the only thing between this
   and a v1 you would put a client on, and the move does not change that.
3. **Tag `v0.1.0`** once the droplet pass is clean. `release.yml` has never
   executed, so treat the first release as a thing to watch rather than fire and
   forget.

---

## What can go wrong

**The old URL in someone's shell history.** GitHub redirects `git` operations
and web traffic, so an existing clone keeps working. `install.sh` piped from the
old raw URL also keeps working via redirect. Neither is a reason to skip
updating them.

**A stale Go module cache.** Anyone who fetched the old path gets a module-path
mismatch. `go clean -modcache` fixes it. With no consumers today this affects
nobody, which is the argument for now.

**The images and the module path are independent.** `mainRepo` moves the image
namespace; the sweep moves the import path. Doing one without the other leaves a
tree that builds and publishes to the wrong place, or vice versa. They are in
the same PR for that reason.

**Actions not actually coming back.** Everything after Phase A step 3 assumes
runners provision. If they do not, Phase B still stands on the local gate as it
does today, and Phase C's image publish falls back to
`scripts/publish-base-images.sh`. Say so rather than assuming CI passed.
