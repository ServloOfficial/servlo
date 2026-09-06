# Command Reference

Every command the `servlo` binary answers to, generated against the binary itself
so it cannot drift from what is installed. Run `servlo <command> --help` for the
flags, which are not repeated here.

::: info Some features have no command
Deploys, cron entries, per-site SMTP, the firewall and fail2ban are managed from
the dashboard rather than the CLI. They are as real as anything here — there is
simply no command to type. Their pages say so where it matters:
[Deploy](/usage/deploy), [Cron](/usage/cron), [Email](/usage/email) and
[Security](/usage/security).
:::

## Install and server setup

One-time setup, and the commands that keep the machine itself in shape. `servlo install` refuses rather than half-installing if Ubuntu, Podman, crun, cgroup v2 or linger are not right, and prints any command needing root for you to run rather than invoking sudo itself.

| Command | Description |
|---|---|
| `servlo install` | Run one-time Servlo setup |
| `servlo bootstrap` | Apply the root-level system setup (for package maintainer scripts) |
| `servlo uninstall` | Remove Servlo and all its components |
| `servlo update` | Update Servlo to the latest release |
| `servlo whatsnew` | Show what changed in the latest release |
| `servlo tools:update` | Update composer and fnm to their pinned versions |
| `servlo doctor` | Diagnose your Servlo environment and report issues |
| `servlo harden` | Audit this server's exposure and print what to fix |
| `servlo fetch` | Pre-build PHP FPM images so first use isn't slow |

## Running Servlo

Day-to-day lifecycle. See [Start, Stop & Autostart](/usage/lifecycle) for what each one starts and stops.

| Command | Description |
|---|---|
| `servlo start` | Start Servlo (nginx, PHP-FPM, and installed services) |
| `servlo stop` | Stop Servlo containers (nginx, PHP-FPM, and running services) |
| `servlo quit` | Stop all Servlo processes and containers (including UI and watcher) |
| `servlo status` | Show overall Servlo health status |
| `servlo autostart` | Manage autostart on login |
| `servlo autostart disable` | Disable servlo autostart on login |
| `servlo autostart enable` | Enable servlo autostart on login |
| `servlo alerts` | What is currently wrong with this server |
| `servlo alerts clear` | Take an alert off the list by hand |
| `servlo production` | Show or change production mode |
| `servlo production off` | Turn production mode off (needs --force) |
| `servlo production on` | Turn production mode on |
| `servlo cleanup` | Reclaim podman disk from orphaned servlo images, unused service images, and dangling leftovers |
| `servlo cleanup auto` | Enable, disable, or show automatic cleanup |
| `servlo cleanup auto off` | Disable automatic cleanup |
| `servlo cleanup auto on` | Enable automatic cleanup |
| `servlo cleanup auto status` | Show whether automatic cleanup is on |

## Sites

A site is a directory served on a real domain. There are four ways to create one — clone from Git, upload a ZIP, point at a folder already on the server, or install an app — and the ones with a CLI form are here. See [Site management](/usage/sites).

| Command | Description |
|---|---|
| `servlo sites` | List all registered sites |
| `servlo new` | Scaffold a new PHP project |
| `servlo init` | Initialize a project: run the setup wizard and save .servlo.yaml |
| `servlo link` | Link the current directory as a site |
| `servlo unlink` | Unlink the current directory site |
| `servlo park` | Park a directory to serve all subdirectories as sites |
| `servlo unpark` | Remove a parked directory and unlink all its sites |
| `servlo setup` | Bootstrap a PHP project (composer, npm, env, migrate, assets, open) |
| `servlo check` | Validate .servlo.yaml — PHP version, services, workers, container config, custom_workers, and db |
| `servlo which` | Show resolved PHP, Node, document root, and nginx config for the current site |
| `servlo site:doctor` | Run app-level health checks for a site |
| `servlo open` | Open the current site in the default browser. Needs a desktop on the machine running it, so on a headless server it has nothing to open — use the URL |
| `servlo pause` | Pause a site: stop its workers and replace the vhost with a landing page |
| `servlo unpause` | Resume a paused site: restore its vhost and restart previously running workers |
| `servlo group` | Group the current site under a main site as a subdomain |
| `servlo group add` | Group the current site under &lt;main-site&gt; at the &lt;label&gt; subdomain |
| `servlo group db` | Switch the current secondary between sharing the main's database and its own |
| `servlo group label` | Change the subdomain label of the current secondary site |
| `servlo group list` | List all site groups and their members |
| `servlo group remove` | Ungroup the current secondary site, restoring a standalone domain |
| `servlo workspace` | Group sites into workspaces for the dashboard and the TUI |
| `servlo workspace add` | Create an empty workspace |
| `servlo workspace assign` | Move a site into a workspace, or out of one with 'none' |
| `servlo workspace list` | List the workspaces and their sites |
| `servlo workspace move` | Reposition a workspace in the display order (0 is first) |
| `servlo workspace rename` | Rename a workspace, keeping its sites |
| `servlo workspace rm` | Delete a workspace; its sites become ungrouped |
| `servlo apps` | Install an application as a new site |
| `servlo apps install` | Fetch an application, configure it, and serve it on a domain |
| `servlo import` | Import data from other environments |
| `servlo import sail` | Import database (and S3 files) from a Laravel Sail project |
| `servlo import site` | Import a site that is already running somewhere else |
| `servlo staging` | A copy of a live site, not indexed and behind a password |
| `servlo staging create` | Make a staging copy of a live site |
| `servlo staging password` | Give a staging site a new password |
| `servlo staging refresh` | Copy the live site over its staging copy |

