# Servlo

> A free, open-source production PHP server panel for Ubuntu 24.04 LTS.
> Rootless Podman, systemd units, real domains and real certificates,
> driven from a browser.

[![CI](https://github.com/realrashid/servlo/actions/workflows/ci.yml/badge.svg)](https://github.com/realrashid/servlo/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-Ubuntu%2024.04%20LTS-lightgrey)]()

One operator runs many PHP sites on one Ubuntu droplet. Add a site from a ZIP, a
GitHub clone, an existing folder or a one-click app install; bind it to a real
domain; click **Get SSL** once DNS points at the server; pick a PHP version per
site; toggle services like MySQL and Redis; deploy with `git pull` plus a
per-site script; and rely on backups that are verified by real test restores.

This is the cPanel/Forge/Ploi niche, self-hosted and free. There is no paid tier
and no telemetry.

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
authentication with roles, backups and server rebuild.

The upstream MIT copyright notice is retained in [LICENSE](LICENSE).

## Status

Servlo is in early development and is **not yet ready to run anything you care
about**. Phase 0 (fork and strip) is in progress: the local-development features
that have no production meaning, and the ones that amount to remote code
execution on a public server, are being removed rather than disabled.

See `PRD.md` for the specification and `STORY.md` for the backlog.

## What Servlo is not

It is not multi-tenant hosting. There are no per-client Linux users, no root
broker daemon and no edge proxy. Every site runs as the same Linux user, which
is a deliberate, documented tradeoff: a compromised site can read every other
site's `.env` on that server. Per-site database users with schema-scoped grants
limit what a leaked `.env` is worth, but the advice stands not to co-locate a
site you do not control with a site that matters.

There is also no mail server, and there will not be one. SMTP credentials are
configured per site and for the panel; that is the whole email story.

## Requirements

Ubuntu 24.04 LTS with Podman 4.5 or newer. The installer refuses other
distributions rather than half-installing.

## Licence

MIT. See [LICENSE](LICENSE).
