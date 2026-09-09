# Framework Workers

Every framework can define long-running workers (queue consumers, schedulers, WebSocket servers). This page covers the worker commands, conditional rules, conflicts, proxy wiring, project-specific custom workers, and orphan cleanup.

Each framework can define **workers**: long-running processes managed as systemd user services inside the PHP-FPM container.

| Command | Description |
|---|---|
| `servlo worker start <name>` | Start a named worker for the current project |
| `servlo worker stop <name>` | Stop a named worker |
| `servlo worker list` | List all workers defined for this project's framework |

The shortcut commands `servlo queue:start`, `servlo schedule:start`, `servlo reverb:start`, and `servlo horizon:start` are aliases; they look up the worker from the framework definition and delegate to the generic handler. They work for any framework that defines a worker with that name.

## Worker features

**Conditional workers**: Workers with a `check` rule only appear when the condition passes (e.g. `laravel/horizon` is in `composer.json`):

```yaml
workers:
  horizon:
    command: php artisan horizon
    check:
      composer: laravel/horizon
```

**Conflict resolution**: Workers can declare conflicts. When a conflicting worker starts, the other is stopped automatically and hidden from the UI:

```yaml
workers:
  horizon:
    command: php artisan horizon
    conflicts_with:
      - queue      # stops queue before starting horizon; hides queue toggle in UI
```

**WebSocket/HTTP proxy**: Workers that need an nginx proxy block define a `proxy` config. Servlo auto-assigns a collision-free port and regenerates the nginx vhost:

```yaml
workers:
  reverb:
    command: php artisan reverb:start
    proxy:
      path: /app                    # URL path for the proxy location block
      port_env_key: REVERB_SERVER_PORT  # env key holding the port
      default_port: 8080            # starting port for auto-assignment
```

Port assignment scans all proxy port env keys across all sites to prevent collisions between different workers and frameworks.

The generated nginx location anchors on `path`, so `/app` proxies `/app` and everything under it without also swallowing an unrelated route that merely starts with the same letters (`/appstore`, say). Write `path` as a literal URL path and servlo normalises it, so `/app`, `/app/` and `app` all anchor identically; a `path` of `/` mounts the worker at the site root and proxies everything. Regex-special characters are escaped (a literal `.` in something like `/socket.io`, for instance) before the path reaches nginx's location block, so write it exactly as the URL reads. A `proxy` block with no `path` names nothing to proxy and is ignored.

Anchoring also turns the location into a regex, and nginx runs regex locations in file order ahead of the prefix location it would otherwise have picked, so the proxy takes precedence over servlo's own PHP handling and dotfile deny for anything under `path`. That's the right call for a worker mounted on its own path, but it means a `.php` file or dotfile living under `path` is proxied rather than served by servlo.

**Server health probe**: A worker whose process can outlive its server (a Vite dev server that dies under `npm` while the Node process lingers) declares a `health` block, so servlo probes reachability rather than mere process liveness:

```yaml
workers:
  vite:
    command: npm run dev
    host: true
    health:
      url_file: public/hot   # a file the server writes on boot, holding its URL
```

`url_file` names a file the dev server writes when it binds (Vite's `public/hot` holds a URL like `http://[::1]:5173`). While the process is up, servlo reads that file and makes a short TCP dial to its host and port; if nothing is accepting, the worker reports **unreachable** instead of running and [worker-heal](/usage/worker-heal) restarts it. A worker with no `health` block keeps the process-only liveness check.

A missing `url_file` is never itself a failure, only a signal servlo cannot use: the worker keeps the process-only check. Plenty of healthy setups never write one, from a Vite config with a custom `hotFile` to `vite build --watch`. The failure the probe exists to catch is a *stale* file whose advertised port refuses a connection, which is what a dev server that died behind a live unit leaves behind. A file older than the unit's last activation is a leftover from a previous run and is not dialled.

**Host workers**: Workers that need to run on the host instead of inside the PHP-FPM container set `host: true`. The command runs via fnm at the project's pinned Node.js version. This is used for tools like Vite that need direct filesystem access for HMR:

```yaml
workers:
  vite:
    label: Vite
    command: npm run dev
    restart: on-failure
    host: true
    check:
      file: vite.config.js
```

The `command` is wrapped in `/bin/sh -c` so shell features (`&&`, `|`, env-var expansion, redirects) work as written. A composite command like `npm run build && npm run preview` runs end-to-end without quoting tricks.

Host workers auto-start in three places:

- at daemon boot, so units recover after a host reboot or `servlo stop && servlo start` even when fsnotify hasn't fired.

Host workers run with servlo's bin dir prepended to `PATH`, so subprocesses spawned by `npm run dev` (for example Inertia's wayfinder Vite plugin shelling out to `php artisan`) reach servlo's `php`, `composer` and `laravel` shims and route into the containerised runtime.

**Dev servers on the site's own domain**: A dev server normally advertises its own address, so a Vite app renders asset URLs pointing at `localhost:5173`. That address means nothing to anyone else, so the page arrives unstyled on any host other than the one that started it.

servlo puts a supported dev server behind the site's own domain instead. Everything the tool serves lives under one prefix (`/@servlo-vite/`), which the site's vhost proxies to it, so the assets and the hot-reload websocket both travel on whatever hostname the visitor actually used. Nothing needs rewriting, because the client derives its host, port and protocol from the URL it was loaded from.

This needs no configuration and no framework definition. A host worker qualifies when the project has the tool installed and the worker command starts it directly, following one level of `npm run` indirection. A command that only reaches the tool through a runner such as `concurrently` is left alone, since the flags servlo appends would land on the wrong process.