## Domains and HTTPS

Each site has a primary domain and any number of aliases; every one of them is included in the certificate's SANs and in the DNS pre-flight check. Servlo runs no resolver and never touches this machine's resolver configuration — pointing a domain at this server is DNS you hold at your registrar. See [Domains](/usage/domains) and [HTTPS / TLS](/features/https).

| Command | Description |
|---|---|
| `servlo domain` | Manage domains for the current site |
| `servlo domain add` | Add a domain to the current site. Give the full domain, e.g. `example.com` |
| `servlo domain list` | List domains for the current site |
| `servlo domain remove` | Remove a domain from the current site |
| `servlo secure` | Enable HTTPS for the current site |
| `servlo unsecure` | Disable HTTPS for the current site |
| `servlo dns-provider` | Manage the DNS credentials wildcard certificates need |
| `servlo dns-provider list` | Show which DNS providers have credentials |
| `servlo dns-provider remove` | Forget a DNS provider's credentials |
| `servlo dns-provider set` | Record the credentials for a DNS provider |
| `servlo dns-provider use` | Choose how certificates prove control of a domain |

## PHP

Each site gets its own PHP-FPM pool, so a PHP setting is per-site rather than shared across every site on that version. See [PHP](/usage/php).

Supported versions are **8.5**, **8.4**, **8.3**, **8.2** and **8.1**, plus a frozen legacy tier of **8.0** and **7.4**. The legacy tier is opt-in — you have to ask for it with `servlo use 7.4` or `servlo isolate 7.4` — and it is pinned rather than maintained: built from Alpine 3.16, without ext-mongodb, and **not security-updated**. Use it for a legacy application you are hosting, not for anything new. FrankenPHP publishes no image below 8.2, so a site on 8.1 or the legacy tier runs under PHP-FPM only.

| Command | Description |
|---|---|
| `servlo php` | Run PHP in the project's container (e.g. servlo php artisan migrate) |
| `servlo php:list` | List installed PHP versions |
| `servlo php:ini` | Edit the user php.ini for a PHP version, or the shared file (php:ini shared) |
| `servlo php:ext` | Manage custom PHP extensions |
| `servlo php:ext add` | Install a custom PHP extension on every PHP version |
| `servlo php:ext list` | List your custom PHP extensions and where they did not build |
| `servlo php:ext remove` | Remove a custom PHP extension from every PHP version |
| `servlo php:pkg` | Manage extra Alpine packages in the PHP-FPM image |
| `servlo php:pkg add` | Add Alpine packages to every PHP-FPM image |
| `servlo php:pkg list` | List your extra Alpine packages and where they did not install |
| `servlo php:pkg remove` | Remove extra Alpine packages from every PHP-FPM image |
| `servlo php:bun` | Manage an optional bun runtime inside the PHP-FPM container |
| `servlo php:bun install` | Install (or update) bun inside the PHP-FPM container |
| `servlo php:bun remove` | Remove the in-container bun and clear its persistent volume |
| `servlo php:bun update` | Update the container's bun in place (bun upgrade) |
| `servlo php:bun version` | Show the bun version installed in the PHP-FPM container |
| `servlo php:ports` | Publish extra host ports on a PHP version's shell container |
| `servlo php:ports add` | Publish a host:container port and restart the version's FPM |
| `servlo php:ports list` | List extra host ports published for a PHP version |
| `servlo php:ports remove` | Unpublish a host port and restart the version's FPM |
| `servlo php:rebuild` | Force-rebuild PHP-FPM image(s) |
| `servlo use` | Set the global PHP version |
| `servlo isolate` | Pin the PHP version for the current directory |
| `servlo runtime` | Switch the PHP runtime for the current site (fpm or frankenphp) |
| `servlo rebuild` | Rebuild the custom container image and restart the container |
| `servlo restart` | Restart the container for the current or named site |
| `servlo pest:browser` | Set up in-container Pest browser testing (Playwright on musl chromium) |
| `servlo pest:browser doctor` | Diagnose the Pest browser testing setup for a PHP version |
| `servlo pest:browser install` | Bake musl chromium into the FPM image and wire up Playwright for Pest |
| `servlo pest:browser remove` | Remove chromium from the FPM image and disable Pest browser testing |

