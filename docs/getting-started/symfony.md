# Symfony walkthrough

End-to-end: from `servlo install` to a Symfony app running on `https://myapp.test` with Doctrine, MySQL, and a Messenger worker.

::: info Prerequisites
You've already run `servlo install` once on this machine. If not, see [Installation](installation.md).
:::

::: tip Drive it from your AI assistant
:::

---

## 1. Register the Symfony framework definition (one-time)

Servlo's built-in framework is Laravel. Other frameworks are user-defined YAML files dropped into `~/.config/servlo/frameworks/`. Save this as `~/.config/servlo/frameworks/symfony.yaml`:

```yaml
# ~/.config/servlo/frameworks/symfony.yaml
name: symfony
label: Symfony
detect:
  - file: symfony.lock
  - composer: symfony/framework-bundle
public_dir: public
create: composer create-project symfony/skeleton
console: bin/console
env:
  file: .env.local
  example_file: .env
  format: dotenv
  url_key: DEFAULT_URI
  services:
    mysql:
      detect:
        - key: DATABASE_URL
          value_prefix: "mysql://"
        - key: DATABASE_URL
          value_prefix: "mariadb://"
      vars:
        - "DATABASE_URL=mysql://root:servlo@servlo-mysql:3306/{{site}}?serverVersion={{mysql_version}}"
    postgres:
      detect:
        - key: DATABASE_URL
          value_prefix: "postgresql://"
        - key: DATABASE_URL
          value_prefix: "postgres://"
      vars:
        - "DATABASE_URL=postgresql://postgres:servlo@servlo-postgres:5432/{{site}}?serverVersion={{postgres_version}}"
    redis:
      detect:
        - key: REDIS_URL
        - key: REDIS_DSN
      vars:
        - "REDIS_URL=redis://servlo-redis:6379"
    mailpit:
      detect:
        - key: MAILER_DSN
      vars:
        - "MAILER_DSN=smtp://servlo-mailpit:1025"
composer: auto
npm: auto
workers:
  messenger:
    label: Messenger
    command: php bin/console messenger:consume async --time-limit=3600
    restart: always
    check:
      composer: symfony/messenger
setup:
  - label: "Run migrations"
    command: "php bin/console doctrine:migrations:migrate --no-interaction --allow-no-migration"
    default: true
    check:
      composer: doctrine/doctrine-migrations-bundle
  - label: "Load fixtures"
    command: "php bin/console doctrine:fixtures:load --no-interaction"
    check:
      composer: doctrine/doctrine-fixtures-bundle
  - label: "Clear cache"
    command: "php bin/console cache:clear"
    default: true
```

Then register it with servlo:

```bash
servlo framework add symfony --from-file ~/.config/servlo/frameworks/symfony.yaml
```

You only do this once per machine. From now on, every Symfony project is auto-detected via `symfony.lock` or `symfony/framework-bundle`.

See [Frameworks & Workers](../usage/frameworks.md) for the full schema reference.

---

## 2. Create the project

::: code-group

```bash [servlo new]
cd ~/Servlo
servlo new myapp --framework=symfony
# runs: composer create-project symfony/skeleton ./myapp
```

```bash [composer]
cd ~/Servlo
composer create-project symfony/skeleton myapp
```

```bash [existing repo]
cd ~/Servlo
git clone git@github.com:you/myapp.git
```

:::

---

## 3. Register the site

```bash
cd myapp
servlo link
```

`servlo link` detects Symfony (via `symfony.lock` or the composer package), assigns `http://myapp.test`, and sets the document root to `public/`.

---

## 4. Configure PHP, Node, database, services

```bash
servlo init
```

```
? PHP version: 8.5
? Node version (leave blank to skip): 22
? Enable HTTPS? Yes
? Database: mysql
? Services: [mailpit]
? Workers to auto-start: [messenger]
Saved .servlo.yaml
```

The wizard discovers `messenger` as an available worker because the framework YAML declares it (and the `check: composer: symfony/messenger` rule matches your project).

---

## 5. Bootstrap the project

```bash
servlo setup
```

```
? Select setup steps to run:
  ◉ composer install
  ◉ npm ci                       # only if package.json exists
  ◉ servlo env                     # injects DATABASE_URL, MAILER_DSN, DEFAULT_URI
  ◉ Run migrations               # from framework setup block
  ◉ Clear cache                  # from framework setup block
  ◯ Load fixtures
  ◉ servlo secure                  # mkcert TLS for myapp.test
  ◉ messenger:start
  ◉ servlo open
```

The "Run migrations", "Clear cache", and "Load fixtures" steps come from the `setup:` block in your `symfony.yaml`. Servlo surfaces them automatically and respects the `check:` rules; fixtures only appears if `doctrine/doctrine-fixtures-bundle` is installed.

When it finishes, `https://myapp.test` opens in your browser and `servlo-messenger-myapp` is running as a systemd user service.

---

## 6. Verify

```bash
servlo status
```

```bash
# Tail messenger logs
journalctl --user -u servlo-messenger-myapp -f
```

App logs (anything in `var/log/*.log`) show up in the [Web UI](../features/web-ui.md) **App Logs** tab; add a `logs:` block to `symfony.yaml` to customise paths or parsing.

---

## What just happened

| Command | What it did |
|---|---|
| `servlo framework add symfony` | Registered the YAML so Symfony projects are auto-detected |
| `servlo link` | Assigned `myapp.test`, set document root to `public/` |
| `servlo init` | Wrote `.servlo.yaml` with PHP, Node, MySQL, Mailpit, messenger |
| `servlo env` (via setup) | Wrote `DATABASE_URL=mysql://root:servlo@servlo-mysql:3306/myapp?serverVersion=8.0` and `MAILER_DSN=smtp://servlo-mailpit:1025` into `.env.local`, seeded from the committed `.env` |
| `servlo secure` (via setup) | Issued mkcert cert, set `DEFAULT_URI=https://myapp.test` |
| Doctrine migrations + cache:clear | Ran via the framework's `setup:` block |
| `servlo worker start messenger` (via setup) | Launched `servlo-messenger-myapp` |

---

## FrankenPHP / Symfony Runtime (optional)

By default your site runs on the shared PHP-FPM stack. To run it on a per-site FrankenPHP container instead (useful for testing under the long-running worker model Symfony Runtime provides):

```bash
servlo runtime frankenphp            # classic mode
servlo runtime frankenphp --worker   # Symfony Runtime, keeps the kernel in memory
servlo runtime fpm                   # back to shared PHP-FPM
```

Worker mode needs `composer require runtime/frankenphp-symfony`. Servlo starts the FrankenPHP container with `--watch` so edits to controllers and config reload within a second or two without restarting the worker manually. See the [FrankenPHP runtime](../features/frankenphp.md) page for limitations.

---

## Next steps

- [Frameworks & Workers](../usage/frameworks.md): add custom workers, customise log paths, define more setup steps
- [Database](../usage/database.md): `servlo db:import`, `servlo db:shell`, switching to Postgres
- [Services](../usage/services.md): start Meilisearch, RustFS (S3), custom services
- [HTTPS](../features/https.md): how `servlo secure` works under the hood
