---
name: servlo-add-app
description: Add a one-click app installer (WordPress and friends) as YAML in the stores/apps store. Use whenever the task is to make an application installable from the Add Site flow, or to change how an existing app provisions its database, config file or admin account — the app installer must need no Go release.
---

# Add a Servlo app definition

App definitions live in **`stores/apps/<name>.yaml`** in this repository, one file
per app. An app is the fourth way to create a site, alongside an existing folder,
a ZIP upload and a GitHub clone: one click installs the application, creates its
database, writes its config file and sets up the admin account (PRD 5.2, S6.4).

**This store has no upstream.** The project Servlo forked from had no app
installer, so unlike frameworks and services there is no original to copy from
and no inherited file to treat as the schema of record. That makes this the most likely place for an agent
to give up and write Go instead. Do not. The acceptance criterion on S6.4 is
explicit: WordPress installs in one click and is **defined as store YAML with no
Go code specific to it**, so that further apps need no release.

If a capability an app needs genuinely has no declarative expression yet, the fix
is to add a general field to the schema and implement it generically in Go, named
for what it does rather than for the app that wanted it. `wordpress_config` is
wrong; `config_file` with a template is right.

## What an app definition has to cover

- **Identity** — `name`, `label`, `description`, and the framework it maps onto so
  detection, deploy template and doctor checks come from `stores/frameworks/`.
- **Source** — where the release is fetched from, with a pinned version and a
  checksum. An installer that fetches `latest` unverified is a supply-chain hole
  on a production server (PRD 7).
- **Database** — that one is created with a least-privilege, schema-scoped user
  (PRD 5.9, S11.4), and works whether the connection is a local container or an
  external managed database.
- **Config file** — the template and the substitutions, `wp-config.php` being the
  first case. Written 0600 where it holds credentials.
- **Admin account** — the fields the setup flow collects, with a generated
  password never echoed into logs or API responses.
- **Post-install** — for WordPress specifically, disabling WP pseudo-cron and
  installing a real one-minute system cron (PRD 5.11, S12.2), declared here.

## Rules

- No app name may appear in Go. Same law as frameworks, and the surface scan is
  not what catches this one — review is, so be strict with yourself.
- Pin the version and verify the checksum.
- Credentials are generated, redacted in output, and written to files that are
  0600.
- Validate by actually installing the app on an Ubuntu 24.04 droplet and logging
  into it. An app installer that has never been run is not done.