## Node and JavaScript

A Node version per site, and the shims that put it on your PATH. See [Node](/usage/node).

| Command | Description |
|---|---|
| `servlo node` | Run node using the project's version |
| `servlo node:install` | Install a Node.js version |
| `servlo node:use` | Set the default Node.js version |
| `servlo node:uninstall` | Uninstall a Node.js version |
| `servlo node:manage` | Let servlo manage Node.js (install shims and a default version) |
| `servlo node:manager` | Show or switch the Node version manager servlo drives |
| `servlo node:unmanage` | Stop managing Node.js: remove servlo's node shims and fnm-installed versions |
| `servlo npm` | Run npm using the project's node version |
| `servlo npx` | Run npx using the project's node version |
| `servlo isolate:node` | Pin the Node.js version for the current directory |
| `servlo js:runtime` | Pin the JS runtime (bun or Node) for the current site |
| `servlo path:enable` | Put servlo's shims (php, composer, node…) back on your shell PATH |
| `servlo path:disable` | Take servlo's shims (php, composer, node…) off your shell PATH |

## Nginx

Every save runs `nginx -t` before committing and keeps a timestamped backup, so a bad edit cannot take a site down. See [Nginx overrides](/usage/nginx-overrides).

| Command | Description |
|---|---|
| `servlo nginx` | Show, edit, or reset a site's custom nginx override |
| `servlo nginx edit` | Open the custom nginx override in $EDITOR, then validate and reload |
| `servlo nginx reset` | Delete the custom nginx override and reload nginx (backups are kept) |
| `servlo nginx show` | Print the custom nginx override (or its file path with --path) |

## Databases

A database is a connection, not necessarily a container: local MySQL, MariaDB or PostgreSQL, or an external managed database. Both are first-class. See [Database](/usage/database).

| Command | Description |
|---|---|
| `servlo db` | Database shortcuts for the current site |
| `servlo db create` | Create a database (and testing database) for the current project |
| `servlo db export` | Export a database to a SQL dump (default: site DB from .env) |
| `servlo db extension` | List or add database extensions the engine can create |
| `servlo db extension add` | Create an extension in the database |
| `servlo db extension list` | Show which extensions the engine offers and which the database has |
| `servlo db import` | Import a SQL dump into a database (default: site DB from .env) |
| `servlo db move` | Move site databases from one service to another in the same family |
| `servlo db restore` | Restore a database from a stored snapshot |
| `servlo db shell` | Open an interactive database shell for the current project |
| `servlo db snapshot` | Create a named snapshot of the project database |
| `servlo db snapshot:rm` | Delete a stored database snapshot |
| `servlo db snapshots` | List stored database snapshots |
| `servlo db user` | The database account a site reaches its database as |
| `servlo db user rotate` | Give the site's database account a new password |
| `servlo db:connection` | Manage the databases sites can be put on, local or managed |
| `servlo db:connection add` | Add a connection, local or managed |
| `servlo db:connection default` | Put new sites on this connection |
| `servlo db:connection list` | List the configured connections |
| `servlo db:connection rm` | Remove a connection |
| `servlo db:connection test` | Open a connection and report what happened |
| `servlo db:extension` | List or add database extensions the engine can create |
| `servlo db:extension add` | Create an extension in the database |
| `servlo db:extension list` | Show which extensions the engine offers and which the database has |
| `servlo db:create` | Create a database (and testing database) for the current project |
| `servlo db:export` | Export a database to a SQL dump (default: site DB from .env) |
| `servlo db:import` | Import a SQL dump into a database (default: site DB from .env) |
| `servlo db:move` | Move site databases from one service to another in the same family |
| `servlo db:restore` | Restore a database from a stored snapshot |
| `servlo db:shell` | Open an interactive database shell for the current project |
| `servlo db:snapshot` | Create a named snapshot of the project database |
| `servlo db:snapshots` | List stored database snapshots |
| `servlo db:snapshot:rm` | Delete a stored database snapshot |

