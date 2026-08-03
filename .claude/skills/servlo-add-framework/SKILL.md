---
name: servlo-add-framework
description: Add or extend a Servlo framework definition (a new PHP framework or a new major version) as versioned YAML in the stores/frameworks store. Use whenever the task involves framework detection, per-framework workers, env wiring, deploy templates, doctor checks, custom commands or a WordPress-style exclude list — never branch on a framework name in Go.
---

# Add a Servlo framework definition

Framework definitions live in **`stores/frameworks/<name>/<version>.yaml`** in this
repository, one file per major version. Servlo is framework-agnostic: no Go code
knows a framework's name. Everything a framework needs is declared here as data.

Upstream authored these in a separate `lerd-env/frameworks` repo so a change
shipped to every install in ~24h with no binary release. Servlo keeps the same
file layout so the fetch code in `internal/origin/origin.go` needs only new base
URLs, but the no-release property is not live yet: this repository is private, so
an installed binary cannot fetch `raw.githubusercontent.com` from it without a
token. The runtime fetch still points at the public `lerd-env/frameworks` until
S0.8 resolves that. Author here anyway; do not assume a droplet sees your change.

## Procedure

1. **Copy the closest existing definition.** `laravel/12.yaml` is the most
   complete reference. For a new major version of an existing framework, copy the
   previous version file and adjust. The existing YAML is the schema of record —
   do not invent fields.

2. **Fill the sections that apply:**
   - `name`, `version`, `label`, `public_dir`, `create` (scaffold command)
   - `php.min` / `php.max` — the versions the framework supports
   - `detect` — marker file, lockfile or `composer:` package that identifies it,
     including the major version
   - `env` — `.env` file/format, `key_generation`, and the `services` map wiring
     each service (mysql, postgres, redis, meilisearch…) with detection rules and
     the vars to inject. **Env vars belong to the site, declared here — never on
     workers.**
   - `workers` — queue, schedule and any framework-specific long-runners. Each has
     `command`, `restart`, optional `check`/`exclude_check`, `conflicts_with`,
     `proxy`, `host`. **New workers go here, not in Go.**
   - `deploy` — the build commands, migration command, health path and exclude
     list that S9.1 requires. A WordPress-style `exclude` list keeping
     `wp-content/uploads` and `wp-content/plugins` is declared here, never in Go.
   - `setup` — post-create steps with sensible `default:`
   - `doctor.checks` — declarative checks (`env_combo`, `symlink`, `command`) with
     a `fix:` command and a human `detail:` string
   - `commands` — dashboard custom commands, with `confirm` where destructive
   - `logs`, `console`, `composer`, `npm`

   **Fields that exist upstream and must not appear here:** `tinker` and
   `per_worktree`. The Tinker REPL and git worktrees are deleted from Servlo
   (CLAUDE.md 3.1), and the surface scan fails the build if either returns.
   Mailpit is likewise gone from the `env.services` map.

3. **Update `stores/frameworks/index.json`** if the store requires it, following
   how existing entries are registered.

4. **Validate against a real site** on an Ubuntu 24.04 droplet: create a site of
   that framework and confirm detection, PHP pinning, `.env` wiring, workers,
   deploy template and doctor checks all behave. A browser session cannot do this;
   say so rather than implying it passed.

## Rules

- Data only. If you are tempted to write Go that branches on the framework name,
  the logic belongs in this YAML instead.
- Version the file by major version; keep detection specific enough to pick the
  right one.
- Production defaults, not development ones. Nothing here may assume a `.test`
  domain, mkcert, a local resolver or a developer's laptop.
