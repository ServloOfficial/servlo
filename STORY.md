# STORY.md — Servlo Epics & User Stories

Companion to `PRD.md` v1.1. Sizes: **S** ≤ 1 day, **M** ≤ 3 days, **L** ≤ 1 week, **XL** > 1 week.

**Standing rules, inherited from the upstream codebase and kept:**

- Write the failing test first. A change without test coverage does not merge.
- Behaviour belongs in store YAML, not Go. If you are branching on a framework name in Go, the logic is in the wrong layer.
- Environment variables belong to sites, never to workers.
- Run the full local gate before every commit: build, `go test ./...`, `go vet ./...`, `gofmt -l` empty, UI tests if the UI changed, installer bats tests if the installer changed.
- Then install the local build on a real Ubuntu 24.04 droplet and drive it by hand. Tests do not catch runtime-surface bugs.

**Standing CI gate — the surface scan.** Enumerates the built binary's symbols and every API route. Fails the build if any deleted development feature reappears, or if any state-changing route lacks a declared permission. This runs from Phase 1 onward and never goes away.

---

# PHASE 0 — Fork and strip
*Goal: a clean, minimal, Ubuntu-only Servlo binary with nothing dangerous left in it.*

**S0.1 — Rename to Servlo.**
Binary `servlo`; systemd unit prefix `servlo-` (`servlo-nginx`, `servlo-php84-fpm`, `servlo-panel`, `servlo-watcher`, `servlo-queue-<site>`); config at `~/.config/servlo/`; data at `~/.local/share/servlo/`; the Go module path becomes `github.com/realrashid/servlo`; the stale root `mkdocs.yml` is deleted, the docs site is VitePress under `docs/`.
*Done when:* no "lerd" string appears in user-facing output, unit names, paths or docs; the upstream MIT copyright notice is retained in `LICENSE`; the README states the fork relationship and links upstream. **M**

**S0.2 — Server-only build.**
*Done when:* `make build-server` produces a CGO-free `servlo` binary; `cmd/lerd-tray` is **deleted outright**, along with desktop notifications and every macOS and WSL2 code path, and the `nogui` build tag goes with them; amd64 and arm64 both build.
Deleted, not build-tagged. A tray excluded by a tag is still in the tree and still one tag away from shipping, which is exactly what CLAUDE.md 3.1 forbids and what the surface scan asserts against. **S**

**S0.3 — Ubuntu-only platform gate.**
*Done when:* distro detection collapses to Ubuntu; the installer refuses other distributions, and refuses Ubuntu 22.04 when Podman is below 4.5 while printing the upgrade path rather than half-installing. **M**

**S0.4 — Delete the MCP server.**
*Done when:* the MCP package, all `mcp:*` commands and MCP documentation are removed; the surface scan asserts no MCP symbols remain. **M**

**S0.5 — Delete the code-execution surfaces.**
Tinker REPL, container shell, SPX profiler, dump bridge, Xdebug toggles, browser `php.ini` editing.
*Done when:* all are removed from Go and the Svelte UI, **including the dump bridge's `auto_prepend` mount inside every generated PHP-FPM unit** — verified by inspecting a generated unit file, not by checking a runtime flag. **L**

**S0.6 — Delete local-development site features.**
*Done when:* git worktrees, idle-suspend, LAN sharing and tunnel sharing are gone. **L**

**S0.7 — Reject inline service definitions from the project config file.**
*Done when:* a project declaring an inline service is linked without it and the operator is told why; only reviewed store presets can run a container. **S**

**S0.8 — Bring the stores in-repo, and solve serving them.**
*Done when:* `stores/frameworks/`, `stores/services/` and `stores/apps/` exist in this repository, mirroring upstream's subdir layout so `internal/origin/origin.go` needs only new base URLs; versions are pinned in config; manifest signatures are verified before use; the promotion process is documented.
**Blocked on a decision, and the story is not done until it is made.** An installed binary on a droplet fetches stores over `raw.githubusercontent.com`, which returns 404 for a private repository unless the request carries a token. So authoring the stores here does not by itself make them reachable. The two exits are a separate public store repository, or this repository becoming public. Until one is chosen, the runtime fetch stays pointed at the public `lerd-env/frameworks` and `lerd-env/services`, and that fallback must be visible in config rather than silent. **L**

**S0.9 — CI on Ubuntu 24.04.**
*Done when:* build, unit tests, installer tests and the surface scan all run on a real 24.04 VM rather than a container. **M**

---

# PHASE 1 — Real server, real domains, real certificates
*Goal: a site on a real domain with a real certificate, served by Servlo. This milestone proves the whole thesis.*

## E1 — Server foundation

**S1.1 — Installer preflight.**
*Done when:* Ubuntu version, Podman ≥ 4.5, crun, cgroup v2 and linger are all verified; anything unmet refuses cleanly with a specific message rather than half-installing. **M**