## Services

Services come from the YAML preset store rather than from Go code, so a new one is a store change and not a release. They bind to the container network only and never publish to a public interface. See [Services](/usage/services).

| Command | Description |
|---|---|
| `servlo service` | Manage Servlo services (mysql, redis, postgres, meilisearch, rustfs) |
| `servlo service add` | Define a new custom service (from a YAML file or flags) |
| `servlo service config` | Edit a service's runtime tuning override (e.g. my.cnf for mysql/mariadb) |
| `servlo service expose` | Add (or remove) an extra published port on a built-in service |
| `servlo service list` | List all services and their status |
| `servlo service migrate` | Migrate a service across data-incompatible versions (dump + restore) |
| `servlo service pin` | Pin a service so it is never auto-stopped |
| `servlo service port` | Set or reset a service's published host port |
| `servlo service preset` | Install a bundled service preset (e.g. phpmyadmin, pgadmin) |
| `servlo service reinstall` | Stop, remove, and reinstall a service in place |
| `servlo service remove` | Stop and remove a service (custom or default) |
| `servlo service restart` | Restart a service |
| `servlo service rollback` | Roll back a service to its previously-running image |
| `servlo service search` | Search the external service-preset store |
| `servlo service start` | Start a service |
| `servlo service status` | Show the status of a service |
| `servlo service stop` | Stop a service |
| `servlo service unpin` | Unpin a service so it can be auto-stopped when unused |
| `servlo service update` | Pull a newer image for a service and restart it |
| `servlo shims` | Manage the client-tool shims services expose on your PATH |
| `servlo shims add` | Install the host shim for a client tool (e.g. mysqldump) |
| `servlo shims list` | List client-tool shims and whether each is installed |
| `servlo shims remove` | Remove the host shim for a client tool |
| `servlo minio:migrate` | Migrate MinIO data to RustFS |

## Workers

Queue, schedule, Horizon, Reverb and framework-defined workers, each supervised as a systemd user unit with failure detection and one-click healing. See [Queue workers](/usage/queue-workers) and [Healing failed workers](/usage/worker-heal).

Horizon and Reverb are detected from `composer.json` rather than configured: a project requiring `laravel/horizon` or `laravel/reverb` gets the commands without being told. The colon spellings (`servlo queue:start`) and the subcommand spellings (`servlo queue start`) are the same command.

| Command | Description |
|---|---|
| `servlo queue` | Manage queue workers for the current site |
| `servlo queue start` | Start a queue worker for the current site as a systemd service |
| `servlo queue stop` | Stop the queue worker for the current site |
| `servlo queue:start` | Start a queue worker for the current site as a systemd service |
| `servlo queue:stop` | Stop the queue worker for the current site |
| `servlo horizon` | Manage Laravel Horizon for the current site |
| `servlo horizon reload` | Toggle Horizon auto-reload on file changes (horizon:listen) for the current site |
| `servlo horizon start` | Start Laravel Horizon for the current site as a systemd service |
| `servlo horizon stop` | Stop Laravel Horizon for the current site |
| `servlo horizon:start` | Start Laravel Horizon for the current site as a systemd service |
| `servlo horizon:stop` | Stop Laravel Horizon for the current site |
| `servlo horizon:reload` | Toggle Horizon auto-reload on file changes (horizon:listen) for the current site |
| `servlo schedule` | Manage the Laravel task scheduler for the current site |
| `servlo schedule start` | Start the Laravel task scheduler for the current site as a systemd service |
| `servlo schedule stop` | Stop the Laravel task scheduler for the current site |
| `servlo schedule:start` | Start the Laravel task scheduler for the current site as a systemd service |
| `servlo schedule:stop` | Stop the Laravel task scheduler for the current site |
| `servlo reverb` | Manage the Laravel Reverb WebSocket server for the current site |
| `servlo reverb start` | Start the Laravel Reverb WebSocket server for the current site as a systemd service |
| `servlo reverb stop` | Stop the Laravel Reverb WebSocket server for the current site |
| `servlo reverb:start` | Start the Laravel Reverb WebSocket server for the current site as a systemd service |
| `servlo reverb:stop` | Stop the Laravel Reverb WebSocket server for the current site |
| `servlo octane` | Manage Laravel Octane (FrankenPHP worker mode) for the current site |
| `servlo octane reload` | Toggle Octane auto-reload on file changes (octane:start --watch) for the current site |
| `servlo octane:reload` | Toggle Octane auto-reload on file changes (octane:start --watch) for the current site |
| `servlo worker` | Manage framework-defined workers for the current site |
| `servlo worker add` | Add a custom worker to this project or global framework overlay |
| `servlo worker heal` | Reset failed worker units and start them again |
| `servlo worker list` | List workers defined for the current site's framework |
| `servlo worker remove` | Remove a custom worker from .servlo.yaml or global framework overlay |
| `servlo worker start` | Start a framework worker as a systemd service |
| `servlo worker stop` | Stop a framework worker |
| `servlo workers` | Inert on Ubuntu. Left over from macOS support; the setting it manages is ignored on Linux, which always runs workers under systemd |
| `servlo workers mode` | Inert on Ubuntu, as above |
| `servlo stripe:config` | Show or set the Stripe webhook path and secret env key for the current site (without starting the listener) |
| `servlo stripe:listen` | Start a Stripe webhook listener for the current site as a systemd service |

