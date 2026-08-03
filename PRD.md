# PRD — Servlo: Production PHP Server Panel

**Status:** v1.1 — decisions and name locked
**Date:** 2026-08-02
**Base:** fork of [lerd-env/lerd](https://github.com/lerd-env/lerd) — MIT, Go 80% / Svelte 10% / TS 8%
**Licence:** MIT, free to use, no paid tier, no telemetry
**Target host:** Ubuntu 24.04 LTS, primarily DigitalOcean droplets

---

## 0. Name and identity

**Servlo.** An invented word from "server" — six letters, two syllables, easy to type daily (`servlo deploy`, `servlo ssl`), and verified clear of collisions in the hosting, deployment and PHP tooling space at the time of naming. It deliberately does not rhyme with Herd or Lerd; Servlo is a production panel, not a third local-dev variation.

Concrete identity strings, used consistently everywhere:

| Thing | Value |
|---|---|
| Binary / CLI | `servlo` |
| systemd unit prefix | `servlo-` (e.g. `servlo-nginx`, `servlo-php84-fpm`, `servlo-queue-<site>`) |
| Config directory | `~/.config/servlo/` |
| Data directory | `~/.local/share/servlo/` |
| Panel service | `servlo-panel` |
| Watcher service | `servlo-watcher` |
| Repository | `realrashid/servlo`, private, single operator |
| Go module path | `github.com/realrashid/servlo` (currently `github.com/geodro/lerd`; changed in S0.1) |
| Stores | `stores/frameworks/`, `stores/services/`, `stores/apps/` inside this repository |

Everything above is settled and safe to write into code.

**Not settled, and not to be referenced anywhere until it exists.** The `servlo` GitHub organisation is unregistered, and so are `servlo.sh` and `servlo.com`. They remain the preferred destination, and if the organisation is registered later the module path and store locations move with it, but until then no constant, fetch URL, unit file, installer string or documentation page may name them. Write `realrashid/servlo` instead.

The upstream MIT copyright notice is retained in `LICENSE`; the README states plainly that Servlo is a fork of Lerd and links upstream.

### Inherited from upstream, deliberately not yet replaced

Two upstream dependencies survive the fork on purpose, and both are tracked as debt rather than architecture.

The PHP container images come from `lerd-env/lerd-php` on GHCR, referenced from ten places in Go. They are public and MIT, Servlo consumes them unchanged through Phases 0 and 1, and they are mirrored into an owned namespace before v1 ships.

The framework and service stores are still fetched from the public `lerd-env/frameworks` and `lerd-env/services`. Servlo's own stores are authored in this repository under `stores/`, but a private repository cannot serve `raw.githubusercontent.com` fetches to an installed binary without a token, so the runtime fetch cannot move to them until either a public store mirror exists or this repository becomes public. S0.8 owns that decision.

---

## 1. The problem, from the operator

> We migrated sites from cPanel to DigitalOcean. One site needs PHP 8.1, another 8.2, another 8.3. I installed every version by hand, wrote every nginx file by hand, ran certbot for every domain by hand, and edited `php.ini` with `sed` every time a site needed a bigger upload limit. Then we deploy another server and I do the whole thing again.

Each new server is a day of manual work, repeated, where a single typo in an nginx file takes a client's site down. Nothing about that day is interesting or unique — it is the same work every time.

**Servlo turns that day into fifteen minutes and a browser tab.**

---

## 2. Why fork Lerd rather than build from scratch

Lerd is a local PHP development environment, but the engine underneath is already doing most of the job: it installs and runs multiple PHP versions side by side, generates and validates nginx vhosts per site, runs databases and Redis as one-click services, supervises queue and schedule workers with automatic recovery, issues and renews certificates, streams live logs, and ships a working dashboard. It is MIT licensed, written in Go, and runs rootless with no Docker daemon and no root process.

It points all of that at `.test` domains on a laptop. The work is not building the engine — it is aiming it at real domains, real certificates and a real server, then adding the hosting features a development tool never needed.

**Two architectural properties of Lerd are preserved deliberately, because they shape everything below:**

1. **Store-first.** Frameworks and services are versioned YAML files, not Go code. A new framework or service is a YAML change with no release.
2. **Framework-agnostic.** No Go code is allowed to know the name "Laravel". Behaviour comes from the YAML.

These are why the app installer, the deploy templates and the service catalogue are cheap to extend later. Fight them and the fork becomes expensive; work with them and most new capability is data, not code.

---

## 3. Locked architectural decisions

| Area | Decision | Reasoning |
|---|---|---|
| **Scope** | One server, many sites, one operator plus a small team | Not multi-tenant hosting. No per-client Linux users, no root broker daemon, no edge proxy. This is the decision that keeps the project a months-long build instead of a year-long one. |
| **Ports 80/443** | `net.ipv4.ip_unprivileged_port_start=0` sysctl at install; nftables DNAT 80→8080 / 443→8443 as fallback | Lets Servlo's rootless nginx container bind real ports directly. No root daemon, no second nginx, no extra hop. Safe here because there are no untrusted users on the box. |
| **Root** | No permanent root process. One sysctl or one firewall rule applied at install | Sudo commands are printed for the operator to run; Servlo never invokes sudo itself (upstream convention, kept). |
| **Certificates** | Let's Encrypt via HTTP-01, DNS-01 for wildcards | mkcert is removed. Lerd's renewal scanner, expiry warnings and nginx reload are issuer-agnostic once the issuer sits behind an interface. |
| **DNS** | None. Sites use real FQDNs; the host resolver is never touched | dnsmasq, the resolver hooks, the sudoers rule and `.localhost` mode are all deleted. |
| **Deploy** | `git pull` in place plus a per-site deploy script. No `releases/` directories | Rollback is "check out the previous commit and redeploy" — 90% of the value, none of the disk cost. Database changes are covered by the pre-deploy backup instead. |
| **Databases** | A database is a *connection*, not necessarily a container | Local MySQL/MariaDB/PostgreSQL, or an external managed database (DigitalOcean Managed). Both are first-class. |
| **Email** | External SMTP only. No mail server | Running postfix/dovecot with DKIM, SPF and deliverability is a whole product, and a bad one to get wrong. |
| **SSH** | Password authentication stays enabled; fail2ban protects it; Servlo manages keys additively | Servlo will never disable password login on its own. |
| **Platform** | Ubuntu 24.04 LTS only | 22.04 ships Podman 3.4.4, below the 4.5 minimum; the installer refuses and prints the upgrade path. Debian, Fedora, Arch, macOS and WSL2 are dropped. |
| **Panel access** | `https://<droplet-ip>:<port>` with a self-signed certificate at first boot, then attach a subdomain with a real certificate | The IP path is the bootstrap; the subdomain is the destination. |

---

## 4. Inherited, deleted, changed

### 4.1 Kept from Lerd, unchanged — this is the bulk of the product

Multiple PHP versions side by side (8.1–8.5, plus a frozen 7.4/8.0 legacy tier) · rootless Podman with systemd units, no Docker daemon, no root · per-site nginx vhost generation with `nginx -t` validation, timestamped backups and one-click restore · one-click services from a YAML preset store · queue, schedule, Horizon and custom worker supervision with failure detection and one-click heal · live logs for nginx, PHP-FPM, workers and application log files with level colouring and search · site health checks (env drift, application key, composer and npm lockfile state, `composer audit`, `npm audit`, database presence, PHP version range) · request timing analytics with p50/p95, throughput, error rate and slowest routes · per-site Node.js versions · the Svelte dashboard with its live WebSocket state channel, command palette and resource widgets · the YAML store architecture

### 4.2 Deleted from Servlo

The MCP server · the in-browser Tinker PHP REPL · the container shell drop-in · the SPX profiler · the `dump()`/`dd()` bridge, **including its `auto_prepend` mount in every generated PHP-FPM unit** · Xdebug toggles · `.test` domains, the dnsmasq container, host resolver mutation and the sudoers rule · mkcert · git worktrees · idle-suspend · LAN sharing and tunnel sharing · Mailpit · the system tray · all macOS and WSL2 code paths · inline service definitions loaded from a repository's project config file

Each of these is either a code-execution path, an information-disclosure path, or local-development machinery with no production meaning. They are **deleted rather than disabled**, because a disabled feature is one config mistake away from being enabled. A CI surface scan fails the build if any of them reappears.

### 4.3 Changed

Certificates move from mkcert to ACME · nginx binds real 80/443 · sites are added from a ZIP, a GitHub repository, an existing folder or an app installer rather than by pointing at a local directory · databases become connections that may be local or managed · PHP defaults become production defaults · authentication becomes a real login with roles · the framework and service stores move into this repository under `stores/` rather than being fetched from a third party

---

## 5. Feature specification

### 5.1 Install and server setup

One command on a fresh Ubuntu 24.04 droplet. It verifies the Ubuntu version, Podman ≥ 4.5, crun, cgroup v2 and systemd linger, and **refuses rather than half-installing** if anything is unmet.

It then sets up the server basics automatically: a swap file sized to the droplet (which also prevents npm builds from triggering the OOM killer), the system timezone, unattended security updates, and fail2ban configured for SSH. **SSH password authentication is left enabled.** Servlo manages authorised keys additively for team members but never removes password login.

It resolves the port strategy, records the choice in config, and prints any sudo command for the operator to run rather than executing it.

### 5.2 Adding a site

Four ways to create a site, all from the dashboard:

- **Clone from GitHub.** Servlo generates an SSH deploy key, shows it for pasting into the repository's deploy keys, verifies the connection, then clones. This removes the most common friction point in server deployment.
- **Upload a ZIP.** Uploaded, extracted, document root detected.
- **Point at an existing folder** already on the server.
- **Install an app.** A fresh WordPress in one click, with the database created, `wp-config.php` written and the admin account set up. WordPress ships at launch; further apps are YAML files in the `stores/apps/` store, needing no code release.

In every case Servlo asks for the domain, PHP version and framework (auto-detected), creates the directory, generates the vhost and registers the site. The **Get SSL** button appears immediately with its DNS check already running.

### 5.3 Certificates — the Get SSL flow

The button starts disabled with a live DNS check beside it. It resolves A and AAAA records for the primary domain **and every alias**, and compares them against the droplet's public addresses. Until they match it displays the actual mismatch:

> *Waiting for DNS — `example.com` currently resolves to 1.2.3.4, this server is 5.6.7.8*

Once every domain resolves here the button enables. One click issues the certificate covering all domains, installs it, rewrites the vhost to listen on 443, adds HSTS and a 301 redirect from HTTP, and updates `APP_URL`. DNS-01 with Cloudflare, Route53 and DigitalOcean providers handles wildcards. Renewal is automatic at 30 days; **failure is loud** — a dashboard banner, an audit entry and an email once panel SMTP is configured. A site never silently serves an expired certificate.

### 5.4 Domains

Each site has a primary domain plus any number of aliases. www-to-non-www redirection is a toggle. Subdomains can be their own independent sites. Redirects come in both forms: whole-domain (`oldcompany.com` → `newcompany.com`) and URL-level (`/about-us` → `/about`), the latter mattering specifically because sites migrated off cPanel have inbound links and search results pointing at URLs that may have moved.

All alias domains are included in the certificate's SANs and in the DNS pre-flight check.

### 5.5 Per-site PHP settings

Each site gets **its own PHP-FPM pool**, so PHP settings are per-site rather than shared across every site on that PHP version. This replaces editing `php.ini` with `sed`.

**Settings that span two systems are exposed as one field.** This is the part that a raw config editor never solves:

| Panel field | Writes to |
|---|---|
| **Max upload size** | PHP `upload_max_filesize`, PHP `post_max_size`, nginx `client_max_body_size` |
| **Max execution time** | PHP `max_execution_time`, nginx `fastcgi_read_timeout` and `fastcgi_send_timeout` |
| **Memory limit** | PHP `memory_limit` |

Setting only the PHP half of the first gives a 413 error with no useful message; setting only the PHP half of the second gives a 504 while PHP is still working happily. Binding them together removes an entire category of debugging.

Production defaults apply on every site: `display_errors=Off`, `expose_php=Off`, OPcache enabled with `validate_timestamps=0`.

### 5.6 Per-site nginx settings

Common needs are form fields — upload size, timeouts, custom headers, redirects, static-file caching. An advanced raw editor sits underneath for anything unusual. Every save runs `nginx -t` before committing and keeps a timestamped backup with one-click restore, so a bad edit cannot take a site down.

### 5.7 Deploy

Each site has an editable **deploy script**, pre-filled from a template chosen by the detected framework:

- **Laravel:** `composer install --no-dev --optimize-autoloader`, `npm ci && npm run build`, `php artisan migrate --force`, `config:cache`, `route:cache`, `view:cache`, `queue:restart`
- **WordPress:** empty by default
- **Plain PHP:** empty by default

Deploying runs `git pull` followed by the script, then reloads PHP-FPM gracefully. Output streams live into the panel so a failure is visible rather than mysterious. A database backup runs automatically before any deploy whose script contains a migration.

**WordPress deploys use a per-site exclude list.** A WP deploy is never a clean checkout — `wp-content/uploads` and `wp-content/plugins` are excluded by default, because the client installs plugins and uploads media through WP admin and the first deploy after that would otherwise destroy their site.

**Redeploy previous commit** is a one-click button: check out the prior commit and re-run the script. It does not undo database migrations — the pre-deploy backup covers that.

### 5.8 Node.js and asset builds

Node version per site. `npm run build` runs on the server.

**Builds run inside their own memory-capped scope.** A Vite or webpack build can spike past 1 GB, and on a small droplet the OOM killer may not kill the build — it may kill MySQL, taking every site down. Capping the build's scope means an over-large build fails alone and visibly. Combined with the swap file created at install, this closes a failure mode that is very hard to diagnose after the fact.

### 5.9 Databases

A database is a **connection**. At install and again per site, the choice is: install MySQL/MariaDB locally, install PostgreSQL locally, or connect to an external managed database.

For a managed database Servlo stores host, port, credentials and CA certificate, tests the connection, creates the database and a least-privilege user on the remote server, and writes the values into `.env`. **It displays the droplet's public IP for pasting into DigitalOcean's trusted sources** — the step that otherwise costs an hour of confused debugging.

phpMyAdmin is installed automatically alongside MySQL or MariaDB, pgAdmin alongside PostgreSQL, each pointed at whichever connection the site uses, local or remote. Each site gets its own database user with grants scoped to its own schema.

### 5.10 Services

Redis, Meilisearch, OpenSearch, Beanstalkd and the rest of the preset store, installed and started from the dashboard, with connection values injected into the site `.env` automatically. Inherited from Lerd; production hardening means services bind to the container network only and never publish to the public interface, with strong generated passwords.

**Servlo's default stack is mysql, postgres, redis, meilisearch and rustfs.** Upstream ships Mailpit in that set; Servlo does not, because Mailpit is deleted (§4.2) and there is no mail server in this product at all. Everything outside the default stack is a preset in the service store rather than something the binary carries.

### 5.11 Workers and cron

Queue, schedule, Horizon and custom workers are inherited, with `restart: always` in production and idle-suspend removed.

**A cron UI** lets any site add scheduled tasks — command, schedule, output capture and last-run status. It is built on systemd timers, which the inherited engine already supports for timer-based workers, so the plumbing exists and the UI sits on top.

For WordPress, Servlo disables WP's built-in pseudo-cron (which only fires when a visitor happens to load a page) and runs a real one-minute cron instead. That is a genuine improvement over the cPanel setup, not just parity.

### 5.12 Email

No mail server. Each site has a **Mail** tab where SMTP credentials are entered; Servlo writes `MAIL_*` into `.env` or the equivalent into `wp-config.php`, and a **Send test email** button confirms it works before a client discovers it doesn't.

The panel has its own separate SMTP settings, used for alerts.

### 5.13 File access

**SFTP per site** — an account locked to that site's directory, so FileZilla works exactly as it did on cPanel.

**A file manager in the panel** — browse, edit, upload, unzip, and fix permissions. It carries an explicit "you are editing a live site" warning and every change is written to the audit log, because this is the fastest way to break a production site by accident.

### 5.14 Backups

Files and database, per site, on a schedule with a retention policy. Destinations: S3-compatible (which covers both DigitalOcean Spaces and Amazon S3 with one driver) and SFTP to another server.

**Test restore is a first-class feature, not a nice-to-have.** Backups are restored into a scratch database, verified, and torn down. A backup that has never been restored is not a backup, and most panels ship a green checkmark that tells you nothing.

Servlo's own configuration and the full site registry are included, which is what makes the next feature possible.

### 5.15 Server rebuild

Restore an entire server onto a fresh droplet: install Servlo, restore, point DNS at the new IP. Every site, setting, cron entry and database returns.

Two honest caveats surfaced in the UI: SSL certificates are **reissued** rather than restored (Let's Encrypt is bound to the domain and DNS must point at the new server first), and managed-database users must add the new droplet IP to trusted sources.

This is the feature that answers "we deploy another server and I do the whole thing again."

### 5.16 Staging

A staging copy per site at its own subdomain, with its own database and its own certificate, carrying `noindex` headers and an HTTP password prompt so neither Google nor a client stumbles onto it.

**Refresh from live** copies live files and database across on demand — without it, staging drifts within a month and stops being trusted, which is how staging environments die.

### 5.17 Firewall and intrusion protection

Servlo manages ufw — open and close ports from the dashboard — and **also shows whether DigitalOcean's cloud firewall is filtering**, because two firewall layers is how people spend an hour debugging a blocked port at the wrong layer.

fail2ban is configured at install and protects SSH. Since password authentication stays enabled, this is the primary defence against the brute-force attempts that begin within minutes of a droplet going live.

### 5.18 Users, roles and audit

Two roles:

- **Admin** — PHP versions, services, firewall, server settings, user management, everything
- **Developer** — deploy, logs, files, cron and settings for assigned sites only

Authentication is Argon2id hashing, session cookies (`HttpOnly`, `Secure`, `SameSite=Strict`), CSRF tokens on every state-changing route, per-IP rate limiting with progressive lockout, optional TOTP with recovery codes, and revocable sessions.

Every state-changing action is written to an append-only audit log: who, from where, what, and the result. With a team, this is how you find out at 2am who deployed what.

### 5.19 Alerts and monitoring

CPU, memory and disk history, per-site status, worker state and certificate expiry, all in the dashboard (largely inherited).

Alerts appear in the panel for: site down, certificate renewal failed, backup failed, deploy failed, worker down, disk filling. Once panel SMTP is configured they are also emailed — which matters, because a panel alert only works if someone has the tab open.

---

## 6. Known tradeoffs, accepted deliberately

**All sites run as the same Linux user.** A compromised site — an outdated WordPress plugin, a vulnerable dependency — can read every other site's `.env` on that server, and therefore every other site's database credentials.

This is the same model most single-operator panels ship, and it is a reasonable trade for your own and your clients' sites on your own droplet. It is worth knowing rather than discovering. Two cheap mitigations are included: **per-site database users with schema-scoped grants**, which limits what a leaked `.env` is worth, and the advice not to co-locate a site you do not control with a site that matters. A per-site FPM pool running as its own Linux user is a possible later hardening, but it is not in v1.

**No atomic releases.** A deploy modifies the live directory. There is a brief window during `composer install` where the site is in a mixed state. Mitigated by the pre-deploy database backup, the redeploy-previous-commit button, and staging.

**Log volume and disk.** A production server that never rotates logs fills its disk and takes every site down. Log rotation with configurable retention is v1, not optional, and disk usage is an alert category.

---

## 7. Non-functional requirements

- **Security.** No permanent root process. Dangerous capabilities default-deny. A CI surface scan fails the build if any deleted dev feature reappears or any API route lacks a declared permission.
- **Supply chain.** The framework, service and app stores are authored in this repository under `stores/`, versions pinned, manifests signed and verified before use. Upstream fetches YAML that runs commands and container images; on a production server that must not be a third party's decision. The runtime fetch still points at the public upstream stores until the private-repository fetch problem in §0 is resolved, which is the single largest outstanding gap in this requirement and is owned by S0.8.
- **Reliability.** Everything survives reboot through enabled systemd units plus linger. A CI job reboots a VM and asserts every site, service and worker returns unaided.
- **Footprint.** A 2 GB droplet should comfortably run five to ten small PHP sites across one or two PHP versions with MySQL and Redis. The sizing model is published, and Servlo warns before a heavy asset build on a small server.
- **Performance.** Deploy overhead under 10 seconds excluding `composer install` and the asset build. The panel adds nothing to the request path.
- **Licensing.** MIT retained, upstream copyright notice kept, the fork relationship stated plainly in the README. Free, no paid tier, no telemetry.

---

## 8. Risks

| Risk | Impact | Mitigation |
|---|---|---|
| One compromised site reads every other site's credentials | High | Documented in §6; per-site database users in v1; per-site FPM pool as later hardening |
| Upstream's API is effectively remote code execution by design (REPL, shell, arbitrary container install) | High | Deleted, not gated; CI surface scan fails the build if any returns |
| Third-party stores execute code and container images on production servers | High | Stores authored in this repository under `stores/`, pinning, signature verification, reviewed promotion |
| The file manager makes it easy to break a live site | Medium | Explicit live-site warning, full audit trail, permission-gated |
| Certificate renewal fails silently and a client site goes dark | Medium | Loud failure by design: banner, audit entry, email; never serve expired |
| An asset build OOM-kills MySQL on a small droplet | Medium | Swap at install, memory-capped build scope, pre-build warning on small servers |
| Disk fills from unrotated logs or accumulated backups | Medium | Log rotation and backup retention both in v1; disk alerting in v1 |
| SSH password authentication stays enabled | Medium | Accepted deliberately; fail2ban configured at install; Servlo manages keys additively so the operator can migrate whenever they choose |
| Fork drift makes upstream security fixes hard to pull | Low | Additive changes, new packages rather than edits, track upstream advisories specifically |

---

## 9. Success metrics

- Fresh DigitalOcean droplet to a live HTTPS Laravel site in **under 15 minutes**, dashboard only, no terminal after install.
- A site's max upload size changed correctly — PHP and nginx together — in **one field, one click**.
- **Get SSL is one click**, and never fires against a domain that does not yet point at the server.
- A dead droplet rebuilt from backup onto a fresh one, with every site live, in **under an hour**.
- Every backup verified by an automated test restore.
- Zero deleted dev features present in the shipped binary, proven by CI.
- Reboot recovery with no human action.

---

## 10. Deferred

**v1.1:** staging sites · importing an existing live site (files plus a `.sql` dump) · email alerts once panel SMTP is set · additional apps in the installer store beyond WordPress

**Later, driven by real use:** atomic releases with true rollback · per-site Linux user isolation · Prometheus metrics endpoint · managing more than one server from a single panel