**S1.2 — Port binding.**
*Done when:* the installer applies `net.ipv4.ip_unprivileged_port_start=0`, falls back to nftables DNAT 80→8080 / 443→8443 persisted across reboot, records the choice in config, and prints the sudo command rather than running it. **M**

**S1.3 — Automatic server basics.**
*Done when:* a swap file sized to the droplet, the system timezone, unattended security updates and fail2ban for SSH are all configured at install. **SSH password authentication is left enabled and is never disabled by Servlo.** **L**

**S1.4 — Health verification of the port strategy.**
*Done when:* `servlo doctor` confirms the strategy still holds on every run and specifically after a reboot. **S**

## E2 — Real domains

**S2.1 — Remove the `.test` DNS stack.**
*Done when:* the dnsmasq container, its config, the host resolver mutation, the sudoers rule and `.localhost` mode are all gone; a test asserts no host resolver file is ever written. **L**

**S2.2 — Sites registered on real FQDNs.**
*Done when:* site creation takes a full domain; the registry, vhost, `APP_URL` and certificate SANs all use it; no TLD is ever appended. **M**

## E3 — Certificates

**S3.1 — Extract a certificate-issuer interface.**
*Done when:* mkcert is removed and the interface is in place; the inherited 30-day reissue scanner, expiry warnings and nginx reload all operate through it without modification. **M**

**S3.2 — HTTP-01 issuance from Let's Encrypt.**
*Done when:* the challenge is served by the existing nginx container; the key is written 0600; a staging flag targets the ACME staging endpoint; the certificate installs and the vhost regenerates. **L**

**S3.3 — The Get SSL button.**
*Done when:* the site panel shows a live DNS check resolving A and AAAA for the primary domain **and every alias**, compared against the droplet's public addresses. While unmatched the button is disabled and displays the actual mismatch — *"Waiting for DNS — example.com currently resolves to 1.2.3.4, this server is 5.6.7.8"*. Once matched, one click issues a certificate covering all domains, installs it, rewrites the vhost to 443, adds HSTS and a 301 redirect, and updates `APP_URL`. Progress and any ACME error stream into the panel. **L**

**S3.4 — DNS-01 with Cloudflare, Route53 and DigitalOcean.**
*Done when:* wildcards issue successfully; provider credentials are stored 0600 outside any site directory; covered by a staging integration test. **L**

**S3.5 — Renewal failure is loud.**
*Done when:* a failed renewal produces a dashboard banner and an audit entry (and an email once panel SMTP exists). A site never silently serves an expired certificate. **M**

**S3.6 — Secure vhost defaults.**
*Done when:* TLS 1.2+, modern ciphers, HSTS with configurable max-age, OCSP stapling and a 301 HTTP-to-HTTPS redirect are generated by default. **M**

---

# PHASE 2 — Production mode and the login
*Goal: Servlo can be safely exposed to the internet.*

## E4 — Production defaults

**S4.1 — Production mode switch.**
*Done when:* a mode flag exists in config; enabling is confirmed; disabling requires a force flag; the mode is shown prominently in the dashboard. **M**

**S4.2 — Production defaults across the stack.**
*Done when:* idle-suspend off; worker restart policy `always`; PHP defaults `display_errors=Off`, `expose_php=Off`, OPcache on with `validate_timestamps=0`. **M**

**S4.3 — Service hardening.**
*Done when:* database and cache services bind to the container network only and never publish to a public interface; passwords are generated strong; a test asserts no service listens on a public address. **M**

## E5 — Authentication

**S5.1 — Panel access, both paths.**
*Done when:* at first boot the panel is reachable at `https://<ip>:<port>` with a self-signed certificate; a subdomain can then be attached and receives a real certificate through the normal Get SSL flow. **L**

**S5.2 — Session authentication.**
*Done when:* Argon2id hashing; cookies `HttpOnly`, `Secure`, `SameSite=Strict`; CSRF on every state-changing route; per-IP rate limiting with progressive lockout; sessions listable and revocable from the CLI. **L**

**S5.3 — Optional TOTP.**
*Done when:* QR enrolment, one-time recovery codes, and a CLI reset path for lockout recovery all work. **M**

**S5.4 — Roles.**
*Done when:* Admin sees everything; Developer sees deploy, logs, files, cron and settings for assigned sites only. Enforced on every route and in every WebSocket message. **L**

**S5.5 — Permission registry.**
*Done when:* every state-changing route declares a permission and the surface scan fails the build on any undeclared route. **L**

**S5.6 — Audit log.**
*Done when:* append-only, 0600, rotated, never truncated by the app; records actor, source IP, action, target, result and timestamp; visible in the dashboard. **M**

