<img src="docs/public/assets/logo.svg" alt="" width="64" height="64">

# Servlo

> A free, open-source production PHP server panel for Ubuntu 24.04 LTS.
> Rootless Podman, systemd units, real domains and real certificates,
> driven from a browser.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-Ubuntu%2024.04%20LTS-lightgrey)]()
[![Version](https://img.shields.io/badge/version-0.1.0%20beta-orange)]()

One operator runs many PHP sites on one Ubuntu droplet. Add a site from a ZIP, a
GitHub clone, an existing folder or a one-click app install; bind it to a real
domain; click **Get SSL** once DNS points at the server; pick a PHP version per
site; toggle services like MySQL and Redis; deploy with `git pull` plus a
per-site script; and rely on backups that are verified by real test restores.

This is the cPanel/Forge/Ploi niche, self-hosted and free. There is no paid tier
and no telemetry.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/realrashid/servlo/main/install.sh | bash
```

Ubuntu 24.04 LTS with Podman 4.5 or newer. The installer checks that first and
refuses other distributions rather than half-installing. Steps needing root are
printed for you to run; Servlo never invokes `sudo` itself.

Then open the panel, or drive the same things from the CLI:

```bash
servlo doctor                       # is this machine actually healthy
servlo apps install wordpress --domain blog.example.com
servlo secure blog.example.com      # Let's Encrypt, once DNS resolves here
servlo backup blog.example.com
servlo backup verify --latest blog.example.com
```

## What you get

**Sites** from a folder, a ZIP, a GitHub clone or a one-click app (WordPress,
Joomla, Grav). Per-site PHP version and PHP-FPM pool. Domain aliases, www
redirects, subdomains as independent sites, and URL-level redirects.

**Certificates** from Let's Encrypt over HTTP-01, DNS-01 for wildcards via
Cloudflare, Route53 or DigitalOcean. **Get SSL** stays disabled until a live
check shows every domain resolving to this server, and tells you what it is
seeing while it waits. Renewal failure is loud: a banner, an audit entry and an
email.

**Deploys** as `git pull` plus a per-site script pre-filled from the framework
store, with a database backup taken automatically before any deploy that
migrates, a WordPress exclude list so uploads and plugins survive, deploy
history, and redeploy-previous-commit. Signed git webhooks, off by default.

**Backups** encrypted, scheduled on systemd timers with daily/weekly/monthly
retention, pushed to S3-compatible storage or SFTP, and **verified by restoring
into a scratch database on a schedule** rather than assumed. Plus a server-state
archive that makes a rebuild onto a fresh droplet possible.

**Operations**: one alert list for the six things that actually go wrong, uptime
checks per site, log rotation, a hardening audit, ufw and fail2ban status, SSH
key management, and a file manager and SFTP scoped to one site.

**A panel safe to expose**: Argon2id, session cookies, CSRF on every
state-changing route, per-IP rate limiting, optional TOTP, two roles enforced on
every route *and* every WebSocket message, and an append-only audit log.

Full documentation: **<https://realrashid.github.io/servlo>**

## Status

**v0.1.0, beta.** Every story in the backlog is built, tested and merged. It has
not yet been run end to end on a real droplet, and until that pass is done this
is **not ready for anything you care about**.

Being straight about what that means: the test suite, the deleted-feature scan
and the UI checks are green, but they run in a container. A real machine is the
only thing that can prove a certificate issues from Let's Encrypt, that a backup
lands in a Spaces bucket, or that every site comes back after a reboot.
[`HANDOVER.md`](HANDOVER.md) lists exactly what that pass has to confirm.

GitHub Actions is switched off for this repository, so the badges above do not
include CI and no commit here has been verified by a runner. That is a billing
condition, not a code one, and `HANDOVER.md` covers it too.

`PRD.md` is the specification and `STORY.md` is the backlog.

## What Servlo is not

It is not multi-tenant hosting. There are no per-client Linux users, no root
broker daemon and no edge proxy. Every site runs as the same Linux user, which
is a deliberate, documented tradeoff: a compromised site can read every other
site's `.env` on that server. Per-site database users with schema-scoped grants
limit what a leaked `.env` is worth, but the advice stands not to co-locate a
site you do not control with a site that matters.

There is also no mail server, and there will not be one. SMTP credentials are
configured per site and for the panel; that is the whole email story.

Servlo does not disable SSH password authentication. It adds authorised keys
alongside what is already there and configures fail2ban, and it will not lock
you out of your own machine on your behalf.

## A fork of Lerd

Servlo is a fork of [Lerd](https://github.com/lerd-env/lerd), an MIT-licensed
local PHP development environment. Lerd's engine already does most of the hard
work: multiple PHP versions side by side, per-site nginx vhost generation with
`nginx -t` validation, one-click services from a YAML preset store, supervised
queue and schedule workers, certificate issuance and renewal, live logs, and a
Svelte dashboard, all rootless with no Docker daemon and no permanent root
process.

Lerd points that engine at `.test` domains on a laptop. Servlo aims it at a real
server: real domains, ACME certificates, production PHP defaults, panel
authentication with roles, backups and server rebuild. The local-development
features with no production meaning, and the ones that amount to remote code
execution on a public server, were removed rather than disabled, and a scan in
CI fails the build if any of them reappears.

The upstream MIT copyright notice is retained in [LICENSE](LICENSE).

## Contributing

Read [`CLAUDE.md`](CLAUDE.md) first: it carries the design laws, chiefly that
behaviour belongs in the YAML stores rather than in Go, and that no Go code may
know a framework's name. A new framework, service or app is a store change, not
a release.

Security issues: [`SECURITY.md`](SECURITY.md).

## Licence

MIT. See [LICENSE](LICENSE).
