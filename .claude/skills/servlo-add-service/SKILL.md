---
name: servlo-add-service
description: Add or edit a Servlo service preset (database, cache, search engine, admin dashboard) as YAML in the stores/services store. Use whenever the task is to add a new service, a new version of a service, or wire a service into sites — never add services in Go.
---

# Add a Servlo service preset

Service presets live in **`stores/services/<name>.yaml`** in this repository, one
file per service. They are **data, not Go**.

Servlo's default stack is **mysql, postgres, redis, meilisearch and rustfs**.
Upstream also ships Mailpit; Servlo does not, because Mailpit is deleted and there
is no mail server in this product at all (PRD 4.2, 5.12). Everything outside the
default stack is a preset here.

Only reviewed presets in this store may run a container. A project's own config
file declaring an inline service is rejected (S0.7) — that is a code-execution
path, not a convenience.

As with frameworks, the runtime fetch still points at the public
`lerd-env/services` because this repository is private and cannot serve raw
fetches to an installed binary. Author here; S0.8 owns making it reachable.

## Procedure

1. **Find the closest existing preset and copy it.** The existing YAML is the
   schema of record — do not invent fields. For a Redis-alike copy `valkey.yaml`;
   for a database copy `mariadb.yaml` or `mongo.yaml`; for an admin dashboard copy
   `phpmyadmin.yaml` / `pgadmin.yaml`.

2. **Fill the core fields** (`valkey.yaml` shows the minimal shape):
   - `name`, `description`, `family` (groups alternates and admin UIs)
   - `image` (pin a specific tag), `ports`
   - `data_dir` for the persistent volume
   - `env_vars` — the host/port/credentials injected into a linked site's `.env`
   - `connection_url` where applicable

3. **Bind to the container network only.** This is the one place Servlo diverges
   hardest from upstream. A preset must not publish to a public interface, and
   passwords must be strong generated values, not defaults (S4.3, PRD 5.10). A test
   asserts no service listens on a public address, so a preset that publishes will
   fail the build, not merely be discouraged.

4. **Declare dependencies and mounted config** if the preset needs them. For a
   **database engine**, add an `introspect.list_databases` command so it appears in
   the Databases tab: it runs via `sh -c` inside the container and prints one
   `name<TAB>size_bytes` row per user database, filtering the engine's own system
   databases. Copy the block from `mariadb.yaml` (MySQL family),
   `postgres-pgvector.yaml` (Postgres) or `mongo.yaml`.

5. **Remember a database may be remote.** A preset describes a local container,
   but every code path consuming it must also work against an external managed
   database (PRD 3.6). Do not add a preset whose wiring assumes the database is
   local.

6. **Validate end-to-end** on an Ubuntu 24.04 droplet: the container starts, the
   port is reachable only on the container network, and a site's `.env` gets the
   expected vars.

## Rules

- One service per file. No Go changes.
- Pin image tags; never rely on `latest`.
- Keep `description` to one line; it shows in service search.
