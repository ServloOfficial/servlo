# Symfony walkthrough

End-to-end: from `servlo install` to a Symfony app running on `https://myapp.example.com` with Doctrine, MySQL, and a Messenger worker.

::: info Prerequisites
You've already run `servlo install` once on this machine. If not, see [Installation](installation.md).
:::

---

## 1. Symfony is already known

Nothing to register. Symfony ships in the framework store, embedded in the
binary, alongside Laravel, WordPress, Drupal, Joomla, Grav, Magento, CakePHP,
CodeIgniter, Statamic and Tempest. Servlo detects the framework from the
project itself — for Symfony, `symfony.lock` or the `symfony/framework-bundle`
requirement — and takes the document root, the console binary, the env wiring
and the worker set from that definition.

`servlo framework list` shows what this install knows. You only write a YAML of
your own for a framework the store does not carry; see
[Framework definitions](/usage/framework-definitions).

---

## 2. Create the project

::: code-group

```bash [servlo new]
cd ~/sites
servlo new myapp --framework=symfony
# runs: composer create-project symfony/skeleton ./myapp
```

```bash [composer]
cd ~/sites
composer create-project symfony/skeleton myapp
```

```bash [existing repo]
cd ~/sites
git clone git@github.com:you/myapp.git
```

:::

---

## 3. Register the site

```bash
cd myapp
servlo link
```

`servlo link` detects Symfony (via `symfony.lock` or the composer package), assigns `http://myapp.example.com`, and sets the document root to `public/`.

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
? Services: [redis]
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
  ◉ servlo secure                  # TLS for myapp.example.com
  ◉ messenger:start
```

The "Run migrations", "Clear cache", and "Load fixtures" steps come from the `setup:` block in your `symfony.yaml`. Servlo surfaces them automatically and respects the `check:` rules; fixtures only appears if `doctrine/doctrine-fixtures-bundle` is installed.

When it finishes, `https://myapp.example.com` is serving and `servlo-messenger-myapp` is running as a systemd user service.

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
| `servlo link` | Assigned `myapp.example.com`, set document root to `public/` |
| `servlo init` | Wrote `.servlo.yaml` with PHP, Node, MySQL, messenger |
| `servlo env` (via setup) | Wrote `DATABASE_URL=mysql://root:servlo@servlo-mysql:3306/myapp?serverVersion=8.0` into `.env.local`, seeded from the committed `.env` |
| `servlo secure` (via setup) | Issued a certificate, set `DEFAULT_URI=https://myapp.example.com` |
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
