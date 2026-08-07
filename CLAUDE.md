# CLAUDE.md — agent guide for the Servlo codebase

You are a coding agent working on **Servlo**: a free, open-source (MIT), production PHP server panel for Ubuntu 24.04 LTS. It is a fork of Lerd (lerd-env/lerd), a local PHP development environment. Servlo keeps Lerd's engine — rootless Podman, systemd units, nginx vhost generation, worker supervision, the Svelte dashboard, the YAML store architecture — and aims it at real servers, real domains and real certificates.

This file is loaded into every session. Read it before you touch anything, then follow it exactly. It overrides your defaults, and it overrides any habit you have from the upstream Lerd codebase. When a rule here conflicts with something you see in inherited code or docs, this file wins.

`PRD.md` is the product specification and `STORY.md` is the backlog. When implementing a story, read its acceptance criteria first and write the failing test from them.

---

## 1. What Servlo is, in one paragraph

One operator (plus a small team) runs many PHP sites on one Ubuntu droplet. They add a site from a ZIP, a GitHub clone, an existing folder, or a one-click app install; bind it to a real domain; click **Get SSL** when DNS propagates; pick a PHP version per site; toggle services like MySQL and Redis; deploy with `git pull` plus a per-site script; and sleep because backups are verified by real test restores. It is the cPanel/Forge/Ploi niche, self-hosted and free.

It is **not** multi-tenant hosting. There are no per-client Linux users, no root broker daemon, no edge proxy. All sites run as the same Linux user — a documented, accepted tradeoff (PRD §6). Do not "fix" this.

---

## 2. The design laws

Inherited from upstream and kept. Almost every mistake an agent makes here violates one of these.

1. **Store-first, never hardcode.** Frameworks, services and apps are versioned YAML in the `stores/frameworks/`, `stores/services/` and `stores/apps/` stores in this repository. A new framework's deploy template, worker set, env wiring, doctor checks or exclude list is a YAML change, not a Go change. Copy the closest existing YAML; those files are the schema of record.
2. **Framework-agnostic.** No Go code may know the name "Laravel" (or WordPress, or any framework). If you find yourself branching on a framework name in Go, the logic belongs in the store as declarative data. This includes deploy scripts, cron behaviour and WordPress's exclude list — all declared in YAML.
3. **Env belongs to sites, not workers.** All environment variables live in the site's `.env`. Workers never declare env vars. Services inject host/port/credentials into the site `.env` via the framework's `env.services` mapping.

---

## 3. Servlo-specific laws — these differ from upstream

These are the decisions that make Servlo not-Lerd. They are settled. Do not relitigate them in code, and do not resurrect upstream behaviour that contradicts them.

### 3.1 Deleted means deleted

The following are **removed from the codebase**, not disabled, not gated, not hidden behind a flag. If a task seems to need one of them, stop and say so instead of reintroducing it:

- The MCP server (`internal/mcp`, all `mcp:*` commands)
- The Tinker in-browser PHP REPL
- The container shell drop-in
- The SPX profiler
- The `dump()`/`dd()` bridge — including its `auto_prepend` mount in generated PHP-FPM units; verify removal by inspecting a generated unit, not a runtime flag
- Xdebug toggles
- Browser editing of per-version `php.ini` (per-**site** PHP settings exist instead; see §3.4)
- `.test` domains, the dnsmasq container, all host resolver mutation, the sudoers rule, `.localhost` mode
- mkcert
- Git worktrees, idle-suspend, LAN sharing, tunnel sharing
- Mailpit, the system tray, all macOS and WSL2 code paths
- Inline service definitions read from a project's config file (only reviewed store presets may run containers)

A CI **surface scan** enumerates binary symbols and API routes and fails the build if any of these reappears or if any state-changing route lacks a declared permission. Keep it green.

### 3.2 Platform and privilege

