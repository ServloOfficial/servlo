# Configuration

## Global config: `~/.config/servlo/config.yaml`

Created automatically on first run with sensible defaults:

```yaml
php:
  default_version: "8.5"
node:
  default_version: "22"
  managed: true           # optional. Whether servlo manages Node (install/use/default
                          # via the active manager). With fnm that includes PATH
                          # shims; with nvm the user's shell keeps owning
                          # node/npm/npx. Written by the install prompt and by
                          # servlo node:manage / node:unmanage; honoured on servlo
                          # update so an opt-out is not undone. Omitted on
                          # configs predating it, which fall back to whether a
                          # node shim is on disk.
  manager: fnm            # optional. Which version manager servlo drives: "fnm"
                          # (the bundled default) or "nvm" (a user-installed nvm,
                          # picked automatically when you decline managed Node and
                          # nvm is present). Switchable from the dashboard's Node
                          # page; switching to fnm downloads it on demand if an
                          # nvm-only install skipped it. Empty means fnm.
  nvm_dir: ~/.nvm         # optional. Where nvm lives when manager is nvm.
                          # Written at install/switch so servlo-ui and the watcher
                          # (which never load your shell rc) find a custom
                          # $NVM_DIR. Empty falls back to $NVM_DIR or ~/.nvm.
shims:
  path_disabled: false    # optional. Set true (or run servlo path:disable) to keep
                          # servlo's shims dir (~/.local/share/servlo/bin with php,
                          # composer, node…) off your shell PATH, for typing
                          # `servlo php` explicitly instead. Honoured by install
                          # and update, so the rc entry is not re-added. servlo's
                          # own commands and workers are unaffected.
nginx:
  http_port: 80
  https_port: 443
  request_timeout: 60   # optional, default 60. Seconds nginx waits on a slow
                        # request before returning 504. Maps to
                        # fastcgi_read_timeout/fastcgi_send_timeout for PHP-FPM
                        # sites and proxy_read_timeout/proxy_send_timeout for
                        # proxy and custom-container sites. A project's
                        # .servlo.yaml request_timeout overrides it per site.
dns:
  # Legacy. Servlo removed its DNS stack in S2.1 and reads none of these; they
  # are listed only so an older config file is recognisable. Delete the block.
  enabled: true
  tld: "test"
host_proxy:
  disabled: false         # set true to refuse setting up or starting any
                          # host-proxy dev server (servlo never supervises a
                          # process on the host). Default false.
  skip_confirmation: false # set true to link a host-proxy project without the
                          # "start this command on your host?" confirmation.
                          # Default false so a command from a cloned repo is
                          # never run unconfirmed. See usage/host-proxy.md.
auto_cleanup: true      # when true (default), servlo reclaims its own orphaned
                        # podman images on its own: the servlo-watcher runs a safe
                        # daily sweep, and a PHP rebuild or a service update/remove
                        # reclaims the image it just superseded. Only ever removes
                        # servlo's own images (old PHP build and base images,
                        # superseded service versions), never data volumes or
                        # images in use. Toggle with `servlo cleanup auto on/off`
                        # (or set this key); `servlo cleanup` still works on demand
                        # when off. See reference/commands.md.
parked_directories:
  - ~/sites
services:
  mysql:       { enabled: true,  image: "docker.io/library/mysql:8.4",             port: 3306 }
  redis:       { enabled: true,  image: "docker.io/library/redis:7-alpine",        port: 6379 }
  postgres:    { enabled: false, image: "docker.io/postgis/postgis:16-3.5-alpine", port: 5432 }
  meilisearch: { enabled: false, image: "docker.io/getmeili/meilisearch:v1.7",     port: 7700 }
  rustfs:      { enabled: false, image: "docker.io/rustfs/rustfs:latest",          port: 9000 }
php:
  extensions: [mongodb] # custom PHP extensions (`servlo php:ext`). One declared set,
                        # applied to every PHP image servlo builds: extensions belong
                        # to you, not to a version, so a site that changes version
                        # keeps them.
  packages: [chromium]  # extra Alpine packages (`servlo php:pkg`), same model.
  ext_apk_deps:         # extra Alpine packages required at build time by
                        # `servlo php:ext add <ext> --apk-deps <pkgs>` invocations.
                        # Keyed by extension name (build deps don't vary by PHP
                        # version), value is a list of apk package names, e.g.
                        # `gd: [libwebp-dev, libpng-dev]`. The PHP-FPM
                        # Containerfile reads this block on rebuild so the extra
                        # build deps reattach to the layer automatically.
  realised:             # what each version's image actually loaded, verified
                        # after its build. Managed by servlo, not hand-edited.
    "7.4":              # the declared set can't always be honoured (mongodb needs
      packages: []      # 8.1+; the 7.4/8.0 images are Alpine 3.16), so servlo records
      extensions: []    # the truth per version and never advertises what an image
                        # does not have.
```

