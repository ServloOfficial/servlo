# Queue Workers & Framework Workers

Servlo can run framework-defined workers as persistent systemd user services. Workers run inside the project's PHP-FPM container and restart automatically on failure.

## Queue worker

| Command | Description |
|---|---|
| `servlo queue:start` | Start the queue worker for the current project |
| `servlo queue:stop` | Stop the queue worker for the current project |
| `servlo queue start` | Same as `queue:start` (subcommand form) |
| `servlo queue stop` | Same as `queue:stop` (subcommand form) |

Works for any framework that defines a `queue` worker: Laravel (`php artisan queue:work`, built-in) and CodeIgniter (`php spark queue:work`, once `codeigniter4/queue` is installed). The `--queue`, `--tries`, and `--timeout` flags are rendered into each framework's own syntax, so `servlo queue:start --queue emails --tries 5` runs the right command either way.

---

## Laravel Horizon

If `laravel/horizon` is present in `composer.json`, servlo detects it automatically and switches to Horizon mode:

- The queue toggle in the web UI is replaced by a **Horizon** toggle
- Use `servlo horizon:start` / `servlo horizon:stop` instead of `queue:start` / `queue:stop`

| Command | Description |
|---|---|
| `servlo horizon:start` | Start Horizon for the current project as a systemd service |
| `servlo horizon:stop` | Stop Horizon for the current project |
| `servlo horizon:reload [on\|off]` | Toggle auto-reload on file changes (prints the current state with no argument) |
| `servlo horizon start` | Same as `horizon:start` (subcommand form) |
| `servlo horizon stop` | Same as `horizon:stop` (subcommand form) |
| `servlo horizon reload [on\|off]` | Same as `horizon:reload` (subcommand form) |

Horizon manages its own worker pools via `config/horizon.php` and does not accept `--queue`, `--tries`, or `--timeout` flags. Those are configured in the Horizon config file instead.

The systemd unit is named `servlo-horizon-{sitename}`. Logs:
```bash
journalctl --user -u servlo-horizon-my-app -f
```

### Auto-reload on file changes

By default servlo runs `php artisan horizon`, which boots the app once and caches your code, so after editing a job, listener, or any class a worker touches you have to restart Horizon for the change to take effect.

Turn on auto-reload to run `php artisan horizon:listen` instead. Horizon then watches your project and restarts its workers automatically whenever a file changes, so you never stop/restart Horizon by hand while developing. The dashboard and `config/horizon.php` keep working exactly the same.

```bash
servlo horizon:reload on    # use horizon:listen (auto-restart on file changes)
servlo horizon:reload off   # back to standard horizon
servlo horizon:reload       # show the current state
```

Auto-reload is off by default. The preference is per project, stored as `reload_workers` in the project's `.servlo.yaml` (a list of worker names opted into reload mode, currently just `horizon`), so one project can develop with auto-reload while another stays in standard mode. In the dashboard the Horizon toggle and the reload toggle sit together as one grouped control, and the reload toggle only appears while Horizon is running, since reload is a property of a live worker. Either way, the running Horizon worker for the project is restarted so the change applies immediately, and the new state is pushed to every open dashboard over the websocket.

Three notes:

