# Command Reference

## Setup & lifecycle

| Command | Description |
|---|---|
| `servlo install` | One-time setup: directories, network, binaries, nginx, watcher |
| `servlo install --database mysql\|mariadb\|postgres\|none` | Install that engine and put new sites on it |
| `servlo start` | Start nginx, PHP-FPM containers, and all installed services; warns about port conflicts and builds or pulls any missing images first |
| `servlo stop` | Stop nginx, PHP-FPM containers, and all running services; leaves the dashboard and watcher running so you can bring servlo back up from either |
| `servlo quit` | Stop all Servlo processes and containers, including the dashboard and the watcher |
| `servlo update` | Check for updates and update after confirmation |
| `servlo update --beta` | Update to the latest pre-release build |
| `servlo update --rollback` | Revert to the previously installed version |
| `servlo whatsnew` | Show what changed between the installed version and the latest release |
| `servlo uninstall` | Stop all containers and remove Servlo |
| `servlo uninstall --force` | Same, skipping all confirmation prompts |
| `servlo panel` | Show how the panel is reached |
| `servlo panel domain set <fqdn>` | Serve the panel on a domain, through nginx |
| `servlo panel domain secure` | Issue a real certificate for the panel's domain |
| `servlo panel domain remove` | Stop serving the panel on its domain |
| `servlo users` | List the panel accounts |
| `servlo users add <name> --role admin\|developer` | Add a panel account |
| `servlo users password <name>` | Change a password, and sign that account out everywhere |
| `servlo users role <name> <admin\|developer>` | Change an account's role |
| `servlo users remove <name>` | Remove an account and end its sessions |
| `servlo users sites <name>` | Show which sites a developer works on |
| `servlo users sites <name> <domain...>` | Set which sites a developer works on |
| `servlo users totp enable <name>` | Enrol an authenticator app, showing a QR code and the recovery codes |
| `servlo users totp disable <name>` | Turn the second factor off, which is the way back from a lost phone |
| `servlo users totp codes <name>` | Issue a fresh set of recovery codes, replacing the old |
| `servlo audit` | Show what changed on this machine, and who changed it |
| `servlo audit --limit <n>` | Same, showing more or fewer entries |
| `servlo sessions list` | Show who is signed in, from where, and when they were last seen |
| `servlo sessions revoke <id>` | End one session |
| `servlo sessions revoke --all` | End every session |
| `servlo production` | Show whether production mode is on |
| `servlo production on` | Turn production mode on (asks for confirmation; `--yes` skips it) |
| `servlo production off --force` | Turn production mode off, which starts showing PHP errors to visitors |
| `servlo autostart enable` | Start Servlo automatically on every login |
| `servlo autostart disable` | Disable autostart on login |
| `servlo path:disable` | Take servlo's shims (`php`, `composer`, `node`…) off your shell PATH; `servlo php` etc. keep working, and installs/updates stop re-adding the entry |
| `servlo path:enable` | Put servlo's shims back on your shell PATH (the default) |
| `servlo status` | Health summary: nginx, PHP-FPM containers, watcher, services, cert expiry and dashboard remote access; shows a notice if an update is available |
| `servlo which` | Show resolved PHP version, Node version, document root, and nginx config for the current site |
| `servlo about` | Show version, build info, and project URL |
| `servlo man [page]` | Browse the built-in documentation in the terminal; pass a page name to jump directly (e.g. `servlo man sites`) |
| `servlo tui` | Open a btop-style terminal dashboard with live site / service / worker status, per-site detail pane, inline domain and version editing, shell drop-in, log tailing, filter + sort, and global settings |
| `servlo check` | Validate `.servlo.yaml` syntax, services, and PHP version before setup |
| `servlo doctor` | Full environment diagnostic: podman, systemd, container DNS, the recorded port strategy (checked both live and across a reboot), PHP images, config validity; also reports how much podman disk is reclaimable. Add `--fix` to apply the safe automatic repairs (confirming each; `--yes` to skip prompts, `--dry-run` to preview); privileged and external-state findings are left for you to run. `--json` emits the findings, each tagged with a fix tier, for tooling |
| `servlo site:doctor [domain]` | App-level health checks for a single site (env file, env drift, application key, a configured SQLite database that is missing or empty, composer/node dependency install + lock, `composer audit`/`npm audit`, PHP version range, routes running well above the site's typical response time, plus the framework's own checks). A broken database suppresses the framework migration check so the remedy isn't repeated. Defaults to the site in the current directory; pass a domain to target another. Add `--json` for machine-readable output |
| `servlo cleanup` | Reclaim podman disk from orphaned servlo images (old PHP build and base images a rebuild left behind), unused service images no installed service references any more (e.g. an old `mysql:8.0` after upgrading, keeping each service's current image and its one-back rollback target), and dangling untagged images. Previews the list and confirms before removing. Never touches a tagged image in use, your databases, or volumes |
| `servlo cleanup --dry-run` | Show what would be reclaimed and the approximate size, remove nothing |
| `servlo cleanup --safe` | Only reclaim images provably built by servlo, leave unused service and dangling images alone |
| `servlo cleanup --yes` | Remove without the confirmation prompt |
| `servlo cleanup auto on` | Enable automatic cleanup (the default): the watcher's daily deep sweep plus immediate reaping after a PHP rebuild or service update/remove |
| `servlo cleanup auto off` | Disable automatic cleanup; `servlo cleanup` still works on demand |
| `servlo cleanup auto status` | Show whether automatic cleanup is enabled |
| `servlo bug-report [-o file] [--log-lines n] [--show-real-names]` | Dump doctor output, config files, unit state, recent logs, network state and env vars to a plain-text file you can attach to a GitHub issue. Site names, domains, parked paths, home paths and the username are anonymized by default; `--show-real-names` keeps raw values |
| `servlo logs [-f] [target]` | Show logs for the current project's FPM container, `nginx`, a service name, or a PHP version |

## Project creation

| Command | Description |
|---|---|
| `servlo new <name-or-path>` | Scaffold a new PHP project using the framework's create command (default: Laravel) |
| `servlo new <name> --framework=<name>` | Scaffold using a specific framework |
| `servlo new <name> -- <extra args>` | Pass extra args to the scaffold command |

## Project setup

| Command | Description |
|---|---|
| `servlo init` | Wizard: choose PHP version, HTTPS, and services, save `.servlo.yaml`, apply |
| `servlo init --fresh` | Re-run the wizard with existing `.servlo.yaml` values as defaults |
| `servlo setup` | Bootstrap a project: runs the servlo init wizard first, then a checkbox list of steps |
| `servlo setup --all` | Run init (or apply saved `.servlo.yaml`) and all steps without prompting (useful in CI) |
| `servlo setup --skip-open` | Same as above but don't open the browser at the end |

Setup steps include common tasks (composer install, npm install, servlo env) plus framework-specific commands defined in the framework's `setup` field (e.g. migrations, storage links). See [Framework definitions](/usage/framework-definitions) for how to define custom setup commands.

## Site management

| Command | Description |
|---|---|
| `servlo park [dir]` | Register every PHP project inside `dir` as a site, and keep doing so as new ones appear (defaults to cwd) |
| `servlo unpark [dir]` | Remove a parked directory and unlink all its sites |
| `servlo link [domain]` | Register the current directory as a site on a fully qualified domain. The argument is required unless the directory is itself named for the domain or `.servlo.yaml` declares one: servlo has no TLD to complete a bare name with. On a fresh project with no `.servlo.yaml`, an interactive terminal routes through the `servlo init` wizard first (PHP version, HTTPS, services) before linking; prompts to import data when `laravel/sail` is detected in `composer.json`. **Non-PHP projects** (Node.js, Python, Go, etc.) must have `Containerfile.servlo` and `.servlo.yaml` with `container: {port: N}` already written before calling this, see Custom Containers |
| `servlo unlink [name]` | Stop serving the site |
| `servlo sites` | Table view of all registered sites |
| `servlo open [name]` | Open the site in the default browser |
| `servlo secure [name]` | Issue a TLS certificate and enable HTTPS, updates `APP_URL` in `.env` |
| `servlo secure --renew [name]` | Reissue a secured site's TLS cert on demand, resetting its expiry |
| `servlo secure --staging [name]` | Issue from Let's Encrypt staging from now on, for working out a DNS problem without spending the production rate limit |
| `servlo unsecure [name]` | Remove TLS and switch back to HTTP, updates `APP_URL` in `.env` |
| `servlo dns-provider set <provider>` | Store the DNS API credentials a wildcard certificate needs |
| `servlo dns-provider use <provider|http-01>` | Choose how certificates prove control of a domain |
| `servlo dns-provider list` | Show which DNS providers are configured, secrets redacted |
| `servlo dns-provider remove <provider>` | Forget a provider's credentials |
| `servlo pause [name]` | Pause a site: stop workers (and custom container if applicable), replace vhost with landing page |
| `servlo unpause [name]` | Resume a paused site: start container, restore vhost, restart workers |
| `servlo restart [name]` | Restart the container for the current or named site (custom container or PHP-FPM) |
| `servlo rebuild [name]` | Rebuild the custom container image from Containerfile and restart |
| `servlo group add <main> <label>` | Group the current site under `<main>` (name or domain) at `<label>.<main-domain>`; add `--share-db` to share the main's database. See [Site Groups](../usage/site-groups.md) |
| `servlo group label <label>` | Change the current secondary's subdomain label |
| `servlo group db <share\|separate>` | Switch the current secondary between sharing the main's database and keeping its own |
| `servlo group remove` | Ungroup the current secondary, restoring a standalone domain |
| `servlo group list` | List all site groups and their members |
| `servlo workspace add <name>` | Create an empty workspace, a display-only grouping of sites. See [Workspaces](../usage/sites.md#workspaces) |
| `servlo workspace rename <old> <new>` | Rename a workspace, keeping its sites |
| `servlo workspace rm <name>` | Delete a workspace; its sites stay linked and become ungrouped |
| `servlo workspace assign <site> <workspace\|none>` | Move a site into a workspace, or out of one with `none`; assign a group main, not a secondary |
| `servlo workspace move <name> <position>` | Reposition a workspace in the display order (`0` is first) |
| `servlo workspace list` | List the workspaces and their sites |
| `servlo env` | Configure `.env` for the current project with servlo service connection settings; backs up the original as `.env.before_servlo` on first run (skipped if servlo has already written to the file) |
| `servlo env:restore` | Restore `.env` from the pre-servlo backup (`.env.before_servlo`) |
| `servlo env:override [KEY=VALUE ...]` | Create/seed a personal, gitignored `.env.servlo_override` whose values win over servlo's defaults on `servlo env`; `SERVLO_EXTERNAL_SERVICES=` marks services servlo should not start or provision |
| `servlo env:check` | Compare all `.env` files against `.env.example` and flag missing or extra keys |

## Where things bind

There is no command here, and that is the point. Nginx serves the sites, so it
publishes on every interface; databases, caches and admin UIs publish on
loopback and nothing else. Neither half is configurable, because a server whose
sites answer nobody is not serving and a database anyone can reach is not safe.
See [Production mode](/features/production-mode).

What a signed-in operator may do in the panel is decided by their role and by
the permission each route declares, not by where they are: an Admin reads a
site's `.env`, browses the filesystem and drops a database from wherever they
signed in, and a Developer does none of those anywhere. Reaching the panel from
another machine at all needs credentials, set with `servlo remote-control on`.

## PHP

Supported PHP versions: **8.5**, **8.4**, **8.3**, **8.2**, **8.1**, and the frozen legacy tier **8.0** and **7.4**. The legacy tier is opt-in only (you have to `servlo use 7.4` or `servlo isolate 7.4` explicitly), pulls from `php:7.4-fpm-alpine` / `php:8.0-fpm-alpine` upstream tags, and intentionally skips ext-mongodb (unavailable on those PHP versions). Use the legacy tier for hosted legacy apps; default new projects to 8.4 LTS or 8.5.

| Command | Description |
|---|---|
| `servlo use <version>` | Set the global PHP version and build the FPM image if needed |
| `servlo isolate <version>` | Pin PHP version for cwd: writes `.php-version` and updates `.servlo.yaml` if present, then re-links |
| `servlo php:list` | List all installed PHP-FPM versions |
| `servlo php:rebuild [--local]` | Force-rebuild all installed PHP-FPM images (pulls pre-built base by default; `--local` builds from source) |
| `servlo fetch [version...] [--local]` | Pull pre-built PHP FPM base images from ghcr.io for the given (or all supported) versions; `--local` builds from source instead |
| `servlo php:ext add <ext> [--apk-deps PKG[,PKG]]` | Add a custom PHP extension to every PHP image and rebuild the current version. `--apk-deps` accepts additional Alpine packages that the extension needs at build time (e.g. `--apk-deps libwebp-dev,libpng-dev` for `gd` with WebP support); the package list is persisted in `~/.config/servlo/config.yaml` so future rebuilds reapply it |
| `servlo php:ext remove <ext>` | Remove a custom PHP extension from every PHP image and rebuild |
| `servlo php:ext list` | List your declared extensions, and what each PHP version's image actually loaded |
| `servlo php:pkg add <package...>` | Add extra Alpine packages to every FPM image and rebuild the current version; the list is persisted so future rebuilds reapply it |
| `servlo php:pkg remove <package...>` | Remove extra Alpine packages from every FPM image and rebuild |
| `servlo php:pkg list` | List your declared Alpine packages, and what each PHP version's image actually installed |
| `servlo php:ports add <host:container...> [--php VERSION]` | Publish extra host ports on the version's FPM container so a process in the container is reachable at `localhost:PORT`; a bare number publishes straight through, and a busy host port shifts to the next free one |
| `servlo php:ports remove <host...> [--php VERSION]` | Unpublish host ports from the version's shell container |
| `servlo php:ports list [--php VERSION]` | List the extra host ports published for a PHP version |
| `servlo php:ini [version\|shared]` | Open a PHP version's php.ini in `$EDITOR`, or the shared file (`php:ini shared`) applied to every version |
| `servlo pest:browser install [version]` | Set up in-container Pest browser testing: bake musl chromium into the FPM image, download the Playwright registry into a persistent volume, and shim Playwright's glibc browser to it |
| `servlo pest:browser remove [version]` | Remove chromium from the FPM image and disable Pest browser testing (the Playwright cache volume is left intact) |
| `servlo pest:browser doctor [version]` | Diagnose the Pest browser testing setup (plugin, chromium, playwright, shim) for a PHP version |
| `servlo php:bun install [version] [--pin VERSION]` | Install (or update) a musl bun into the container's persistent `/root/.bun` volume, shared across every PHP version; `--pin` fixes a specific bun version instead of latest |
| `servlo php:bun update [version]` | Update the container's bun in place (`bun upgrade`) |
| `servlo php:bun version [version]` | Show the bun version installed in the PHP-FPM container |
| `servlo php:bun remove` | Remove the in-container bun and clear its persistent volume |
| `servlo dump on` | Enable the debug bridge so `dump()` / `dd()` calls ship to the servlo dashboard and TUI |
| `servlo dump off` | Disable the debug bridge and restore FPM containers to their default state |
| `servlo dump status` | Show whether the bridge is enabled and how many events are buffered |
| `servlo dump tail [--site X] [--branch Y] [--ctx fpm\|cli]` | Stream captured dumps to the terminal until Ctrl-C |
| `servlo dump clear` | Clear the in-memory dump ring without disabling the bridge |
| `servlo notify on` | Enable servlo notifications globally (dashboard banners + Web Push fanout) |
| `servlo notify off` | Globally mute servlo notifications; bypasses per-device prefs |
| `servlo notify target <browser\|native>` | Choose the delivery sink: browser (WebSocket + Web Push) or native desktop notifications (Linux) |
| `servlo notify status` | Show whether notifications are globally enabled and the current delivery sink |

## Runtime

Switch the PHP runtime for the current site between shared PHP-FPM and per-site FrankenPHP. See the [FrankenPHP runtime](../features/frankenphp.md) page for adapters, worker mode, and limitations.

| Command | Description |
|---|---|
| `servlo runtime` | Print the current runtime for the site in cwd |
| `servlo runtime frankenphp` | Switch to per-site FrankenPHP (non-worker); writes `runtime: frankenphp` to `.servlo.yaml` |
| `servlo runtime frankenphp --worker` | Enable FrankenPHP with worker mode (Laravel Octane or Symfony's FrankenPHP adapter with `--watch`) |
| `servlo runtime frankenphp --no-worker` | Switch to FrankenPHP and explicitly disable worker mode |
| `servlo runtime fpm` | Back to shared PHP-FPM; clears the runtime field from `.servlo.yaml` |
| `servlo octane:reload [on\|off]` | Toggle Octane auto-reload on file changes (`octane:start --watch`) for the current FrankenPHP worker-mode site; with no argument prints the current state. Needs the `chokidar` npm package |

## Node

| Command | Description |
|---|---|
| `servlo node:install <version>` | Install a Node.js version globally via fnm |
| `servlo node:uninstall <version>` | Uninstall a Node.js version via fnm |
| `servlo node:use <version>` | Set the default Node.js version |
| `servlo isolate:node <version>` | Pin Node version for cwd: writes `.node-version`, runs `fnm install` |
| `servlo node [args...]` | Run `node` using the project's pinned version via fnm |
| `servlo npm [args...]` | Run `npm` using the project's pinned Node version via fnm |
| `servlo npx [args...]` | Run `npx` using the project's pinned Node version via fnm |
| `servlo js:runtime [bun\|node\|auto]` | Pin the current site's JS runtime in `.servlo.yaml` (the CLI equivalent of the dashboard's bun/Node toggle); with no argument prints the current runtime |

## Services

| Command | Description |
|---|---|
| `servlo service start <name>` | Start a service (auto-installs on first use) |
| `servlo service stop <name>` | Stop a service container |
| `servlo service restart <name>` | Restart a service container; refreshes the quadlet first so config edits take effect |
| `servlo service status <name>` | Show systemd unit status |
| `servlo service list` | All services with status, version, and an Update column showing pending updates |
| `servlo service update <name> [tag]` | Pull a newer image and restart; with no tag applies the safe in-strategy update, with a tag targets an explicit upgrade |
| `servlo service migrate <name> <target-tag>` | SQL dump + restore for cross-version mysql / postgres moves; old data dir and dump preserved under `~/.local/share/servlo/backups` |
| `servlo service rollback <name>` | Swap back to the previously-running image; toggles, so a second rollback redoes the update |
| `servlo service expose <name> <host:container>` | Publish an extra port on any bundled preset service (persisted, auto-restarts if running) |
| `servlo service expose <name> <host:container> --remove` | Remove a previously exposed port |
| `servlo service port <name> <port>` | Move a service's primary published host port without touching its container-internal port; persisted and auto-restarts if running |
| `servlo service port <name> <port> --container <cport>` | Move a specific mapping of a multi-port service (e.g. RustFS' `9001` console behind the `9000` S3 API primary), named by its container-internal port |
| `servlo service port <name> --reset` | Reset a service to its preset default published port (same as `port <name> 0`); combine with `--container` to reset one mapping |
| `servlo service pin <name>` | Pin a service so it is never auto-stopped when no sites use it |
| `servlo service unpin <name>` | Unpin a service so it can be auto-stopped when unused |
| `servlo service add [file.yaml]` | Register a new custom service (from a YAML file or flags) |
| `servlo service preset [name]` | List presets, or install one (use `--version` for multi-version presets); a store-only preset is fetched on demand |
| `servlo service search [query]` | Browse the external service-preset store; filter by name, description, or family |
| `servlo service remove <name> [--purge]` | Stop and remove a service (custom or default). With `--purge`, also rename the data dir aside (recoverable as `<name>.pre-remove-<ts>`) |
| `servlo service reinstall <name> [--reset-data]` | Stop, remove, and reinstall at the current version. With `--reset-data`, rename the data dir aside and recreate linked sites' databases or buckets on the fresh service |
| `servlo minio:migrate` | Migrate existing MinIO data to RustFS |

## Database

| Command | Description |
|---|---|
| `servlo db:create [name]` | Create a database and a `<name>_testing` database |
| `servlo db:import [-d name] <file.sql>` | Import a SQL dump (defaults to site DB from `.env`) |
| `servlo db:export [-d name] [-o file.sql]` | Export a database to a SQL dump (defaults to site DB from `.env`) |
| `servlo db:shell` | Open an interactive MySQL or PostgreSQL shell |
| `servlo db:snapshot [name] [-A]` | Create a named, restorable snapshot of a database |
| `servlo db:snapshots [--all]` | List stored database snapshots |
| `servlo db:restore <name> [-A] [-f]` | Restore a database from a stored snapshot |
| `servlo db:snapshot:rm <name> [-A]` | Delete a stored database snapshot |
| `servlo db:move [--from svc] [--to svc] [--all\|--site name]` | Move sites' databases between two installed services in the same family and repoint their `.env`; wizard when run without flags |
| `servlo db user [site]` | Show the database account a site reaches its database as |
| `servlo db user rotate [site]` | Issue a new password for that account and write it into the site's env file |

## Import

| Command | Description |
|---|---|
| `servlo import sail` | Import database and S3/MinIO files from a Laravel Sail project into servlo |
| `servlo sail import` | Alias, natural order when already in a Sail project (`servlo sail <anything-else>` proxies to `vendor/bin/sail`) |
| `servlo import sail --skip-s3` | Import database only, skip S3/MinIO file mirroring |
| `servlo import sail --no-stop` | Leave Sail running after import completes |
| `servlo import sail --sail-db-name <name>` | Override the Sail-side database name (auto-detected by default) |

See Importing from Laravel Sail for full documentation.

## Queue workers

| Command | Description |
|---|---|
| `servlo queue:start` | Start a queue worker for the current project |
| `servlo queue:stop` | Stop the queue worker for the current project |

## Horizon

For projects that use `laravel/horizon`, servlo detects it automatically from `composer.json`.

| Command | Description |
|---|---|
| `servlo horizon:start` | Start Laravel Horizon for the current project as a persistent background service |
| `servlo horizon:stop` | Stop Horizon |
| `servlo horizon:reload [on\|off]` | Toggle Horizon auto-reload on file changes for the current site; with no argument prints the current state. Needs the `chokidar` npm package |

## Reverb

Requires [Laravel Broadcasting](https://laravel.com/docs/13.x/broadcasting) with the `laravel/reverb` package, servlo detects it automatically from `composer.json`.

| Command | Description |
|---|---|
| `servlo reverb:start` | Start the Reverb WebSocket server for the current project as a persistent background service |
| `servlo reverb:stop` | Stop the Reverb server |

## Schedule

| Command | Description |
|---|---|
| `servlo schedule:start` | Start the task scheduler (`schedule:work`) for the current project as a persistent background service |
| `servlo schedule:stop` | Stop the task scheduler |

## Framework workers

| Command | Description |
|---|---|
| `servlo worker start <name>` | Start any named framework worker for the current project |
| `servlo worker stop <name>` | Stop a named framework worker |
| `servlo worker list` | List all workers defined for the current project's framework |

## Framework definitions

| Command | Description |
|---|---|
| `servlo framework list` | List all available framework definitions and their workers |
| `servlo framework add <name>` | Install a published framework from the store, or author a custom one (flags or `--from-file`) |
| `servlo framework remove <name>` | Remove a framework definition (confirms if a site still uses it) |
| `servlo framework prune` | Remove installed definitions no site uses |

## Stripe

| Command | Description |
|---|---|
| `servlo stripe:listen` | Start a Stripe webhook listener for the current project as a background service |
| `servlo stripe:listen stop` | Stop the Stripe webhook listener |
| `servlo stripe:config` | Show or set the webhook path and secret env key in `.servlo.yaml` without starting the listener |

## Authentication

| Command | Description |
|---|---|
| `servlo auth ssh [key...]` | Load SSH keys into a shared `servlo-ssh-agent` sidecar so `servlo composer` can reach private git repositories, including passphrase-protected keys. Defaults to `~/.ssh/id_*`. The agent socket lives on a named volume shared into the FPM containers, so it is reachable from inside them. Unlocked keys stay in the agent's memory and clear when it stops |
| `servlo auth ssh --list` | List the keys currently loaded into the agent |
| `servlo auth ssh --remove` | Remove all keys and stop the agent |

## Console & runtime passthrough

| Command | Description |
|---|---|
| `servlo console [args...]` | Run the framework's console command (e.g., `php artisan` for Laravel, `php bin/console` for Symfony) inside the project's PHP-FPM container |
| `servlo artisan [args...]` | Alias for `servlo console`, equivalent to `php artisan` since the `php` shim also runs inside the FPM container |
| `servlo a [args...]` | Short alias for `servlo console` / `servlo artisan` |
| `servlo test [args...]` | Shortcut for `servlo artisan test` |
| `servlo <vendor-bin> [args...]` | Run any composer-installed binary from the project's `vendor/bin` directory (e.g. `servlo pest`, `servlo pint`, `servlo phpstan`). Real servlo commands always win over vendor binaries with the same name. |

## Dashboard

| Command | Description |
|---|---|
| `servlo dashboard` | Open the Servlo dashboard (`http://127.0.0.1:7073`) in the default browser |

## Shell completion

```bash
servlo completion bash   # add to ~/.bashrc
servlo completion zsh    # add to ~/.zshrc
servlo completion fish   # add to ~/.config/fish/completions/servlo.fish
```
