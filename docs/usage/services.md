# Services

## Built-in services

| Command | Description |
|---|---|
| `servlo service start <name>` | Start a service (auto-installs on first use) |
| `servlo service stop <name>` | Stop a service container |
| `servlo service restart <name>` | Restart a service container; refreshes the quadlet first so config edits land |
| `servlo service status <name>` | Show systemd unit status |
| `servlo service list` | All services with status, version, and an Update column |
| `servlo service search [query]` | Browse the service-preset store; install a hit with `servlo service preset <name>` |
| `servlo service update <name> [tag]` | Pull a newer image and restart; tag selects an explicit upgrade target |
| `servlo service migrate <name> <version>` | SQL dump + restore for cross-version moves (mysql, mariadb, postgres); `<version>` is a preset version label such as `18` |
| `servlo service rollback <name>` | Swap back to the previously-running image (toggles) |
| `servlo service pin <name>` | Pin a service so it is never auto-stopped |
| `servlo service unpin <name>` | Unpin a service so it can be auto-stopped when unused |
| `servlo service expose <name> <host:container>` | Publish an extra port on a built-in service |
| `servlo service expose <name> <host:container> --remove` | Remove a previously exposed port |
| `servlo service port <name> <port>` | Move a service's primary published host port (e.g. free 3306 for a host server) |
| `servlo service port <name> <port> --container <cport>` | Move a specific mapping of a multi-port service (e.g. RustFS' 9001 console) |
| `servlo service port <name> --reset` | Reset a service to its preset default published port |

The five services above are compiled into the binary as YAML presets. The other
twenty-three — Elasticsearch, OpenSearch, MongoDB, RabbitMQ, Typesense, Selenium,
phpMyAdmin, pgAdmin and the rest — live in this repository under
`stores/services/` and are embedded too, so a fresh install never depends on the
repository being reachable. The runtime fetch is the update path rather than the
only way in, and a definition is verified against a recorded sha256 digest before
it is used.

Available services: `mysql` (8.4 LTS canonical, 9.7 LTS / 5.7 alternates), `redis` (7-alpine), `postgres` (16 canonical with PostGIS, 17 / 18 alternates), `meilisearch` (v1.42), `rustfs` (S3-compatible).

Default services are defined as YAML presets with `default: true` in the servlo binary. Adding or replacing a default service is a YAML edit, not a code change. Each preset declares its own `update_strategy` (patch / minor / rolling), whether `track_latest` should auto-bump fresh installs to the current upstream, whether `allow_major_upgrade` lets the cross-strategy upgrade button cross numeric majors, and where the engine records the version that wrote its data (`data_version_file`) so a data dir that outlives its config still gets a server that can open it. See [Service updates](service-updates.md) for the full update / upgrade / migrate / rollback flow.

`servlo service list` shows the version (derived from the image tag) and an Update column with green / amber / violet badges:

```
╭─────────────┬─────────┬────────┬────────╮
│ Service     │ Version │ Status │ Update │
├─────────────┼─────────┼────────┼────────┤
│ meilisearch │ v1.42.1 │ active │        │
│ mysql       │ v8.4.9  │ active │        │
│ postgres    │ v16     │ active │        │
│ redis       │ v7.4.8  │ active │        │
│ rustfs      │ latest  │ active │        │
╰─────────────┴─────────┴────────┴────────╯
```

The Web UI, the TUI, and `servlo status` display the same labels. Services pinned to rolling tags (`latest`, `main`) show the tag verbatim. Services where an update is available show `→ <new-tag>`; cross-strategy upgrades show `⇧ <new-tag>` in amber.

### Exposing extra ports on bundled services

Bundled services publish a fixed set of ports by default. Use `servlo service expose` to bind additional host ports without recompiling or replacing the service. This works for any service servlo ships as a preset, both the default-stack ones (MySQL, PostgreSQL, Redis) and the optional ones you install on demand (Gotenberg, MongoDB, Elasticsearch, and so on). Only genuinely custom services you define yourself are excluded, since those declare their ports in their own YAML.

```bash
# Expose MySQL on an extra port (e.g. for a second GUI client using a different port)
servlo service expose mysql 13306:3306

# Remove the extra port
servlo service expose mysql --remove 13306:3306
```

Extra port mappings are persisted in `~/.config/servlo/config.yaml` under `services.<name>.extra_ports` and are applied automatically every time the service starts. If the service is already running when you run `expose`, it is restarted immediately to apply the change.

You can also edit `~/.config/servlo/config.yaml` directly:

```yaml
services:
  mysql:
    extra_ports:
      - "13306:3306"
```

Then apply with `servlo service restart mysql`.

You can also manage extra ports from the dashboard: open a service and switch to the **Ports** tab, then add or remove mappings alongside the published port. The CLI, dashboard and TUI all route through the same logic, so a change made on one surface shows up on the others.

### Moving a service's published host port

Each service publishes on a default host port (MySQL `3306`, PostgreSQL `5432`, Redis `6379`, and so on). Every member of a service family shares that one canonical port rather than pre-spacing itself, so a single database of any family lands on the familiar port. When servlo writes a service's quadlet and the port can't be bound, or another installed same-family service already holds it, it shifts the service to the next free port and records it, so the container comes up cleanly instead of failing to bind. The decision is made purely from port availability and what other servlo services already claim: servlo never inspects host files, sockets, or installed packages. It applies to every service, not just databases.

Move a port yourself, or undo an automatic shift, with `servlo service port`:

```bash
# Publish servlo-mysql on 3307 so a host-installed MySQL can keep 3306
servlo service port mysql 3307

# Go back to the preset default
servlo service port mysql --reset   # or: servlo service port mysql 0
```

The container-internal port never changes, so containerized apps (which reach the service by name over the `servlo` network) are unaffected. Only host clients pointed at the old published port need to follow. Host-proxy sites that connect over the published loopback port have their `.env` regenerated automatically when the port moves. A host-proxy site that is paused when the port moves is skipped at that moment and picks up the new port when it is next unpaused.

Some services publish more than one host port: RustFS exposes the S3 API on `9000` and the console on `9001`, Selenium the WebDriver on `4444` and the noVNC view on `7900`. `servlo service port <name> <port>` moves the primary (first) mapping. To move any other published port, name the mapping by its container-internal port with `--container`:

```bash
# Move the RustFS console off 9001 to 9002 (the S3 API on 9000 is untouched)
servlo service port rustfs 9002 --container 9001

# Put it back
servlo service port rustfs --reset --container 9001
```

The dashboard link for a service always follows the port its dashboard is served on, so moving the RustFS console port re-points the dashboard and the "open dashboard" iframe automatically.

The chosen ports are persisted in `~/.config/servlo/config.yaml` and reapplied on every start: the primary under `services.<name>.published_port`, any other mapping under `services.<name>.published_ports` keyed by container port. Once a port is set, automatically or with `servlo service port`, it sticks: servlo never moves it again on its own, not even back to the default when that frees up later. Change it only with `servlo service port`.

Every published port can also be moved from the dashboard: a service's **Ports** tab lists one editable host-port field per published port (primary and secondary alike), each with a reset-to-default. The TUI shows the current published and extra ports read-only; editing stays in the CLI and dashboard.

::: warning Known limitation
The shift is decided at quadlet-write time, from whether the port can be bound right then. A host server that is installed but stopped at that moment leaves its port looking free, so servlo may take it and clash when that server next starts (for example at boot). This is the deliberate trade for not inspecting the host: a host database is usually running, and the failure is loud. Recover by moving servlo onto a free port with `servlo service port <name> <port>`.
:::

---

## Service credentials

::: tip Two sets of hostnames
Services run as Podman containers on the `servlo` network. Two hostnames apply depending on where you're connecting from:

- **From host tools** (e.g. TablePlus, Redis CLI): use `127.0.0.1`
- **From your Laravel app** (PHP-FPM runs inside the `servlo` network): use container hostnames (e.g. `servlo-mysql`)

`servlo service start <name>` prints the correct `.env` variables to paste into your project.
:::

| Service | Default version | Host (host tools) | Host (Laravel `.env`) | Port | User | Password | DB |
|---|---|---|---|---|---|---|---|
| MySQL | 8.4 LTS (`mysql:8.4`) | 127.0.0.1 | servlo-mysql | 3306 | root | generated | `servlo` |
| PostgreSQL | 16 + PostGIS 3.5 | 127.0.0.1 | servlo-postgres | 5432 | postgres | generated | `servlo` |
| Redis | 7-alpine | 127.0.0.1 | servlo-redis | 6379 | - | - | - |
| Meilisearch | v1.42 | 127.0.0.1 | servlo-meilisearch | 7700 | - | - | - |
| RustFS | latest | 127.0.0.1 | servlo-rustfs | 9000 | `servlo` | generated | per-site bucket |

Passwords are generated once per install rather than shipped in the definitions,
so no two machines share one. `servlo service start <name>` prints the values for
that service, and they also live at `~/.config/servlo/service-password`. See
[Production mode](/features/production-mode) for how the substitution works.

Additional UIs:

- RustFS console: `http://127.0.0.1:9001`

### Database admin UIs

Installing a database installs its admin UI with it: phpMyAdmin with MySQL or MariaDB, pgAdmin with PostgreSQL. There is no second question, because there was never a second answer. It arrives after the engine is up, reported as its own step in the install output, and a UI that fails to install is said so rather than failing the database that had just come up fine.

Nothing in servlo knows that phpMyAdmin goes with MySQL. Each admin UI's definition already declares what it administers, in `admin_for`, and the install reads that backwards: given the engine going in, which definitions say they administer it. A UI already installed is left alone, and a second engine of the same family does not get a second copy of the same UI, since one already lists every database of that family. Bringing an admin UI to a new engine is a line of YAML in the store.

**They list connections, not containers.** Both UIs take their server list from the [connections](database.md#connections) this install knows: the local engines and any managed database a site is on. A site whose data is on DigitalOcean opens phpMyAdmin and finds its own database there beside the local one, already logged in, with the TLS mode the connection uses and the provider's CA certificate mounted in for `verify-ca`. That list is rebuilt whenever a database service starts, stops or is installed, so it does not go stale.

The definitions ask for it through a `connections:<families>=<field>` entry in `dynamic_env`, which resolves to one comma-separated value per connection — hosts, names, users, passwords and TLS modes as parallel lists. `discover_family` still exists and still answers the question it always answered, which is which containers of a family are up; it simply cannot see a database that is not one.

Both UIs are reachable from the panel, embedded same-origin, and from the Databases tab's **Open in** button on each engine.

### RustFS, per-site buckets

RustFS is an S3-compatible object storage service (a drop-in replacement for MinIO). When `servlo env` detects it is needed (via `FILESYSTEM_DISK=s3` or `AWS_ENDPOINT` in `.env`), it automatically:

1. Creates a bucket named after the site handle, sanitised to match the S3 naming rules (lowercase, digits, hyphens, dots only, max 63 chars). Underscores in the handle are rewritten as hyphens, so `admin_astrolov` becomes bucket `admin-astrolov`.
2. Sets the bucket to **anonymous read**, so an object can be served by URL without a signed request
3. Writes the correct `.env` values:

::: warning What "anonymous read" means here
Anyone who can reach RustFS can read any object in the bucket without
credentials. RustFS publishes on loopback only — like every service except
nginx, its ports are pinned to `127.0.0.1` — so that is this server and the
containers on its Podman network, not the internet. It still means a site's
uploads are readable by anything else running on this machine, and that
proxying the bucket out through a vhost would publish it. Do not put anything in
it you would not serve.
:::

```ini
FILESYSTEM_DISK=s3
AWS_ACCESS_KEY_ID=servlo
AWS_SECRET_ACCESS_KEY=<generated, see servlo service start rustfs>
AWS_DEFAULT_REGION=us-east-1
AWS_BUCKET=my-project
AWS_URL=http://localhost:9000/my-project
AWS_ENDPOINT=http://servlo-rustfs:9000
AWS_USE_PATH_STYLE_ENDPOINT=true
```

If a historical `AWS_BUCKET` value with underscores (or other S3-invalid characters) is present from an earlier servlo run or Sail import, `servlo env` will sanitise it in place on the next run.

`AWS_URL` points to the public bucket URL (browser-reachable). `AWS_ENDPOINT` is the internal container address used by PHP.

### Migrating from MinIO to RustFS

RustFS exposes the same S3 API as MinIO with the same default credentials, no application changes are needed after migration.

**Automatic prompt during `servlo update`**

If servlo detects an existing MinIO data directory (`~/.local/share/servlo/data/minio`) during `servlo update`, it will offer to migrate automatically:

```
==> MinIO detected, migrate to RustFS? [y/N]
```

Answering `y` runs the full migration in-place. The update continues regardless of your answer.

**Manual migration**

```bash
servlo minio:migrate
```

This command:

1. Stops the `servlo-minio` container (if running)
2. Removes the MinIO quadlet so it no longer auto-starts
3. Copies `~/.local/share/servlo/data/minio/` to `~/.local/share/servlo/data/rustfs/`
4. Updates `~/.config/servlo/config.yaml`: removes the `minio` entry and adds `rustfs`
5. Installs and starts the `servlo-rustfs` service

The original MinIO data directory is **not deleted**. Verify the migration works, then remove it manually:

```bash
rm -rf ~/.local/share/servlo/data/minio
```

---

## More

- [Service updates](service-updates.md): the Update / Upgrade / Migrate / Rollback flow, `update_strategy` / `track_latest` / `allow_major_upgrade` configuration, and recovery from failed migrations.
- [Service presets](service-presets.md): one-command installers for phpMyAdmin, pgAdmin, MongoDB, alternate MySQL / MariaDB versions, Selenium, and Stripe Mock.
- Custom services: YAML schema for your own OCI-based services, with env injection, placeholders, dependencies, and worked examples (Soketi, Stripe).
