---
description: What servlo gives you beyond serving sites, from the web dashboard and TUI to automatic HTTPS, logs and queries.
---

# Features

Beyond serving PHP sites, servlo ships a set of tools for working with them. Everything here is part of the single binary, there is nothing extra to install.

## Interfaces

- [Web UI](/features/web-ui) is the browser dashboard at `servlo.localhost`, for sites, services, logs, databases and workers.
- [TUI](/features/tui) is the terminal dashboard, informative with reversible quick actions.
- [Commands](/features/commands) covers the CLI surface and per-framework custom commands.

## Serving sites

- [HTTPS](/features/https) issues locally trusted certificates automatically.
- DNS resolves `.test` domains without editing `/etc/hosts`.
- [Project setup](/features/project-setup) is how servlo detects a framework and configures it.
- [Env setup](/features/env-setup) wires service credentials into your site's `.env`.
- Git worktrees serves branches side by side on their own domains.
- [FrankenPHP](/features/frankenphp) is the alternative runtime to PHP-FPM.

## Inspecting and debugging

- [Logs](/features/logs) tails application, Nginx and container logs in one place.
- [Queries](/features/queries) shows database queries per request.
- Dumps collects `dump()` output from your code.
- Notifications surfaces failures on your desktop.

For installation see [Getting Started](/getting-started/requirements), and for day to day workflows see [Usage](/usage/sites).