- The watcher shells out to Node and resolves [`chokidar`](https://www.npmjs.com/package/chokidar) from your project's `node_modules`. Horizon ships the watcher script but not chokidar itself, so the project has to provide it. It used to arrive for free as a transitive dependency of Vite, but Vite 8 dropped it, so a plain `npm install` is no longer enough. If chokidar is missing, the toggle never silently reads as on: from the dashboard, enabling pops a modal that offers a one-click `npm install --save-dev chokidar` and then turns reload on once the watcher is present; from the CLI, `servlo horizon:reload on` refuses with the same `npm install -D chokidar` hint.
- servlo adds `--poll` where the container can't see host filesystem events: on macOS, where workers run in the podman virtual machine, and under WSL2, where projects on `/mnt` (9p) mounts get no inotify delivery. On native Linux the container shares the host filesystem directly and inotify works, so polling is left off to avoid the wasted CPU.
- Where the watcher does poll, servlo re-stats watched files once a second rather than at chokidar's default of ten times a second, which is the difference between a busy virtiofs mount and an idle one. On a laptop the interval also follows how the machine is powered: running from the battery doubles it, and asking for less background work outright (macOS Low Power Mode, the power-saver profile on Linux, read from UPower and power-profiles-daemon) doubles it again. Unplugging mid-session restarts the affected reload workers so the new cadence applies straight away, at most once every five minutes.

## Generic workers (`servlo worker`)

Use this for any other framework-defined worker:

| Command | Description |
|---|---|
| `servlo worker start <name>` | Start a named worker for the current project |
| `servlo worker stop <name>` | Stop a named worker |
| `servlo worker list` | List all workers defined for this project's framework |

Example, start the Symfony Messenger consumer:
```bash
servlo worker start messenger
# Systemd unit: servlo-messenger-myapp.service
# Logs: journalctl --user -u servlo-messenger-myapp -f
```

Workers are defined in framework YAML definitions at `~/.config/servlo/frameworks/`. See [Frameworks](frameworks.md) for how to add custom workers to any framework.

---

## Options for `queue:start`

| Flag | Default | Description |
|---|---|---|
| `--queue` | `default` | Queue name to process |
| `--tries` | `3` | Max attempts before marking a job as failed |
| `--timeout` | `60` | Seconds a job may run before timing out |

---

## Redis requirement

If `QUEUE_CONNECTION=redis` is set in the project's `.env`, servlo verifies that `servlo-redis` is running before starting the worker. If it is not, you will see:

```
queue worker requires Redis (QUEUE_CONNECTION=redis in .env) but servlo-redis is not running
Start it first: servlo services start redis
```

---

## Example

```bash
cd ~/Servlo/my-app
servlo queue:start --queue=emails,default --tries=5 --timeout=120
# Systemd unit: servlo-queue-my-app.service
# Logs: journalctl --user -u servlo-queue-my-app -f
```

---

## Worker state in `.servlo.yaml`

Every start/stop command (`queue:start`, `queue:stop`, `horizon:start`, `schedule:start`, `reverb:start`, `stripe:listen`, `worker start`, etc.) automatically updates the `workers` list in `.servlo.yaml` when the file exists. This means:

- Cloning a project and running `servlo link` or `servlo setup` restores all workers.
- After an uninstall/reinstall cycle, `servlo start` reads `.servlo.yaml` and recreates missing worker units automatically, no need to re-run each start command manually.

The `workers` field is maintained automatically. You do not need to edit it by hand.

---

## Auto-restart on config changes

The servlo watcher daemon monitors `.env`, `composer.json`, `composer.lock`, and `.php-version` for every registered site. When any of those files change it:

- Signals `php artisan queue:restart` inside the PHP-FPM container (debounced to 2 seconds)
- If `.php-version` changed: updates the site registry and regenerates the nginx vhost automatically, no manual reload needed

This ensures queue workers and nginx stay in sync after deploys or PHP version changes without manual intervention.

---

## Failing and restarting workers

`servlo status` includes a Workers section that lists all active, restarting, or failed workers across sites. Paused sites are excluded from this list.

Workers that are crash-looping (repeatedly failing and restarting) are detected automatically. When you unlink a site, servlo stops any crash-looping workers for that site to prevent them from consuming resources after the site is gone.

In the web UI, a failing worker shows a pulsing red toggle and its log tab appears with a **!** indicator so you can inspect the error output immediately.

---

## Web UI control

Queue workers and Horizon are controllable from the **Sites tab** in the web UI:

- For projects **without** Horizon: an amber **Queue** toggle starts or stops the queue worker.
- For projects **with** `laravel/horizon` installed: the Queue toggle is replaced by a **Horizon** toggle (auto-detected from `composer.json`).

When a worker is running, a log tab (**Queue** or **Horizon**) appears in the site detail panel alongside PHP-FPM. The amber dot next to the site in the sidebar indicates a worker is active.
