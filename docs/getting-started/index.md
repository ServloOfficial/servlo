---
description: Install servlo and get a PHP site running on a local .test domain with HTTPS, on Ubuntu 24.04.
---

# Getting Started

Servlo is a PHP server panel for Ubuntu 24.04 LTS. It runs Nginx, PHP-FPM and your services as rootless Podman containers, so there is no Docker daemon, no sudo for day to day work, and nothing installed system wide.

If you just want a site running, read [Requirements](/getting-started/requirements) and then [Installation](/getting-started/installation). Together they take a few minutes.

## Install

- [Requirements](/getting-started/requirements) covers the supported distributions and the handful of packages servlo expects to find.
- [Installation](/getting-started/installation) is the main path, a single install script that sets up directories, the container network, DNS and certificates.
- [Quick Start](/getting-started/quick-start) is the short version once servlo is installed: link a directory, get a `.test` domain with HTTPS.

## Framework walkthroughs

Servlo detects your framework from the project itself and configures workers, environment wiring and health checks from a versioned store definition rather than hardcoded rules.

- [Laravel](/getting-started/laravel)
- [Symfony](/getting-started/symfony)
- [WordPress](/getting-started/wordpress)
- [Containers (Node, Python, Go, …)](/getting-started/containers) for stacks that are not PHP at all.

## Add-ons and context

- [Services](/getting-started/services) adds MongoDB, phpMyAdmin, Redis and the rest of the service presets.
- Comparison sets servlo against Herd, Laragon, DDEV, Lando and Sail.
- Laravel Herd for Linux is aimed at people who used Herd on a Mac and moved to Linux.
- Laragon for Linux is aimed at people moving over from Windows.

Once you are set up, [Usage](/usage/sites) covers day to day site management and [Features](/features/web-ui) covers the web UI, TUI and the rest.