---

## Per-project config: `.servlo.yaml`

A portable, self-contained description of a project's local environment. Created by `servlo init` or written manually, committed to the repository, and applied automatically by `servlo link` and `servlo init`.

### Fields

| Field | Description |
|---|---|
| `php_version` | PHP version for this project (highest priority, overrides `.php-version` and `composer.json`) |
| `node_version` | Node version (highest priority, overrides `.nvmrc`, `.node-version`, and `package.json`); writes `.node-version` on apply if the file does not already exist |
| `framework` | Framework name (overrides auto-detection) |
| `framework_def` | Full framework definition, embedded automatically for custom (non-Laravel) frameworks so the project is portable across machines |
| `public_dir` | Override for the framework's default document-root subdirectory, e.g. `public_html` for a Laravel skeleton that doesn't use the conventional `public/` folder. Empty means use the framework default |
| `request_timeout` | nginx request timeout in seconds for this site. Maps to `fastcgi_read_timeout`/`fastcgi_send_timeout` for PHP-FPM sites and `proxy_read_timeout`/`proxy_send_timeout` for proxy and custom-container sites. Overrides the global `nginx.request_timeout`. Omit (or `0`) to inherit the global default of 60s. Raise it for apps with deliberately long-running requests |
| `secured` | When `true`, HTTPS is enabled on apply |
| `domains` | The site's fully qualified hostnames (e.g. `[example.com, api.example.com]`). Written whole: servlo appends nothing. The first entry is the primary; additional entries become aliases. Conflict-filtered domains stay in this list on disk but are not registered. A hostname may not contain whitespace, a slash, or nginx punctuation (`{`, `}`, `;`, `#`), since it is written into the generated vhost's `server_name` |
| `app_url` | Override for `APP_URL` (or the framework's URL key) written to `.env`. Highest priority, it beats the per-machine `sites.yaml` override and the default `<scheme>://<primary-domain>` generator. Use for custom path prefixes, ports, or unrelated hostnames you want shared across machines |
| `env_overrides` | Map of env var names to templated or static values applied to `.env` on `servlo setup`. Values may use <code v-pre>{{domain}}</code>, <code v-pre>{{scheme}}</code>, and <code v-pre>{{site}}</code> placeholders, or be plain strings. When `APP_URL` is in `env_overrides` it takes precedence over the default rewrite; declared keys override defaults, undeclared defaults still apply. See Env overrides |
| `services` | Services to start on apply. Accepts built-in names, preset references, and the names of services already installed on this machine. A full inline definition is read but never run, see Inline service definitions are not run |
| `workers` | Active worker names for the site (e.g. `queue`, `horizon`, `schedule`, `reverb`, `stripe`). Automatically kept in sync by start/stop commands. Used by `servlo start` to restore workers after reinstall |
| `container` | Custom container config for non-PHP sites. When present, servlo builds a dedicated container from the project's Containerfile and nginx reverse-proxies to it. See below and Custom Containers |
| `custom_workers` | Custom worker definitions (name to config map). Works for both PHP and custom container sites. See below |
| `db` | Database targeting for non-PHP projects: `service` (e.g. `mysql`, `postgres`) and `database` name |
| `stripe` | Optional Stripe webhook listener config: `path` (forward route, defaults to `/stripe/webhook`) and `secret_env_key` (which `.env` key holds the secret, defaults to auto-detection). See Stripe |

### Basic example

```yaml
php_version: "8.5"
node_version: "22"
framework: laravel
secured: true
services:
  - mysql
  - redis
```

### Custom public folder

When the project ships with a non-standard document root (e.g. a Laravel skeleton that uses `public_html/` instead of `public/`), set `public_dir`:

```yaml
framework: laravel
public_dir: public_html
```

On `servlo link` this value becomes the site's document root in the generated nginx vhost; it takes precedence over the framework's default. No need to define a full `framework_def` just to change the doc root.

### Custom container example

For non-PHP sites (Node.js, Python, Go, etc.), define a `container` section instead of `php_version` and `framework`:

```yaml
domains:
  - nestapp
container:
  port: 3000
  containerfile: Containerfile.servlo
services:
  - mysql
  - redis
custom_workers:
  dev-server:
    label: Dev Server
    command: npm run start:dev
    restart: always
```

Every field of a worker becomes a line of the systemd unit servlo generates, so none of them may contain a newline or a NUL. A worker whose `label`, `command`, `restart` or `schedule` carries one is refused with the offending field named, rather than written out as a unit. `.servlo.yaml` is committed and travels with a checkout, so a cloned project cannot use a worker definition to add directives to a unit on your machine.

When `container` is present, `php_version`, `framework`, and `node_version` are ignored.

#### `container` fields

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `port` | yes | | Port the app listens on inside the container |
| `containerfile` | no | `Containerfile.servlo` | Path to the Containerfile (relative to project root) |
| `build_context` | no | `.` | Build context directory (relative to project root) |

See Custom Containers for the full guide.

### Custom workers

Custom workers can be defined for any site type (PHP or custom container). Each entry in `custom_workers` maps a name to a worker config:

```yaml
custom_workers:
  queue:
    label: Queue Worker
    command: node dist/queue.js
    restart: always
  cron:
    label: Cron Job
    command: node dist/cron.js
    restart: on-failure
    schedule: minutely
```

#### Worker config fields

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `label` | no | worker name | Display name in the dashboard |
| `command` | yes | | Shell command to run inside the container |
| `restart` | no | `always` | `always` or `on-failure` |
| `schedule` | no | | systemd OnCalendar expression for timer-based workers (e.g. `minutely`, `*-*-* *:00:00`) |
| `conflicts_with` | no | | List of worker names to stop before starting this one |
| `host` | no | `false` | Run on the host via fnm instead of inside the PHP-FPM container. Used for Node.js tools (Vite, Tailwind watcher, Encore) that need direct filesystem access for HMR |
| `replaces_build` | no | `false` | While running, the worker provides the asset manifest so the static `npm run build` step is unnecessary. `servlo setup` skips its build prompt when an opted-in `replaces_build` worker is present |

Worker definitions stay in `custom_workers` permanently. The `workers` field (a separate list of names) tracks which are currently active and is synced automatically by start/stop commands.

Framework yamls (under `servlo-frameworks/frameworks/<framework>/<version>.yaml`) declare workers under a sibling `workers:` block with the same shape, so `host` and `replaces_build` apply there too. The shipped Laravel 11 / 12 / 13 yamls use this for `vite` (`host: true`, `replaces_build: true`), and any custom framework can do the same to teach servlo about its dev server.

### Inline service definitions are not run

A `.servlo.yaml` may carry a full service definition inline, beside the named
and preset forms:

```yaml
services:
  - redis
  - mongodb:
      image: docker.io/library/mongo:7
      ports:
        - 27017:27017
```

Servlo reads that entry and refuses it. The image and the command come from the
repository, which the operator may have cloned from anywhere, and a server does
not put a container on itself on that say-so. Only a reviewed store preset may
run a container.

The site links either way. The inline entry is dropped, the rest of the config
applies, and `servlo link` prints which service it dropped:

```
⚠ service mongodb: this project defines it inline, which servlo does not run.
  Install it with 'servlo service preset mongodb' if a preset exists, or
  'servlo service add' to define it yourself.
```

`servlo check` reports the same entry as an error, so it surfaces before a link
rather than only during one.

To actually run the service, install it on the server yourself: `servlo service
preset <name>` for a store preset, or `servlo service add` for a definition you
write. Then reference it in `.servlo.yaml` by name. The block in the project
file is left exactly as it was found, so nothing is rewritten under the author.

### Custom frameworks

When `servlo init` runs in a project that uses a custom framework (one added with `servlo framework add`), the full framework definition is embedded under `framework_def`. On a fresh machine the definition is restored automatically before linking, no manual `servlo framework add` step needed.

```yaml
framework: wordpress
framework_def:
  label: WordPress
  public_dir: .
  detect:
    - file: wp-config.php
  env:
    file: .env
  ...
```

If a framework with that name already exists locally and differs from the embedded definition, a diff is shown before applying.

### Applying `.servlo.yaml`

The config is applied whenever `servlo link` or `servlo init` runs in the project root:

- **`servlo link`**: framework definition restored, `.node-version` written, PHP version applied, HTTPS toggled, presets installed and services started.
- **`servlo init`**: installs PHP FPM if needed, then runs `servlo link` (which applies everything above). Re-runs the wizard if `--fresh` is passed.

Commit `.servlo.yaml` to the repository. On a fresh machine, `servlo link` is sufficient to reproduce the full local environment. Servlo writes the file through a temp file and a rename so a crash or two concurrent writers can never leave it half-written, and it normalises the output (two-space indentation, `services` and `workers` sorted), so a worker starting or stopping produces a minimal, stable git diff rather than a reshuffled block.

The Servlo watcher also monitors `.servlo.yaml` for changes. When you switch branches with a different config the PHP and Node versions are re-detected and applied automatically, no manual `servlo link` or `servlo init` needed. See [Automatic version switching](./features/project-setup.md#automatic-version-switching) for details.

`servlo isolate` and the UI PHP version selector both keep `php_version` in sync when this file exists.

`servlo secure`, `servlo unsecure` and the UI HTTPS toggle keep `secured` in sync when this file exists.
