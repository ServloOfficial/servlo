# The stores

Frameworks, service presets and one-click apps are data, not code. A new
framework's workers, env wiring, deploy template, doctor checks or exclude list
is a YAML change under `stores/`, never a Go change. If you find yourself
branching on a framework name in Go, the logic belongs here instead.

```
stores/
  frameworks/index.json          frameworks/<name>/<version>.yaml
  services/index.json            services/<name>.yaml
  apps/index.json                apps/<name>.yaml
```

## Where a definition comes from at runtime

Two sources, in order:

1. **The network.** An installed binary fetches from
   `raw.githubusercontent.com/realrashid/servlo/main/stores/<kind>/…`. This is
   how a definition published since a build reaches an existing install.
2. **The binary itself.** `stores/stores.go` embeds the whole tree, and the
   store client falls through to it when no base answers.

The embedded copy is the floor, not a cache. It exists because the fetch cannot
be the only way in: the repository is private today, so
`raw.githubusercontent.com` answers 404, and an install that depended on the
fetch would have no frameworks at all. Tying a fresh install to the
repository's visibility is not a tradeoff worth making even once the repository
is public, because a droplet with no outbound route is an ordinary case.

The practical consequence: **a binary always has every definition it shipped
with.** Adding a framework to `stores/` makes it available to the next build
immediately, and to already-installed binaries as soon as they can reach the
repository.

`SERVLO_STORE_BASE_URL`, `SERVLO_SERVICES_BASE_URL` and `SERVLO_APPS_BASE_URL`
override the fetch base (comma-separated for several), for a mirror or a test
rig. The embedded fallback still applies underneath.

## Pinning

A definition already on disk is **not** re-fetched. It carries the deploy
commands and the worker set for every site on that framework, so replacing it on
a timer would change what the next deploy runs on a site nobody touched. A
definition that is missing is always fetched, since there is nothing to pin and
nothing to serve.

To move a pinned install forward, opt in:

```yaml
stores:
  auto_refresh: true
```

## What integrity checking covers, and what it does not

`index.json` records a `sha256` for every definition it lists, and a fetched
body that does not match is refused before it reaches the parser. That matters
because a framework definition is not inert data: it declares the commands
servlo runs on deploy and the images it starts, so a swapped one is code
execution on the droplet.

This is **integrity, not authenticity**. It detects a definition that was
changed independently of the index: a broken mirror, a truncated download, a
tampered file behind a `SERVLO_STORE_BASE_URL` override. It does **not** defend
against someone who controls the index itself, because they would publish a
matching digest alongside the swapped file.

Closing that gap needs a signature over the index, made with a key servlo does
not hold and cannot rotate on the maintainer's behalf. A key committed to this
repository would sign nothing meaningful, so none is claimed; `verifyDigest` in
`internal/store/integrity.go` is the seam a real signature check would slot
into. Until then the strongest guarantee is the embedded copy: every binary
carries the definitions it shipped with, compiled in, and no fetch can replace
them.

**Regenerate the digests whenever you edit a definition.** `go test
./internal/store/` fails when a digest and its file have drifted apart, which is
the reminder.

## Adding or changing a definition

1. Write the YAML. Copy the closest existing file; those files are the schema of
   record, and their comments explain the fields that are not obvious.
2. Add it to that store's `index.json`. Nothing can fetch a definition the index
   does not list, because every path into the store starts from the index. For a
   framework, list the version under `versions` and set `latest` when it is the
   one new projects should get.
3. Run `go test ./internal/store/`. The store tests walk the index and parse
   every file it names against the schema this binary actually reads, in both
   directions: an indexed file that is missing fails, and a file nobody indexed
   fails too.
4. Run `make surface-scan`. The scan walks `stores/` like any other directory, so
   a definition that names a deleted feature is caught here rather than shipping.
   This is not hypothetical: the mirror this store was seeded from carried
   blocks for two features Servlo has since removed, and the scan is what found
   them.

A definition that parses but is wrong is still wrong. Install the local build on
a real Ubuntu 24.04 droplet and link a project that uses it; the store tests
prove the shape, not the behaviour.

## Promotion

There is one branch of record. A definition is promoted by merging it to `main`:

- **To the next build**, immediately. `stores/` is embedded at compile time, so
  the merge is the release for anyone who installs or upgrades afterwards.
- **To existing installs**, on their next fetch. Framework definitions are
  fetched on demand when a site needs a version it does not have locally;
  service presets on install. There is no push.

Because those two paths run at different times, a definition must be correct on
its own rather than correct alongside a matching Go change. A field the running
binary does not understand is ignored, so adding one is safe; changing what an
existing field means is not, and needs a new version file instead.

The stores were seeded from the upstream Lerd stores, which Servlo consumed
until S0.8. They are Servlo's own now and drift from upstream on purpose: the
deleted-feature schema was stripped on the way in.
