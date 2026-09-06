---
description: Install Servlo on Ubuntu 24.04 and get a PHP site running on its own domain with HTTPS.
---

# Getting Started

Servlo is a control panel for a production PHP server on Ubuntu 24.04 LTS. It
runs nginx, PHP-FPM and your services as rootless Podman containers, so there is
no Docker daemon, no permanent root process, and no sudo for day to day work.

If you just want a site running, read [Requirements](/getting-started/requirements)
and then [Installation](/getting-started/installation). Together they take a few
minutes.

## Install

- [Requirements](/getting-started/requirements) — Ubuntu 24.04 and the versions Servlo checks for before it will install.
- [Installation](/getting-started/installation) — the main path: one script that sets up the directories, the container network, nginx and the watcher, and configures the server basics a fresh droplet is missing.
- [Quick Start](/getting-started/quick-start) — the short version once Servlo is installed: put a project on a domain and give it a certificate.

## Framework walkthroughs

Servlo detects your framework from the project itself and configures workers,
environment wiring and health checks from a versioned store definition rather
than from rules baked into the binary.

- [Laravel](/getting-started/laravel)
- [Symfony](/getting-started/symfony)
- [WordPress](/getting-started/wordpress)
- [Containers (Node, Python, Go, …)](/getting-started/containers) for stacks that are not PHP at all.

## Then what

- [Services](/getting-started/services) adds phpMyAdmin, Redis, Meilisearch and the rest of the preset store.
- [Site management](/usage/sites) is the day to day: adding sites, PHP versions, deploys.
- [Domains](/usage/domains) and [HTTPS / TLS](/features/https) cover getting a real certificate on a real name.
- [Backups](/usage/backups) covers the part that matters when a server is lost — including rebuilding onto a fresh droplet.
- [Web UI](/features/web-ui) is the dashboard, which is where most of this happens once you are set up.