## Backups and restore

A backup is one encrypted archive holding a site's files and a dump of its database. A backup that has never been restored is not a backup, which is why `backup verify` restores into a scratch database and checks what came back. See [Backups](/usage/backups).

| Command | Description |
|---|---|
| `servlo backup` | Back up a site: its files and its database, encrypted |
| `servlo backup destination` | Where finished archives are copied to |
| `servlo backup destination add` | Add a destination archives are copied to |
| `servlo backup destination remove` | Stop copying archives to a destination |
| `servlo backup destination test` | Check a destination can be reached |
| `servlo backup key` | Where this server's backup key is, and why it matters |
| `servlo backup key import` | Bring the backup key over from another server |
| `servlo backup list` | The backups on this server |
| `servlo backup schedule` | Back this site up on a schedule |
| `servlo backup state` | Back up servlo's own configuration and the site registry |
| `servlo backup verify` | Restore a backup into a scratch database and check what came back |
| `servlo restore` | Restore a site from a backup archive |

## File access

Per-site SFTP, confined to that site's directory. See [SFTP](/usage/sftp).

| Command | Description |
|---|---|
| `servlo sftp` | Per-site SFTP access: authorised keys, and the root setup to print |
| `servlo sftp add` | Authorise a public key for one site |
| `servlo sftp remove` | Withdraw an authorised key |
| `servlo sftp setup` | Print the root commands that confine each site's SFTP session |
| `servlo sftp status` | Show which sites have SFTP keys and whether sshd confines them |

## The panel

Reaching the dashboard, and who may sign in to it. See [Panel access](/features/panel-access) and [Panel authentication](/features/panel-authentication).

| Command | Description |
|---|---|
| `servlo panel` | Show or change how the Servlo panel is reached |
| `servlo panel domain` | Show, set or remove the panel's domain |
| `servlo panel domain remove` | Stop serving the panel on its domain |
| `servlo panel domain secure` | Issue a real certificate for the panel's domain |
| `servlo panel domain set` | Serve the panel on a domain |
| `servlo users` | Manage who can sign in to the panel |
| `servlo users add` | Add a panel account |
| `servlo users list` | List the panel accounts |
| `servlo users password` | Change an account's password |
| `servlo users remove` | Remove a panel account |
| `servlo users role` | Change an account's role |
| `servlo users sites` | Set which sites a developer may work on, or list them |
| `servlo users totp` | Turn the second factor on or off for an account |
| `servlo users totp codes` | Issue a fresh set of recovery codes, replacing the old ones |
| `servlo users totp disable` | Turn the second factor off, which is the way back in from a lost phone |
| `servlo users totp enable` | Enrol an authenticator app for an account |
| `servlo sessions` | List or end signed-in panel sessions |
| `servlo sessions list` | List signed-in sessions |
| `servlo sessions revoke` | End a session, or every session with --all |
| `servlo remote-control` | Toggle dashboard access from remote clients (off by default) |
| `servlo remote-control off` | Disable remote access to the dashboard |
| `servlo remote-control on` | Enable remote access to the dashboard with HTTP Basic auth |
| `servlo remote-control status` | Show whether remote access to the dashboard is enabled |
| `servlo dashboard` | Open the Servlo dashboard in a browser |
| `servlo tui` | Open a terminal dashboard for sites, services, and workers |
| `servlo notify` | Globally enable or disable servlo notifications |
| `servlo notify off` | Disable notifications globally |
| `servlo notify on` | Enable notifications globally |
| `servlo notify status` | Show whether notifications are globally enabled |
| `servlo audit` | Show what changed on this machine, and who changed it |

