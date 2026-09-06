# Laravel walkthrough

End-to-end: from `servlo install` to a Laravel app running on `https://myapp.example.com` with a database, queue worker, and scheduler.

::: info Prerequisites
You've already run `servlo install` once on this machine. If not, see [Installation](installation.md).
:::

---

## 1. Create the project

Most sites on a server arrive from a repository rather than being created on
it — clone from GitHub in the dashboard and Servlo mints a deploy key for you to
paste into the repository, verifies the connection, then clones. See
[Site management](/usage/sites).

For a project you are starting here, Servlo ships the official Laravel installer,
on your PATH after `servlo install`. Name the directory for the domain it will be
served on, and the link step has nothing to ask you:

::: code-group

```bash [laravel new]
mkdir -p ~/sites && cd ~/sites
laravel new myapp.example.com
```

```bash [servlo new]
mkdir -p ~/sites && cd ~/sites
servlo new myapp.example.com
# runs: composer create-project laravel/laravel ./myapp.example.com
```

```bash [existing repo]
cd ~/sites
git clone git@github.com:you/myapp.git
```

:::

The `laravel new` installer walks you through starter kit, auth, and database choices interactively. `servlo new` is the framework-agnostic alternative: it runs the bare `composer create-project` so you skip the installer's prompts.

---

## 2. Register the site

```bash
cd myapp.example.com
servlo link
```

`servlo link myapp.example.com` registers the directory and serves it on that name. The domain is required: servlo has no TLD of its own to append, so a bare name is refused. If the directory is itself named `myapp.example.com`, that is used and the argument can be left off. Pointing the name at this server is yours to do.

::: info Already parked?
If `~/sites` was registered with `servlo park ~/sites`, every project under it is
linked automatically as it appears, each on the domain its own directory is named
for. You can skip `servlo link` entirely.
:::

---

## 3. Configure PHP, Node, database, services

Run the init wizard from inside the project:

```bash
servlo init
```

```
? PHP version: 8.5
? Node version (leave blank to skip): 22
? Enable HTTPS? Yes
? Database: MySQL (servlo-mysql)
? Services: [redis]
? Workers to auto-start: [queue, schedule]
Saved .servlo.yaml
```

The **Database** select lists every recognised DB family installed on your
machine: SQLite, the built-in MySQL and PostgreSQL, plus any preset
alternates you've installed (e.g. `MySQL 5.7 (servlo-mysql-5-7)`,
`MariaDB 11 (servlo-mariadb-11)`, `MongoDB (servlo-mongo)`). Pick the version
that matches production. The **Services** multi-select hides admin UIs like
phpMyAdmin / pgAdmin / Mongo Express; those are global developer tools, not
project services, so they don't belong in `.servlo.yaml`.

The wizard writes everything to `.servlo.yaml` in the project root. Services
that came from a preset are stored as a small reference like:

```yaml
services:
  - mysql:
      preset: mysql
      version: "5.6"
  - redis
```

Commit that file; on any other machine, `servlo link` reads it, installs the
referenced preset locally if it isn't already, and restores the same setup
without re-running the wizard.

See [Project Setup](../features/project-setup.md) for the full wizard reference.

---

## 4. Bootstrap the project

```bash
servlo setup
```

`servlo setup` reads `.servlo.yaml` and shows a checkbox list, pre-selecting every step that's actually needed:

```
? Select setup steps to run:
  ◉ composer install
  ◉ npm ci
  ◉ servlo env                     # writes DB_*, REDIS_*, MAIL_* into .env
  ◉ php artisan migrate
  ◯ php artisan db:seed
  ◉ php artisan storage:link
  ◉ npm run build
  ◉ servlo secure                  # issues TLS for myapp.example.com
  ◉ queue:start
  ◉ schedule:start
```

Press enter and watch them run. When it's done, the browser opens at `https://myapp.example.com` and the queue + scheduler are running as systemd user services.

::: info One-shot
`servlo setup --all` skips the prompt and runs every selected step. Useful in scripts or after a fresh clone on CI.
:::

---

## 5. Verify

```bash
servlo status
```

You should see `myapp` listed as `active`, the configured services running, and the queue/schedule workers as `running`. Live logs are in the [Web UI](../features/web-ui.md) at `http://127.0.0.1:7073` under the **App Logs** tab for `myapp`.

---

## What just happened

| Command | What it did |
|---|---|
| `servlo link` | Registered `myapp.example.com` with nginx |
| `servlo init` | Wrote `.servlo.yaml` with PHP 8.5, Node 22, MySQL, Redis, queue, schedule |
| `servlo env` (via setup) | Injected `DB_HOST=servlo-mysql` and `REDIS_HOST=servlo-redis` into `.env` |
| `servlo db:create` (via env) | Created `myapp` and `myapp_testing` databases |
| `servlo secure` (via setup) | Issued a certificate, switched the vhost to HTTPS, set `APP_URL=https://myapp.example.com` |
| `servlo worker start queue/schedule` (via setup) | Launched `servlo-queue-myapp` and `servlo-schedule-myapp` systemd units |

---

## Reverb (optional)

If your project uses Laravel Reverb (`composer require laravel/reverb`), a third worker toggle appears automatically in the UI and `servlo setup` step list. Each Reverb-enabled site gets its own `REVERB_SERVER_PORT` starting at `8080`, written to `.env` on first run so multiple Reverb sites can coexist.

```bash
servlo worker start reverb
```

---

## FrankenPHP / Octane (optional)

By default your site runs on the shared PHP-FPM stack. If you want the persistent-process speedup, switch to a per-site FrankenPHP container:

```bash
servlo runtime frankenphp            # classic mode, one process per request
servlo runtime frankenphp --worker   # Laravel Octane, keeps the app in memory
servlo runtime fpm                   # back to shared PHP-FPM
```

Worker mode needs `composer require laravel/octane` in the project. FrankenPHP is an alternative to PHP-FPM, not a different framework, so queues, scheduler, Reverb, and services keep working unchanged. See the [FrankenPHP runtime](../features/frankenphp.md) page for the hot-reload tradeoffs.

---

## Next steps

- [Frameworks & Workers](../usage/frameworks.md): add Horizon, Pulse, or other custom workers
- [Database](../usage/database.md): `servlo db:import`, `servlo db:shell`, switching engines
- [Services](../usage/services.md): start Meilisearch, RustFS (S3), Postgres, custom services
- Browser Testing: run Laravel Dusk with Selenium, no local Chrome needed
- [HTTPS](../features/https.md): certificates for your sites
