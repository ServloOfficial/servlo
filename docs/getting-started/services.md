# Services walkthrough

Servlo ships with **MySQL, PostgreSQL, Redis, Meilisearch, and RustFS** built in. For a curated set of common extras (**MongoDB, phpMyAdmin, pgAdmin, Mongo Express, stripe-mock, MariaDB, alternative MySQL versions**) servlo ships **bundled presets** you can install with one command. Anything not on either list runs as a **custom service**: a YAML file dropped into `~/.config/servlo/services/`, registered with one command, and managed by `servlo service start/stop/list` exactly like the built-ins.

::: info Prerequisites
You've already run `servlo install` once on this machine. If not, see [Installation](installation.md).
:::

::: tip Drive it from your AI assistant
:::

---

## How it works (30 seconds)

For a bundled preset:

```bash
# Browse the catalogue
servlo service preset

# Install one (becomes a normal custom service)
servlo service preset phpmyadmin

# Start it
servlo service start phpmyadmin
```

For everything else, three steps:

```bash
# 1. Save the YAML somewhere
$EDITOR ~/.config/servlo/services/<name>.yaml

# 2. Register it with servlo
servlo service add ~/.config/servlo/services/<name>.yaml

# 3. Start it
servlo service start <name>
```

Either way, the service appears in `servlo service list`, the Web UI Services panel, and `servlo start` / `servlo stop` cycles.

For the full YAML schema (env vars, `depends_on`, `site_init`, <code v-pre>{{site}}</code> placeholders, etc.) see Custom services reference.

---

## Recipe: MongoDB (preset)

```bash
servlo service preset mongo
servlo service start mongo
```

| From | Host |
|---|---|
| Your app (PHP-FPM) | `servlo-mongo:27017` |
| Host tools (Compass, mongosh) | `127.0.0.1:27017` |
| User / password | `root` / generated (`servlo service start mysql`) |

The preset ships with `env_detect` and `site_init` already wired up, so when `servlo env` runs in a project that has `MONGO_DSN=mongodb://...` in its `.env`, the connection string is rewritten to point at `servlo-mongo` and a per-site database is created automatically.

For a Mongo web UI, install the paired preset:

```bash
servlo service preset mongo-express
servlo service start mongo-express
```

It depends on `mongo`, so the database is started first automatically. Open `http://localhost:8082`.

---

## Recipe: phpMyAdmin (preset)

```bash
servlo service preset phpmyadmin
servlo service start phpmyadmin
```

Open `http://localhost:8080`. The preset declares `depends_on: mysql`, so `servlo service start phpmyadmin` boots MySQL first if it isn't already running, and `servlo service stop mysql` cascades down to phpMyAdmin. Sign-in is auto-handled against `servlo-mysql` as `root` / `servlo`.

---

## Recipe: pgAdmin (preset)

```bash
servlo service preset pgadmin
servlo service start pgadmin
```

Open `http://localhost:8081`, log in with `admin@pgadmin.org` and the generated service password (`cat ~/.config/servlo/service-password`). The preset ships with a pre-loaded `Servlo Postgres` connection (via a bundled `servers.json` + `pgpass`) so you don't need to add a server manually. Server mode is disabled and there is no master password. The preset declares `depends_on: postgres`, so PostgreSQL starts first automatically.

---

## Recipe: Adminer

A lightweight, single-file alternative to phpMyAdmin/pgAdmin that supports both MySQL and PostgreSQL:

```yaml
# ~/.config/servlo/services/adminer.yaml
name: adminer
image: docker.io/library/adminer:latest
description: "Universal database web client (MySQL + PostgreSQL + more)"
ports:
  - 8083:8080
depends_on:
  - mysql
dashboard: http://localhost:8083
```

```bash
servlo service add ~/.config/servlo/services/adminer.yaml
servlo service start adminer
```

Open `http://localhost:8083`. Choose the system (MySQL with host `servlo-mysql`, or PostgreSQL with host `servlo-postgres`), then user `root` / `postgres` and the generated service password.

---

## Recipe: Elasticsearch

```yaml
# ~/.config/servlo/services/elasticsearch.yaml
name: elasticsearch
image: docker.io/elasticsearch:8.13.4
description: "Elasticsearch search engine"
ports:
  - 9200:9200
environment:
  discovery.type: single-node
  xpack.security.enabled: "false"
  ES_JAVA_OPTS: "-Xms512m -Xmx512m"
data_dir: /usr/share/elasticsearch/data
env_vars:
  - "ELASTICSEARCH_HOST=http://servlo-elasticsearch:9200"
env_detect:
  key: ELASTICSEARCH_HOST
```

```bash
servlo service add ~/.config/servlo/services/elasticsearch.yaml
servlo service start elasticsearch
```

| From | Host |
|---|---|
| Your app | `http://servlo-elasticsearch:9200` |
| Host tools | `http://127.0.0.1:9200` |

---

## Recipe: RabbitMQ

```yaml
# ~/.config/servlo/services/rabbitmq.yaml
name: rabbitmq
image: docker.io/library/rabbitmq:3-management
description: "RabbitMQ message broker with management UI"
ports:
  - 5672:5672
  - 15672:15672
environment:
  RABBITMQ_DEFAULT_USER: servlo
  RABBITMQ_DEFAULT_PASS: "{{password}}"
data_dir: /var/lib/rabbitmq
env_vars:
  - "RABBITMQ_HOST=servlo-rabbitmq"
  - "RABBITMQ_PORT=5672"
  - "RABBITMQ_USER=servlo"
  - "RABBITMQ_PASSWORD={{password}}"
env_detect:
  key: RABBITMQ_HOST
dashboard: http://localhost:15672
```

```bash
servlo service add ~/.config/servlo/services/rabbitmq.yaml
servlo service start rabbitmq
```

Management UI at `http://localhost:15672` (`servlo` / `servlo`).

---

## Verify

```bash
servlo service list
```

Each registered service shows up with a `[preset]` marker if it came from a bundled preset, or `[custom]` if it was added from a YAML file. `[pinned]` means it stays running across `servlo start`/`servlo stop` cycles. Indented sub-lines show dependency or auto-stop reasons.

```bash
servlo service status mongodb     # systemd unit status
servlo service stop mongodb        # stop without removing
servlo service remove mongodb      # stop + remove quadlet + delete YAML
```

The data directory at `~/.local/share/servlo/data/<name>/` is **not** deleted by `service remove`. Wipe it manually if you want a clean slate.

---

## Per-site auto-injection

Three of the recipes above (`mongo` preset, `elasticsearch`, `rabbitmq`) declare `env_detect` and `env_vars`. When you run `servlo env` in a project that already references one of those services in its `.env` (e.g. `MONGO_DSN=` is set), servlo:

1. Starts the service if it isn't already running
2. Substitutes <code v-pre>{{site}}</code> in the env vars with the project's site handle
3. Writes the resulting variables into the project's `.env`
4. Runs `site_init.exec` inside the container (mongo preset) to create per-site databases

This means installing the preset (or dropping the YAML) once is enough, every project that needs the service gets wired up automatically on `servlo env` (which `servlo init` and `servlo setup` both call).

---

## Next steps

- [Services reference](../usage/services.md): full YAML schema, dependency rules, custom command flags, RustFS / Soketi / stripe-mock built-in details
- [Configuration](../configuration.md): embedding services directly in `.servlo.yaml` so they ship with the repo