## Environment and credentials

Writing service connection settings into a site's `.env`, and sharing host SSH keys with the project containers. See [Environment setup](/features/env-setup).

| Command | Description |
|---|---|
| `servlo env` | Configure .env for this project with servlo service connection settings |
| `servlo env:check` | Compare all .env files against .env.example and flag missing keys |
| `servlo env:override` | Create/edit a personal, gitignored per-project .env override file |
| `servlo env:restore` | Restore .env from the pre-servlo backup (.env.before_servlo) |
| `servlo auth` | Share host credentials (SSH keys) with the project containers |
| `servlo auth ssh` | Load SSH keys into a shared agent so composer can use git over SSH |

## Logs

Application, nginx, PHP-FPM and worker logs, with rotation and retention. See [Logs](/usage/logs).

| Command | Description |
|---|---|
| `servlo logs` | Show logs for the current project's PHP-FPM container, nginx, or a service |
| `servlo logs keep` | How much log to keep |
| `servlo logs rotate` | Rotate every site's logs now |

## Frameworks

Framework behaviour lives in versioned YAML in the store, never in Go, so a new framework is a store change. See [Framework definitions](/usage/framework-definitions).

`servlo setup` runs the common steps — composer install, npm install, `servlo env` — followed by whatever the framework definition lists in its `setup` field, which is where migrations and storage links come from. `servlo sail` is an import shortcut; see [Importing a site](/usage/import).

| Command | Description |
|---|---|
| `servlo framework` | Manage framework definitions |
| `servlo framework add` | Install a framework from the store, or author a custom definition |
| `servlo framework list` | List all available framework definitions |
| `servlo framework prune` | Remove installed framework definitions no site uses |
| `servlo framework remove` | Remove a framework definition (user-defined or store-installed) |
| `servlo framework search` | Search the framework store for available definitions |
| `servlo framework update` | Update framework definitions from the store |
| `servlo run` | Run a framework command (artisan optimize:clear, drush cr, etc.) in the current site |
| `servlo console` | Run framework console command in the project's container |
| `servlo test` | Run framework tests (shortcut for `servlo artisan test`) |
| `servlo composer` | Run composer in the project's container, syncing composer-global bins onto PATH |
| `servlo sail` | Sail import shortcut; other args are passed to vendor/bin/sail |
| `servlo sail import` | Import database (and S3 files) from a Laravel Sail project |

## Help and diagnostics

Shell completion is generated rather than shipped:

```bash
servlo completion bash   # add to ~/.bashrc
servlo completion zsh    # add to ~/.zshrc
servlo completion fish   # add to ~/.config/fish/completions/servlo.fish
```


| Command | Description |
|---|---|
| `servlo about` | Show information about Servlo |
| `servlo man` | Browse the Servlo documentation |
| `servlo bug-report` | Collect diagnostics into a single file for GitHub bug reports |
| `servlo completion` | Generate the autocompletion script for the specified shell |
| `servlo completion bash` | Generate the autocompletion script for bash |
| `servlo completion fish` | Generate the autocompletion script for fish |
| `servlo completion powershell` | Generate the autocompletion script for powershell |
| `servlo completion zsh` | Generate the autocompletion script for zsh |
## Where things bind

There is no command here, and that is the point. Nginx serves the sites, so it
publishes on every interface; databases, caches and admin UIs publish on
loopback and nothing else. Neither half is configurable, because a server whose
sites answer nobody is not serving and a database anyone can reach is not safe.
See [Production mode](/features/production-mode).

## What a role may do

What a signed-in operator may do in the panel is decided by their role and by
the permission each route declares, not by where they are: an Admin reads a
site's `.env`, browses the filesystem and drops a database from wherever they
signed in, and a Developer does none of those anywhere. Reaching the panel from
another machine at all needs credentials, set with `servlo remote-control on`.