- **Ubuntu 24.04 LTS only.** The installer refuses other distros, and refuses 22.04 with Podman < 4.5 while printing the upgrade path. Never half-install.
- **No permanent root process.** Ports 80/443 come from the `net.ipv4.ip_unprivileged_port_start=0` sysctl (fallback: nftables DNAT 80→8080 / 443→8443). One rule at install, verified by `servlo doctor` afterward.
- **Never run sudo from a tool call.** If a step needs privilege, print the exact command for the human. This is upstream's rule and it is kept absolutely.
- **SSH password authentication stays enabled.** Servlo manages authorised keys additively and configures fail2ban, but never disables password login. Do not add code that does.

### 3.3 Certificates and domains

- Sites live on real FQDNs. No TLD is ever appended. No host resolver file is ever written.
- The issuer is ACME (Let's Encrypt), HTTP-01 default, DNS-01 for wildcards, behind the `CertIssuer` interface. The inherited renewal scanner, expiry warnings and nginx reload operate through that interface.
- The **Get SSL** button is disabled until a live DNS check shows every domain (primary and all aliases) resolving to this server, and it displays the actual mismatch while waiting. Never issue against a domain that does not point here.
- Renewal failure must be loud: dashboard banner, audit entry, email when panel SMTP exists. A site never silently serves an expired certificate.

### 3.4 Sites and settings

- Each site has **its own PHP-FPM pool**; PHP settings are per-site, not per-version.
- Cross-system settings are single fields that write everywhere at once: **Max upload size** → `upload_max_filesize` + `post_max_size` + nginx `client_max_body_size`; **Max execution time** → `max_execution_time` + `fastcgi_read_timeout`/`fastcgi_send_timeout`. Never expose half of one of these pairs.
- Every nginx write runs `nginx -t` before committing and keeps a timestamped backup with restore. No exceptions, including generated config.
- Production PHP defaults on every site: `display_errors=Off`, `expose_php=Off`, OPcache on with `validate_timestamps=0`.

### 3.5 Deploy

- Deploy is `git pull` in place plus the site's editable deploy script (template from the framework store). **No `releases/` directories, no atomic-release machinery** — that is explicitly deferred; do not build it.
- A database backup runs automatically before any deploy whose script contains a migration.
- WordPress deploys honour a per-site exclude list (`wp-content/uploads`, `wp-content/plugins` by default) declared in the store YAML. A deploy must never delete a client's uploaded media or installed plugins.
- "Redeploy previous commit" checks out the prior commit and re-runs the script; it does not and must not attempt to revert migrations.
- Asset builds (`npm run build`) run in a memory-capped scope so an oversized build fails alone instead of letting the OOM killer take MySQL down.

### 3.6 Databases

- A database is a **connection**, which may be a local container or an external managed database (e.g. DigitalOcean Managed with CA cert). Both are first-class in every code path — never assume the database is local.
- Each site gets its own database and least-privilege user, scoped to its own schema.
- Managed-database flows must surface the droplet's public IP for the provider's trusted-sources list.

### 3.7 Security posture

- Services bind to the container network only; nothing publishes to a public interface. Strong generated passwords.
- Panel auth: Argon2id, session cookies (`HttpOnly`, `Secure`, `SameSite=Strict`), CSRF on every state-changing route, per-IP rate limiting with lockout, optional TOTP. Two roles: Admin and Developer (assigned sites only), enforced on every route **and every WebSocket message**.
- Every state-changing route declares a permission; the surface scan enforces this.
- Every state-changing action is written to the append-only audit log (0600, rotated, never truncated by the app).
- Secrets are redacted in logs, deploy output and API responses. `.env` files are 0600.
- No mail server, ever. SMTP settings per site and for the panel; that is the whole email story.

---

## 4. Where things live

```
cmd/servlo           CLI + long-running servlo-panel / servlo-watcher entrypoints
internal/
  podman/            Quadlet generation, container lifecycle
  nginx/ certs/      site serving, vhost generation, ACME issuance & renewal
  services/ serviceops/  service-preset engine + operations
  store/ registry/   fetch + verify the stores/frameworks, stores/services, stores/apps stores
  siteops/ siteinfo/ site creation (folder/zip/git/app), domains, aliases, redirects
  deploy/            git pull + deploy script runner, pre-deploy DB backup, redeploy-previous
  worker*/           queue/schedule/horizon/custom workers, self-heal, cron timers
  sitedoctor/        framework-agnostic health checks
  backup/            backup, scheduled timers, test restore, S3/SFTP drivers, server rebuild
  authz/             sessions, CSRF, TOTP, roles, permission registry, audit log
  ui/web/            Svelte panel (built + //go:embed'd into the binary)
pkg/distro/          Ubuntu detection + refusal for everything else
docs/                VitePress docs site (docs/.vitepress/)
tests/installer/     bats tests for install.sh
```

S0.1 landed the rename, so the tree above is what you will actually find. The module path is `github.com/realrashid/servlo` and the entrypoint is `cmd/servlo`; `cmd/lerd-tray` is gone.

The only strings left containing "lerd" are the one upstream dependency PRD §0 retains on purpose, and it must stay: the GHCR PHP base images (`ghcr.io/lerd-env/lerd-php*`). Alongside it sit the fork statement in `README.md`, the upstream copyright in `LICENSE`, and two upstream issue citations in code comments. Treat that set as an allowlist: anything else spelling "lerd" is a regression.

S0.8 brought the stores in-repo: `stores/frameworks/`, `stores/services/` and `stores/apps/` are the definitions themselves, and `stores/stores.go` embeds them into the binary. The layout mirrors what the client fetches, so `internal/origin` needs only the base URL. The embedded copy is the floor an install bootstraps from; the fetch is how a definition published since that build reaches an existing install. The surface scan walks `stores/` like any other directory, so a deleted feature cannot come back as store data either.

Config: `~/.config/servlo/`. Data: `~/.local/share/servlo/`. systemd units are prefixed `servlo-`. Never install to `/usr/local/bin`; the binary goes to `~/.local/bin/servlo`.

The web UI is Svelte under `internal/ui/web/`, built to `dist/` and embedded via `//go:embed`. `make build` builds the UI first.

`make build-server` (the CGO-free production target, S0.2) and `make surface-scan` (the standing deleted-feature gate, S0.4) both exist now. The surface scan lives in `internal/surfacescan`: one rule per deleted feature, naming the story that owns its deletion. An `Enforced` rule fails the gate; a rule whose story has not run yet is reported as pending, so deleting a feature ends with turning its own rule on. The permission half landed with S5.5. `internal/authz` declares one permission per route in `Permissions()`, and the scan reads the panel's own dispatch — the mux registrations and the auth middleware's switch — failing the build on a route that declares none, or a declaration no route registers. The enforcement in `scope_http.go` reads the same table, so there is one list rather than two to keep in step.

---

## 5. The contribution lifecycle — follow every step, in order

### Step 0 — An issue exists first
Recommended while the project is one person, required again the moment a second joins. Frame it as future work, one issue per unit of work. During the phased build the backlog in `STORY.md` is the issue list, so a story does not need one raised first.

Sessions here have no `gh` CLI. GitHub goes through the MCP tools.

### Step 1 — Understand before you build
Read the surrounding package and the closest existing example. Match its naming, comment density and idioms. Decide which layer the change belongs to using §2 and §3. If it is store data, you are editing YAML, not Go. If the story is in `STORY.md`, its acceptance criteria are the spec.

### Step 2 — Write the test first (TDD)
New functionality must include tests; behaviour changes must update them. A PR without corresponding coverage will not merge. Write the failing test, then make it pass. Keep test fixtures in the repo, not `/tmp`.

### Step 3 — Implement (DRY, KISS)
Reuse existing patterns and helpers. In the web UI, extract shared markup into components from the start — never copy markup between views. Pick the simplest design that satisfies the issue. Comments only for what is not self-evident, 2–3 lines at most.

### Step 4 — Document it
Update the relevant page under `docs/` for every feature or behaviour change, before committing.

### Step 5 — Run the full local gate
```
make build-ui                          # if UI changed
go build ./cmd/servlo                  # build
go test ./...                          # tests
go vet ./...                           # vet
test -z "$(gofmt -l .)"                # format
make test-ui                           # Vitest, if UI changed
bats tests/installer/installer.bats    # if install.sh changed
make surface-scan                      # deleted-feature gate
```
CI runs the same gate on a real Ubuntu 24.04 runner. That is the gate for a story: local green, then CI green, then merge.

**The droplet smoke test is deferred to the end of the build, by the project owner's decision.** It used to sit here as a per-story gate, which in a browser session meant every story ended blocked on something no session could do. It now happens once, against the finished product, after the last phase lands. Do not wait for it, do not treat it as a merge condition, and do not re-raise it story by story.

What this does not change: say plainly what ran. A story is "tests and CI green", not "verified working on a server", and the two are different claims. Write the honest one. Anything genuinely unverifiable in a session (a real certificate from Let's Encrypt, a live registrar, a running container's bind mount) is worth one line in the PR body so the eventual droplet pass knows where to look, and no more than that.

### Step 6 — Commit, PR, merge
The standing instruction for this build is to work straight through the phases: write the code and its tests, run the gate, open the PR, wait for CI, merge to `main`, and start the next story without stopping to ask. Do not pause at phase boundaries for a manual check.

This is a deliberate relaxation of the older "only commit when asked" rule and applies to the phased build in `STORY.md`. It is not licence to skip the gate, invent a story, or start work outside the backlog: the ordering and the scope still come from `STORY.md`, and anything that is a genuine judgement call about the product still gets raised.

### Step 7 — Open the PR the Servlo way (see §7)

---

## 6. Commit conventions

- Branch off `main`; never commit straight to `main`.
- Conventional-commit subject (`feat:`, `fix:`, `docs:`…). Body is prose paragraphs that read like a human wrote them, single-line paragraphs, no robotic bullet lists.
- **No `Co-Authored-By` trailer. No "Generated with…" footer. Ever.**
- No em dashes in commit or PR text — use commas or rewrite.
- Stage files by explicit path. **Never `git add -A` or `git add .`** — `git status` first.
- Keep the message about the change; don't narrate tests, TDD, or incidental cleanup.

## 7. Pull request conventions

- For the phased build, open the PR and merge it once CI is green, without waiting. Outside that, draft it and show it first.
- PR body is human prose. No Test plan, no checklists, no "Notes for reviewers", no file:line citations.
- Feature PRs use `Closes #N`. Bug issues use `Refs #N` and stay open until the release ships — except security issues, which close when the fix merges.
- Comment style: casual plain prose, no markdown, no bullets.
- After pushing, return — don't sit waiting on CI.

---

## 8. Scope guards

- **Do not resurrect upstream.** When merging or referencing upstream Lerd code, anything in §3.1's deleted list stays deleted, and upstream's `.test`/mkcert/worktree assumptions stay out. Cherry-pick upstream security fixes narrowly rather than rebasing wholesale.
- **Dashboard clutter is a hard line.** No empty cards; hide empty widgets or fold them into a related card.
- **Destructive actions need friction.** Anything that deletes data (site removal, database drop, backup pruning) gets a typed-confirmation modal and an audit entry. The file manager always shows its live-site warning.
- **Don't flip a default** without user demand behind it.
- Treat "can we do X?" as a question ("is X needed?"), not an instruction to build X. Answer first.
- The deferred list (PRD §10) is deferred on purpose: staging, site import, atomic releases, per-site Linux users, Prometheus, multi-server. Do not start any of it unprompted.
