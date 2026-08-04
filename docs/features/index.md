---
description: What servlo gives you beyond serving sites, the web dashboard, TUI, MCP server, automatic HTTPS, .test DNS, profiler, tinker and more.
---

# Features

Beyond serving PHP sites, servlo ships a set of tools for working with them. Everything here is part of the single binary, there is nothing extra to install.

## Interfaces

- [Web UI](/features/web-ui) is the browser dashboard at `servlo.localhost`, for sites, services, logs, databases and workers.
- [TUI](/features/tui) is the terminal dashboard, informative with reversible quick actions.
- [Commands](/features/commands) covers the CLI surface and per-framework custom commands.
- System tray puts start, stop and site shortcuts in your desktop tray.
- MCP server exposes servlo to AI assistants so they can inspect and drive your environment.

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
- Profiler captures request timings and hands off to SPX.
- Tinker is an in-browser REPL against your application.
- Dumps collects `dump()` output from your code.
- Notifications surfaces failures on your desktop.

For installation see [Getting Started](/getting-started/requirements), and for day to day workflows see [Usage](/usage/sites).