**S5.7 — Strip dev-only UI surfaces.**
*Done when:* no Tinker tab, terminal button, profiler view, dump viewer, Xdebug toggle, per-version `php.ini` editor or worktree strip remains anywhere in the Svelte app. **L**

**S5.8 — `SECURITY.md`.**
*Done when:* the threat model — including the shared-Linux-user tradeoff from PRD §6 — and a disclosure process are written down. **M**

---

# PHASE 3 — Site management
*Goal: everything an operator does day to day, from the browser.*

## E6 — Adding sites

**S6.1 — Add site: existing folder.**
*Done when:* domain, PHP version, auto-detected framework and document root are captured; the directory is created, the vhost generated, the site registered; the Get SSL button appears with its DNS check running. **L**

**S6.2 — Add site: clone from GitHub.**
*Done when:* Servlo generates an SSH deploy key, displays it for pasting into the repository, verifies the connection with a test, then clones. Connection failure gives a specific reason, not a generic error. **L**

**S6.3 — Add site: upload a ZIP.**
*Done when:* the archive uploads, extracts, and the document root is detected. **M**

**S6.4 — Add site: app installer.**
*Done when:* a fresh WordPress installs in one click — database created, `wp-config.php` written, admin account set up. The app is defined as **store YAML with no Go code specific to it**, so further apps need no release. **L**

## E7 — Site settings

**S7.1 — Per-site PHP-FPM pool.**
*Done when:* each site has its own pool, so PHP settings apply per site rather than per PHP version. **XL**

**S7.2 — Combined settings fields.**
*Done when:* **Max upload size** writes PHP `upload_max_filesize`, PHP `post_max_size` and nginx `client_max_body_size` together; **Max execution time** writes PHP `max_execution_time` and nginx `fastcgi_read_timeout`/`fastcgi_send_timeout` together; **Memory limit** writes `memory_limit`. A test uploads a file larger than the old limit and asserts it succeeds after one field change. **L**

**S7.3 — Per-site PHP version switching.**
*Done when:* changing the version regenerates the vhost and pool and reloads without downtime. Inherited; verify against per-site pools. **M**

**S7.4 — Nginx settings, friendly and raw.**
*Done when:* common needs are form fields (upload size, timeouts, headers, static caching); a raw editor sits underneath; every save runs `nginx -t` first and keeps a timestamped backup with one-click restore. **L**

## E8 — Domains

**S8.1 — Aliases.** Multiple domains on one site, included in certificate SANs and the DNS pre-flight. **M**
**S8.2 — www-to-non-www** as a toggle, in either direction. **S**
**S8.3 — Subdomains as independent sites.** **M**
**S8.4 — Redirects,** whole-domain and URL-level, managed from the panel. **M**

## E9 — Deploy

**S9.1 — Production profiles in the framework store.**
*Done when:* Laravel, WordPress and plain PHP templates exist as store YAML declaring build commands, migration command, exclude list and health path. **No framework name appears in Go.** **L**

**S9.2 — Editable deploy script per site,** pre-filled from the framework template. **M**

**S9.3 — Deploy.**
*Done when:* `git pull`, then the deploy script, then a graceful PHP-FPM reload; output streams live into the panel; a database backup runs automatically first when the script contains a migration. **L**

**S9.4 — WordPress exclude list.**
*Done when:* `wp-content/uploads` and `wp-content/plugins` are excluded by default and the list is editable; a test installs a plugin, deploys, and asserts the plugin survives. **M**

**S9.5 — Redeploy previous commit.**
*Done when:* one click checks out the prior commit and re-runs the script; the UI states plainly that database migrations are not undone. **M**

**S9.6 — Deploy history:** commit SHA, author, duration, outcome, who triggered it. **M**

**S9.7 — Git webhook deploy:** signed, per-site secret, branch filter, replay protection, off by default. **L**

## E10 — Node and asset builds

**S10.1 — Per-site Node version.** Inherited; verify against production paths. **M**
**S10.2 — Memory-capped build scope.**
*Done when:* asset builds run in their own scope with a memory cap, so an oversized build fails alone rather than triggering the OOM killer against MySQL. A test runs a deliberately oversized build and asserts other services survive. **L**
**S10.3 — Pre-build warning** when the droplet is small relative to the build. **S**

## E11 — Databases

**S11.1 — Database as a connection.**
*Done when:* install-time and per-site choice between local MySQL/MariaDB, local PostgreSQL, and an external managed database. **L**

**S11.2 — Managed database support.**
*Done when:* host, port, credentials and CA certificate are stored; the connection is tested; the database and a least-privilege user are created remotely; values are written into `.env`; **the droplet's public IP is displayed for pasting into DigitalOcean trusted sources.** **L**

**S11.3 — Admin UIs.** phpMyAdmin auto-installed with MySQL/MariaDB, pgAdmin with PostgreSQL, each pointed at whichever connection the site uses. **M**