Nothing in the project is edited. servlo writes a generated config to `node_modules/.servlo/` that imports the project's own config and merges in the base, origin and allowed hosts for `serve` only, then starts the tool against it. That file is rewritten on every start, since a `node_modules` seeded from another checkout would otherwise carry that checkout's domain. A project with no config file for the tool, or one that tracks the generated path in git rather than ignoring it, keeps its dev server exactly as it was.

Framework plugins released before Vite grew `server.origin` ignore it and publish whatever address the server bound to, writing it to the file the app reads to find its dev server. That address is a wildcard nothing can route to, and on a secured site the browser blocks the plain-HTTP request as mixed content and drops the padlock, so the page arrives unstyled. The generated config catches that one value as it is written and stores the site's own URL instead, which is what a current plugin writes there anyway. Upgrading the plugin remains worthwhile, but an old one no longer breaks the page.

The port is pinned, because the vhost proxies to it and the tool would otherwise drift to the next free one whenever several sites run. It is kept clear of other sites and of whatever else the machine is holding, and a pin something has since taken is re-picked rather than left to fail.

The tool reads those addresses once, when it starts, so servlo writes them back and restarts the dev server whenever they move: `servlo secure` and `servlo unsecure`, `servlo domain add` and `servlo domain remove`, and grouping a site under a main. A dev server that is not running is left down, and a change that leaves the addresses exactly as they were restarts nothing.

A site with more than one domain serves its assets from the primary one, since a dev server can advertise only a single origin. The generated config lists every domain, both as a host the server answers for (along with its subdomains, matching the vhost's wildcard) and as an origin allowed to fetch from it, so a page opened on a second domain loads normally instead of having its assets refused.

Some plugin middleware registers itself ahead of the tool's own base handling and only answers unprefixed, which would 404 on URLs it advertised itself. nginx retries any 404 under the prefix once with the prefix removed, so those routes work without anything having to name them.

## Project-specific custom workers

Add workers to `.servlo.yaml` for project-specific needs that don't belong in the framework definition:

```yaml
# .servlo.yaml
framework: symfony
framework_version: "8"
workers:
  - messenger
  - pdf-generator
custom_workers:
  pdf-generator:
    label: PDF Generator
    command: php bin/console app:generate-pdfs --daemon
    restart: always
```

Custom workers with proxy support:

```yaml
custom_workers:
  mercure:
    label: Mercure Hub
    command: php bin/console mercure:run
    restart: always
    proxy:
      path: /.well-known/mercure
      port_env_key: MERCURE_PORT
      default_port: 3000
```

Custom workers are merged with the framework's workers at runtime. They are committed to git so teammates get the same setup.

A custom-container site need not be on a framework at all, and its custom workers are then the only workers it has. Servlo resolves them the same way everywhere: the dashboard lists them, start and stop act on them, pausing the site takes them down with it, and the boot sweep writes their unit files back after a reinstall or a rebuild. Because they come out of a file inside the repository, a `host: true` worker among them asks for consent before it runs on the server, exactly as one on a framework site does.

## Worker logs

```bash
journalctl --user -u servlo-messenger-myapp -f
```

## Managing custom workers

Use `servlo worker add` to add project-specific or global custom workers without manually editing YAML:

```bash
# Add a project-specific worker (saved to .servlo.yaml)
servlo worker add pulse --command "php artisan pulse:work" --label "Pulse" --check-composer laravel/pulse

# Add a worker that conflicts with another (stops it on start, hides it in UI)
servlo worker add custom-queue --command "php artisan queue:work --queue=emails" --conflicts-with queue

# Add a global worker (saved to ~/.config/servlo/frameworks/<name>.yaml)
servlo worker add pulse --command "php artisan pulse:work" --global

# Remove a custom worker (stops it if running)
servlo worker remove pulse
servlo worker remove pulse --global
```

Project workers (`.servlo.yaml`) apply to a single project and are committed to git. Global workers (user overlay) apply to all projects using that framework. Both survive framework store updates.

The resulting `.servlo.yaml` looks like:

```yaml
framework: laravel
custom_workers:
  pulse:
    label: Pulse
    command: php artisan pulse:work
    check:
      composer: laravel/pulse
  custom-queue:
    command: php artisan queue:work --queue=emails
    conflicts_with:
      - queue
```

After adding, start the worker with `servlo worker start pulse`.

When running `servlo init --fresh`, existing custom workers are shown in a multi-select step before the workers step. Deselecting a custom worker removes it from `.servlo.yaml` and excludes it from the workers selection. If the removed worker had `conflicts_with`, those workers become available again.

## Orphaned workers

A worker becomes orphaned when its systemd unit is still running but its definition has been removed from `.servlo.yaml` (e.g. after a `git pull` or manual edit). Orphaned workers are detected and surfaced in several places:

- **`servlo worker list`**: shows orphaned workers with a stop hint
- **`servlo worker stop <name>`**: can stop orphaned workers even without a definition
- **`servlo setup`**: offers orphaned workers as pre-selected stop steps before framework worker starts
- **UI**: the stop button works for orphaned workers directly

## Web UI (worker toggles)

Framework workers appear as toggles in the Sites panel. Workers with a `check` rule only appear when the condition passes. Workers with `conflicts_with` suppress each other (e.g. when Horizon is available, the queue toggle is hidden).

Custom framework workers from `.servlo.yaml` also appear as toggles alongside the framework's standard workers.

---

See also: [Frameworks](frameworks.md) for the framework store and Laravel definition; [Framework definitions](framework-definitions.md) for the YAML schema.
