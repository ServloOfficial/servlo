# Changelog

## Unreleased

### Servlo starts here

Servlo is a fork of [Lerd](https://github.com/lerd-env/lerd) and inherits an
engine, but it is a different product aimed at a different machine, so it does
not inherit the upstream release history. Those entries described releases of a
local development tool for Linux and macOS; restating them under the Servlo
name would claim releases Servlo never made. The upstream history remains in
this repository's git history, and the fork and its link are stated in
`README.md`.

This entry describes the whole of the first release rather than a delta,
because there is nothing before it to differ from.

**Platform.** Ubuntu 24.04 LTS only, on rootless Podman and systemd, with no
Docker daemon and no permanent root process. Ports 80 and 443 come from the
`net.ipv4.ip_unprivileged_port_start` sysctl, with an nftables redirect as the
fallback, applied once at install and checked by `servlo doctor` after. The
installer refuses any other distribution rather than half-installing, and
nothing in the product ever calls `sudo` for you: a step needing privilege
prints the command for you to run.

**Sites.** Added from a folder you already have, a ZIP, a Git clone, or one
click for WordPress, Joomla and Grav. Every site gets its own PHP-FPM pool, so
PHP settings are per site rather than per version, and PHP 7.4 through 8.5 can
run side by side. Domain aliases, a www redirect in either direction,
subdomains as first-class sites, redirects, staging copies that are
password-protected and noindexed, and an importer for a site that is live
somewhere else right now.

**Certificates.** Let's Encrypt over HTTP-01, or DNS-01 for wildcards through
Cloudflare, Route53 and DigitalOcean, behind an issuer interface. **Get SSL**
stays disabled until a live lookup shows every domain, primary and alias,
resolving to this server, and it shows the mismatch while you wait. Renewal is
scanned on a timer and a failure is loud: a dashboard banner, an audit entry
and an email where panel SMTP is configured. No site silently serves an expired
certificate.

**Deploys.** `git pull` in place plus a per-site script, pre-filled from the
framework store and editable. A database backup is taken automatically before
any deploy whose script migrates. WordPress deploys honour a per-site exclude
list so a client's uploads and plugins survive. Deploy history, redeploy of the
previous commit, signed Git webhooks, and asset builds run in a memory-capped
scope so an oversized `npm run build` fails alone instead of taking MySQL down
with it.

**Databases.** A database is a connection, which may be a local container or an
external managed database with its CA certificate, and both are first-class
everywhere. Each site gets its own database and a least-privilege user scoped
to its own schema. Managed-database flows surface the machine's public IP for
the provider's trusted-sources list.

**Backups.** Encrypted, scheduled, on daily/weekly/monthly retention, pushed to
S3-compatible storage or SFTP, and restored into a scratch database on a
schedule to prove they still work. Plus a state archive that turns a fresh
machine back into this one.

**Operations.** An alerts list covering the handful of things that actually go
wrong, per-site uptime checks, log rotation with configurable retention, a
hardening audit, ufw and a cloud-firewall check, fail2ban, additive SSH key
management, a file manager and per-site SFTP. SSH password authentication is
never disabled.

**The panel.** Argon2id, session cookies that are `HttpOnly`, `Secure` and
`SameSite=Strict`, CSRF on every state-changing route, per-IP rate limiting
with lockout, optional TOTP, and Admin and Developer roles enforced on every
route and every WebSocket message. Every state-changing action lands in an
append-only audit log. Secrets are redacted in logs, deploy output and API
responses.

**Behaviour lives in YAML.** Frameworks, service presets and one-click apps are
versioned definitions in `stores/`, embedded in the binary and refreshed over
the network. Adding a framework, a service or an app is a store change, not a
release.

### Not inherited, on purpose

The features that only make sense on a laptop, and the handful that amount to
remote code execution on a public machine, were deleted rather than disabled:
the MCP server, the in-browser PHP REPL, the container shell, the profiler, the
`dump()` bridge, Xdebug toggles, browser editing of `php.ini`, `.test` domains
and all host resolver mutation, mkcert, git worktrees, idle-suspend, LAN and
tunnel sharing, Mailpit, the system tray, and the macOS and WSL2 code paths. A
scan runs in the test suite and fails the build if any of them reappears,
including as store data or in a comment.

### Known limits

Every site runs as the same Linux user. A compromised site can read another
site's `.env` on the same machine; per-site database users with scoped grants
limit the damage but do not remove it. This is a deliberate tradeoff, not an
oversight, and it is why Servlo is not multi-tenant hosting.

There is no mail server and never will be. SMTP settings per site and for the
panel are the whole email story.

Atomic releases with true rollback, per-site Linux users, Prometheus metrics
and managing more than one server from one panel are all deferred on purpose.
Deploy is `git pull` in place plus a script, and redeploying the previous
commit re-runs that script against the older tree; it does not revert
migrations, and nothing here pretends otherwise.