**S11.4 — Per-site database user** with schema-scoped grants, and rotation. **L**

## E12 — Cron

**S12.1 — Cron UI** per site: command, schedule, output capture, last-run status, built on systemd timers. **L**
**S12.2 — Real WordPress cron.** WP pseudo-cron disabled, a one-minute system cron installed in its place. **M**

## E13 — Email

**S13.1 — Per-site SMTP** written into `.env` or `wp-config.php`, with a **Send test email** button. **M**
**S13.2 — Panel SMTP** for alerts, configured separately. **S**

## E14 — File access

**S14.1 — SFTP per site,** locked to that site's directory. **L**
**S14.2 — File manager:** browse, edit, upload, unzip, fix permissions. Carries an explicit live-site warning; every change is audited; permission-gated. **XL**

---

# PHASE 4 — Operations
*Goal: the difference between "it works" and "you can sleep."*

**S15.1 — Backup a site:** files plus database, compressed and encrypted. **L**
**S15.2 — Scheduled backups** as systemd timers with daily/weekly/monthly retention. **M**
**S15.3 — Test restore:** restore into a scratch database, verify, tear down. Runs on a schedule, not just on demand. **L**
**S15.4 — Destinations:** S3-compatible (covers DigitalOcean Spaces and Amazon S3 in one driver) and SFTP. **L**
**S15.5 — Servlo state and site registry included** in backups. **M**

**S16.1 — Server rebuild.**
*Done when:* a full restore onto a fresh droplet brings back every site, setting, cron entry and database. The UI states plainly that certificates are reissued rather than restored, and that managed databases need the new IP added to trusted sources. A CI job performs a real rebuild onto a clean VM and asserts every site serves. **XL**

**S17.1 — Firewall management** (ufw) from the dashboard. **L**
**S17.2 — Cloud firewall visibility:** detect and display whether DigitalOcean's firewall is also filtering, so a blocked port is debugged at the right layer. **M**
**S17.3 — fail2ban status and unban** from the panel. **M**
**S17.4 — SSH key management:** add and remove authorised keys for team members, additively. Servlo never disables password authentication. **M**

**S18.1 — Log rotation** with configurable retention for nginx, PHP-FPM, workers and application logs. **M**
**S18.2 — Alerts in the panel:** site down, certificate renewal failed, backup failed, deploy failed, worker down, disk filling. **L**
**S18.3 — Email alerts** once panel SMTP is configured. **M**
**S18.4 — Uptime check** per site against the framework's declared health path. **M**

**S19.1 — Hardening audit:** firewall state, open ports, fail2ban, unattended-upgrades, config file modes, any site running with debug enabled. Safe fixes applied on request; anything needing sudo is printed, never executed. **L**
**S19.2 — Reboot resilience CI job:** reboot a VM, assert every site, service and worker returns unaided. **M**

---

# PHASE 5 — v1.1
*Driven by real use, not speculation.*

**S20.1 — Staging sites:** own database, own certificate, `noindex` headers and password protection. **XL**
**S20.2 — Refresh staging from live:** copy live files and database across on demand. **L**
**S20.3 — Import an existing live site:** files plus a `.sql` dump, with the vhost and settings generated. **L**
**S20.4 — More apps in the `stores/apps/` store,** each as YAML with no code release. **M**

**Later, only if real use demands it:** atomic releases with true rollback · per-site Linux user isolation · Prometheus metrics · managing more than one server from a single panel

---

## Delivery plan

| Phase | Outcome | Gate |
|---|---|---|
| **0** | Clean, stripped, Ubuntu-only Servlo binary | Surface scan clean |
| **1** | A real site on a real domain with a real certificate | Live HTTPS site on a real droplet |
| **2** | A panel safe to expose to the internet | Surface scan and permission audit clean |
| **3** | Everything an operator does day to day, from the browser | A site added, configured and deployed without touching a terminal |
| **4** | Backups, rebuild, firewall, alerts | A droplet destroyed and rebuilt from backup, every site live |
| **5** | Staging and site import | — |

Phases 0 through 4 are the shippable v1: roughly **four to five months** for one focused developer, or **two and a half to three** for two.

Phase 4 is not optional and should not be deferred. A panel that hosts client sites without verified backups is a liability, not a product.

---

## Definition of Done — every story

- Failing test written first; behaviour changes update existing tests.
- Surface scan and permission audit still green.
- Behaviour lives in store YAML wherever the design laws say it should; no framework name in Go.
- Documentation updated before the commit.
- Full local gate green: build, `go test ./...`, `go vet ./...`, `gofmt -l` empty, UI tests if the UI changed, installer tests if the installer changed.
- Installed and smoke-tested on a real Ubuntu 24.04 droplet, not only in CI.
- Conventional-commit subject, prose body, files staged by explicit path, no generated-by footers.
