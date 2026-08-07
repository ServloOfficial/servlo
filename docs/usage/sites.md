# Site Management

## Commands

| Command | Description |
|---|---|
| `servlo init` | Interactive wizard: choose PHP version, HTTPS, and services, then save `.servlo.yaml` and apply |
| `servlo init --fresh` | Re-run the wizard with existing `.servlo.yaml` values as defaults |
| `servlo park [dir]` | Register every PHP project inside `dir` as a site, and keep doing so as new ones appear (defaults to cwd) |
| `servlo unpark [dir]` | Remove a parked directory and unlink all its sites |
| `servlo link [domain]` | Register the current directory as a site (domain name without TLD, defaults to directory name). On a fresh project in an interactive terminal it runs the `servlo init` wizard first |
| `servlo unlink` | Unlink the current directory site (removes all domains) |
| `servlo domain add <name>` | Add an additional domain to the current site |
| `servlo domain remove <name>` | Remove a domain from the current site |
| `servlo domain list` | List all domains for the current site |
| `servlo sites` | Table view of all registered sites |
| `servlo open [name]` | Open the site in the default browser |
| `servlo secure [name]` | Issue a TLS certificate and enable HTTPS, updates `APP_URL` in `.env` |
| `servlo unsecure [name]` | Remove TLS and switch back to HTTP, updates `APP_URL` in `.env` |
| `servlo pause [name]` | Pause a site: stop its workers and replace the vhost with a landing page |
| `servlo unpause [name]` | Resume a paused site: restore its vhost and restart previously running workers |
| `servlo env` | Configure `.env` for the current project with servlo service connection settings |
| `servlo workspace add <name>` | Create an empty workspace |
| `servlo workspace rename <old> <new>` | Rename a workspace, keeping its sites |
| `servlo workspace rm <name>` | Delete a workspace; its sites become ungrouped |
| `servlo workspace assign <site> <workspace\|none>` | Move a site into a workspace, or out of one with `none` |
| `servlo workspace move <name> <position>` | Reposition a workspace in the display order (`0` is first) |
| `servlo workspace list` | List the workspaces and their sites |

---

## Project initialisation

`servlo init` runs an interactive wizard, writes the answers to `.servlo.yaml` in the project root, and then applies the configuration: linking the site, enabling HTTPS if requested, picking a database, and starting any required services.

`servlo link` and `servlo init` overlap on purpose. When you run `servlo link` on a project that has no `.servlo.yaml` yet and you're in an interactive terminal, link routes straight into the init wizard, so you don't have to know to reach for `init` first. If the project already has a `.servlo.yaml`, link just applies it. In a non-interactive shell (a script, CI, or any piped invocation) link does a fast auto-detected registration with no wizard, so automation never blocks on a prompt. Passing an explicit domain (`servlo link myapp`) also skips the wizard and links directly.

Every way of linking a project resolves the same plan: the CLI, the dashboard's **+** button, `servlo park`, and the parked-directory watcher. They differ only in what they are allowed to do, and the difference is deliberate. A link you type can prompt, write `.php-version` and `.node-version`, install services, issue a certificate, and supervise a dev command the project declares. An unattended link (park and the watcher) reads the same committed configuration but never prompts, never writes into the project, and never runs anything the repository authored.

```bash
cd ~/Projects/my-app
servlo init
```

```
? PHP version: 8.5
? Node version (leave blank to skip):
? Enable HTTPS? No
? Database:
  > SQLite (no service)
    MySQL (servlo-mysql)
    PostgreSQL (servlo-postgres)
? Services:
  ◉ redis
  ◯ meilisearch
  ◯ rustfs
  ◯ mailpit
Saved .servlo.yaml
Linked: my-app -> my-app.test (PHP 8.5, Node 22, Framework: laravel)
```

Wizard defaults are populated intelligently on first run:

- **PHP version**: from the site registry if already linked, otherwise from `.php-version`, `composer.json`, or the global default
- **Enable HTTPS**: pre-checked if the site is already secured
- **Database**: pre-selected from any database already in `.servlo.yaml`, otherwise from `DB_CONNECTION` in `.env` (or `.env.example` for a fresh clone), falling back to SQLite (Laravel's default for new projects)
- **Services**: pre-checked based on what's detected in the project's `.env` file (only non-database services here, since the database is its own step)

The Database step is a single choice rather than a multi-select, so picking MySQL automatically deselects SQLite and vice-versa. After the wizard completes, `servlo env` runs automatically to write your choices to `.env`:

- **MySQL / PostgreSQL**: `DB_CONNECTION` and the related `DB_HOST` / `DB_PORT` / `DB_DATABASE` / `DB_USERNAME` / `DB_PASSWORD` keys are rewritten to point at `servlo-mysql` / `servlo-postgres`, the service is started if it isn't already, and the project database (plus a `_testing` variant) is created.
- **SQLite**: `DB_CONNECTION=sqlite` and `DB_DATABASE=database/database.sqlite` are written to `.env`, and the `database/database.sqlite` file is created if it doesn't exist. No service is started.

The choice is authoritative: if `.env` already had `DB_CONNECTION=mysql` from a previous setup and you switch to SQLite (or vice versa) in the wizard, servlo skips the auto-detection of the old database and applies your new pick instead.

The same prompt also appears when you run `servlo env` directly on a project whose `.env` says SQLite and whose `.servlo.yaml` doesn't yet have a database picked, for example, after cloning a project that wasn't created with `servlo init`. The prompt is skipped automatically when stdin isn't a TTY (e.g. `servlo setup --all` in CI), and for frameworks with explicit env service rules (`fw.env.services` in the YAML, like Symfony, WordPress, etc.) since those don't use Laravel's `DB_CONNECTION` convention.

Persistence is one-way: servlo reads the source of truth from `.servlo.yaml` and writes only to `.env`. `.env.example` is never modified; it's only used as a template when `.env` doesn't exist yet.

The resulting `.servlo.yaml` is intended to be committed to the repository. On a new machine or after a reinstall, running `servlo init` again reads the saved file and restores the full configuration without any prompts.

```bash
# On a fresh machine, no wizard, config applied directly
git clone ...
cd my-app
servlo init
```

Use `--fresh` to re-run the wizard while keeping existing values as defaults:

```bash
servlo init --fresh
```

---

## Parking a directory of projects

`servlo park ~/Code` registers every PHP project directly inside a directory, and
records the directory so the watcher keeps up with it: a project you clone into
it later is registered on its own, and one you delete is unlinked.

```bash
servlo park ~/Code
```

```
 parking /home/me/Code
 → linking 128 projects… ████████████░░░░░░░░ 74/128 · shop ⠙
 ✓ linking 128 projects 126 linked, 2 skipped
 → publishing… ✓ 126 site(s) serving
```

Each project writes only its own vhost and PHP unit; the reloads that publish
them run once for the whole batch. That matters at scale, because those steps
rewrite every quadlet and every container hosts entry, so doing them per project
made a large directory take minutes rather than seconds.

A parked link reads the project's committed `.servlo.yaml`, its domains, public
directory and PHP version, but it runs unattended, so it stops short of
anything that needs a decision or runs code the repository wrote. It never
prompts, never writes `.php-version` or `.node-version` into your project, never
installs services, and never issues a certificate.

Some projects are reported as skipped rather than registered:

- A directory that is not a PHP project at all.
- A project that declares its own runtime, a custom container, a host-proxy dev
  server, FrankenPHP, or a custom FPM image. Each needs an image built or a
  command run, which an unattended sweep should not do on its own. Run `servlo
  link` in the project to set it up, after which the watcher leaves it alone.

Use `servlo unpark <dir>` to stop watching a directory and unlink its sites.

---

## Non-PHP / custom container sites

For Node.js, Python, Go, or any other non-PHP runtime, servlo builds a dedicated container image per project and has nginx reverse-proxy to it. The workflow differs from PHP sites:

1. Create a `Containerfile.servlo` in the project root that defines the runtime and start command.
2. Run `servlo init`; it detects the non-PHP project (no `composer.json`) and switches to custom container mode, asking for the port, HTTPS, and services. It writes `.servlo.yaml` for you. Alternatively write `.servlo.yaml` manually with a `container: {port: N}` section.
3. Run `servlo link`; it builds the image, starts the container as `servlo-custom-<sitename>`, and generates the nginx vhost.

> **Important:** calling `servlo link` without the container config registers the project as a PHP-FPM site (wrong). If that happened, run `servlo unlink` first, set up the files, then `servlo link` again.

See Custom Containers for the full configuration reference.

### Static sites

A project that is just a `public_dir` of HTML/CSS/JS with no `composer.json` and no `.php` files is served directly by nginx as a static site. servlo recognises these as non-PHP, so the site detail panel hides every PHP-only surface: the PHP version dropdown, the Dumps tab, and the PHP-FPM logs tab. A site counts as PHP only when it has a `composer.json` or a top-level `.php` file, or runs under a custom container or FrankenPHP.

---

## Projects outside the home directory

By default, the PHP-FPM and nginx containers only have access to files under `$HOME`. If your project lives elsewhere (e.g. `/var/www`, `/opt/projects`, `/var/local`), servlo automatically detects this and adds the required volume mount to both containers.

This happens transparently when you:

- **`servlo link`** or **`servlo park`** a directory outside `$HOME`
- Run **`servlo php`**, **`composer`**, **`laravel new`**, or any exec command from an outside path

The containers are restarted once to pick up the new mount. Subsequent commands from the same path run without delay. When you unlink or unpark, stale mounts are cleaned up automatically.

---

## Domain naming

Directories with real TLDs are automatically normalised: dots are replaced with dashes and the TLD is stripped before appending `.test`.

For example: `admin.example.com` becomes `admin-example.test`

---

## Multiple domains

A site can respond to multiple domains. The argument to `servlo link` is the domain name without the `.test` TLD; it is appended automatically from the global config.

```bash
servlo link myapp                # links as myapp.example.com
```

After linking, you can add more domains:

```bash
servlo domain add api.example.com
servlo domain add admin.example.com
servlo domain list
#   myapp.example.com (primary)
#   api.example.com
#   admin.example.com
servlo domain remove api.example.com
```

Every domain is a fully qualified name. Servlo appends nothing, so a bare label like `api` is refused rather than completed: the only name that reaches this server is the one DNS points here, and inventing a suffix would produce a site nobody can visit.

Domains are stored whole in `.servlo.yaml`:

```yaml
domains:
  - myapp.example.com
  - admin.example.com
```

You can also manage domains from the web UI: click the pencil icon next to the domain in the site header to open the domain management modal. Changing the primary domain there also rewrites `APP_URL` in the project's `.env` to match the new primary, unless you have pinned a custom `app_url` (see [Custom `APP_URL`](#custom-app-url) below).

When a site is secured with HTTPS, the certificate is automatically reissued to cover all domains.

Subdomains (e.g. `anything.myapp.example.com`) are automatically routed to the same site.

To route a subdomain to a **different** site instead (for example a separate admin app at `admin.myapp.example.com`), group the two sites rather than adding an alias. See [Site Groups](site-groups.md).

---

## Domain conflicts

A domain may only be claimed by one site at a time. When `servlo link`, the watcher's auto-registration, or a `.servlo.yaml`-driven re-link tries to register a domain that another site already owns, the conflicting domain is **filtered out** (not the whole site) and a warning is printed:

```
$ servlo link
  [WARN] domain "shared.test" already used by site "owner-app", skipped
Linked: clone-app -> clone-app.test (PHP 8.5, Node 22, Framework: laravel)
```

The site still gets registered with whatever domains survived the filter. If every requested domain is conflicted, servlo falls back to a freshly generated `<dirname>.<tld>` (with a numeric suffix to avoid name collisions).

`.servlo.yaml` is **never modified** when this happens; the original `domains:` list stays on disk so the conflict is visible to the UI and the entry self-heals on the next link if you remove the owning site. The web UI surfaces filtered domains in two places:

- The site detail header's domain pill shows an amber ⚠️ when one or more declared domains are filtered (`+N more` count includes them). Hovering reveals each conflicted entry with the owning site name.
- The Manage Domains modal lists conflicted entries at the top with a warning icon, the domain struck-through, a `used by <site>` pill, and a small trash button. Clicking the trash removes the entry from `.servlo.yaml` only; the registry, vhost, and certs are untouched.

The conflict check is **strict**: a domain is reserved regardless of TLS scheme. Two sites cannot share the same domain even if one runs HTTPS and the other HTTP; DNS and browser caches don't reliably disambiguate by scheme, and the resulting setup is fragile.

---

## Custom `APP_URL`

By default `servlo env` writes `APP_URL=<scheme>://<primary-domain>` to the project's `.env` on every run. If you need to override that (for example to add a path prefix, point at a staging hostname, or pin a specific protocol), set `app_url` in `.servlo.yaml` (committed, shared across machines) or in the per-machine site entry in `~/.local/share/servlo/sites.yaml`. The precedence chain is:

1. `.servlo.yaml` `app_url`: committed to the repo, takes effect on every machine.
2. `sites.yaml` `app_url`: per-machine override, useful when only one developer needs a different URL.
3. The default generator (`<scheme>://<primary-domain>`): used when neither override is set.

```yaml
# .servlo.yaml
domains:
  - myapp
app_url: http://myapp.example.com/api
```

`servlo env` reads the chain on every invocation, so editing the file and re-running `servlo setup` (or `servlo env` directly) is enough to apply the change. If the `.servlo.yaml` `app_url` happens to point at a domain that got filtered by the conflict check, servlo silently falls through to the next precedence level so you don't end up writing a `DB_HOST` of `servlo-mysql` next to an `APP_URL` that points at someone else's site.

---

## Workers

The `servlo init` wizard includes a workers step that lets you select which workers to auto-start when linking. Available workers depend on the framework and what's installed:

- **queue**: shown when the framework defines a queue worker (replaced by horizon when `laravel/horizon` is installed)
- **horizon**: shown only when `laravel/horizon` is in `composer.json`
- **schedule**: the task scheduler
- **reverb**: shown only when `laravel/reverb` is installed or `BROADCAST_CONNECTION=reverb` is in `.env`
- **custom workers**: any additional workers defined in the framework definition

Selected workers are saved to `.servlo.yaml`:

```yaml
workers:
  - horizon
  - schedule
```

When `servlo link` runs and workers are configured but not yet running, it prompts to run `servlo setup` so you can install dependencies, run migrations, and start workers in the right order. If workers are already running (re-link), they are left as-is.

`servlo setup` pre-selects worker steps based on the `.servlo.yaml` workers list. Workers not in the list still appear in the step selector but are unchecked.

Toggling workers from the CLI (`servlo queue:start`, `servlo schedule:stop`, etc.) or the web UI syncs the running state back to `.servlo.yaml` when the file exists.

`servlo check` validates that listed workers are valid for the detected framework.

`servlo status` includes a Workers section showing all active, restarting, or failed workers across sites. In the web UI, failing workers show a pulsing red toggle and their log tab appears with a "!" indicator.

---

## Request timing

The Overview of a PHP site carries a **Request timing** section that reads the always-on nginx access feed to show how the site is responding as you work, no debug bridge needed. A range picker (15 minutes up to 7 days) drives the whole view: headline figures for the typical and p95 response times, the request count and error rate, a response-time distribution, a throughput chart, the slowest routes, and a table of every route with its p50 and p95. A **Recent requests** tab lists the latest calls with their time, method, path, status, and duration.

Routes are grouped after collapsing id-like path segments, so `/users/123` and `/users/456` aggregate as one `GET /users/:id` entry, and query strings are dropped before anything is recorded. Requests nginx serves without the app are left out: static assets by file extension, anything with a zero request time (a static file nginx answers directly, like `manifest.json` or `robots.txt`), and upgraded connections such as WebSockets, so a page's dozens of asset requests don't drown out its app routes. Anything a [dev server](framework-workers.md) answered is left out on the same grounds: it serves under its own prefix on the site's domain, so without this its modules arrive looking like routes the app served, and the ones it rebuilds on every save would read as the busiest routes on the site. An upgrade is logged once, when the socket closes, carrying the whole lifetime of the connection as its request time, so a long-lived Reverb or Vite HMR socket would otherwise read as one route taking thousands of seconds. Requests are written to a small SQLite store in the data directory, so the history survives a restart and any range up to the seven-day retention window can be read back; the watcher also keeps an in-memory window, which is what the doctor and slow-route notifications read.

The same flagged routes also surface as a `Response Time` warning in the site doctor (`servlo site:doctor` and the dashboard doctor card), so the nudge reaches you even when you're on another tab. The doctor reads the watcher's snapshot rather than re-measuring, so it stays quiet on a healthy or idle site. If you've enabled notifications, a route crossing the threshold also fires a `slow_route` push. It's edge-triggered: one push when the route goes slow, then it rearms once the route drops back within the typical band, so you're told again if it regresses later (see Notifications).

This is a local, single-developer signal meant to catch a route that is dragging, not a production analytics system. Each flagged route is listed with its own timings so you can see which one is dragging. Profiling is global and stays off until you ask for it, so the button turns it on for every request until you turn it back off. A non-navigable route (a POST, say) can't be opened for you, so there the button just arms profiling and opens the Profiler for you to reproduce it (see Profiler).

---

## Name collision handling

When a directory is parked or linked and another site is already registered with the same name:

- **Same path**: treated as a re-link of the same site. The existing registration is updated and the TLS state is preserved.
- **Different path**: the new site is registered with a numeric suffix (`myapp-2`, `myapp-3`, etc.) so both sites can coexist.

Paths are compared after resolving symlinks, and the resolved path is what gets stored. On atomic images (Fedora Silverblue, Bazzite, and other ostree systems) `/home` is a symlink to `/var/home`, so linking a project through either spelling maps to the one site instead of registering it twice.

---

## Adding a site from the panel

The **+** button next to the Sites list header, and the **Add site** call to action on the dashboard, both open the Add Site modal.

Two fields matter. **Domain** is the fully qualified name the site is served on, typed in full: servlo has no TLD of its own to complete a bare name with, so `myapp` is refused rather than turned into something that resolves nowhere. **Directory** is where the project lives, either typed or picked with **Browse**. If the directory does not exist yet, servlo creates it, provided its parent does; a whole missing tree is refused, because that turns a typo into a directory nobody will look for again.

As soon as a directory is named, servlo reads it and says what it found: the framework it detected, or that it detected none and will guess the document root. **PHP version** and **Document root** are prefilled from that and can be overridden, because the operator looking at the detected values is the one best placed to disagree with them.

An override servlo cannot use is refused with the reason rather than stored. The PHP version has to be one servlo can serve, since it names the FPM container the vhost points at, and the document root has to be a directory inside the site, since a vhost cannot serve a root outside the project it belongs to.

Submitting registers the site, generates its vhost and provisions its runtime. Nothing is created before every refusal has had its chance, so a rejected domain leaves no directory behind on the retry.

The new site is not secured. Point the domain's DNS at this server, and the site's **Get SSL** button issues a certificate once its live DNS check sees every domain and alias resolving here.

A site added on an empty directory is registered and served, and serves nothing until a project is put in it. The modal says so rather than closing onto a blank page.

### Cloning a repository

The modal's second source clones instead of pointing at what is already there, and it is three steps rather than one because the middle one is the point.

Type the domain, then press **Show deploy key**. Servlo generates an ed25519 key for that site, keeps the private half 0600 in its own data directory, and shows you the public half to paste into the repository's **Deploy keys** on GitHub, GitLab or Bitbucket. Read access is enough. Then press **Test connection**.

Without that middle step a clone of a private repository fails with `Permission denied (publickey)` and nothing on screen explains that a key needed pasting anywhere. With it, every failure says what to do:

| What you see | What it means |
|---|---|
| the host refused the key | the key is not on the repository yet, or it is on a different one |
| authentication worked but the repository was not found | the key is on another repository, or the URL names one that is not there |
| the host name did not resolve | a typo in the URL, or this server has no working DNS |
| this server could not reach the host on port 22 | an outbound firewall between the droplet and the forge |

Anything servlo has not seen before carries ssh's own words rather than a sentence servlo made up about a failure it does not understand.

The key is per site, not per server. A compromised site hands over one repository rather than everything the account can read, and revoking a site's access is deleting one key rather than working out what else would break. Each site's key lives in its own directory under `~/.local/share/servlo/deploy-keys/`, private half 0600. Pressing **Show deploy key** twice returns the same key, so the one you already pasted stays the right one.

Paste whichever URL the repository's clone menu offered: the SSH form, the HTTPS form, or `ssh://`. All three name the same repository and servlo converts to the SSH form, since that is the one a deploy key can authenticate. A URL carrying a username or token is refused; that token would end up in the site config and the audit log, and the deploy key is what replaces it.

The clone goes into an empty directory. Pointing it at a directory that already holds a project is refused with that reason rather than attempted, and a clone that fails takes back the directory servlo made for it, so the retry is not blocked by a stub of its own making.

### Uploading a ZIP

The third source takes a `.zip` of the project. Pick the file, name the domain and the directory, and submit; servlo unpacks it, detects the framework and document root from what landed, and registers the site.

If everything in the archive sits inside one folder, which is what every forge's "Download ZIP" produces, that folder is unwrapped so the project sits at the site root rather than one level below it. An archive with several things at its top level is left as it is, since moving either would be inventing a structure the archive did not have.

Unpacking is where an archive gets to decide what servlo writes, so it is bounded and refuses more than it accepts:

- An entry whose name climbs out of the site directory is refused. So is an absolute path, and a name carrying a backslash, which is a path separator on the machine that wrote the archive and an ordinary character to a check that only looks for `/`.
- Symlinks are refused outright. A link pointing at `/etc/passwd` turns a file the site serves into a file the machine owns, and no site needs one badly enough to be worth checking its target.
- The archive is bounded at 60,000 entries and a gigabyte expanded, checked from the declared sizes before a byte is written, so an oversized archive costs no disk at all.
- Modes from the archive are not honoured. Everything lands 0644, directories 0755. Every site here runs as the same user, and an execute bit in a zip is a decision somebody else made about a file on this machine. PHP is read by the FPM pool, not executed by the kernel, so nothing in a site needs one.

Extraction happens in a scratch directory beside the target and is moved into place only once the whole archive has been read. A refused upload leaves the site directory as empty as it found it, and takes back the directory servlo created for it, so nothing has to be cleared up before trying again.



Adding a site from the panel does not run anything the repository authored. A project declaring a host-proxy dev command has that command registered but not started: a click is consent to serve a project, not to execute code it chose, and the browser has no way to ask about that properly. Run `servlo link` from a shell in the project when you want that.

---

## Unlinked domains

When you visit a `.test` domain that isn't linked to any site over **HTTP**, servlo shows a branded "Site Not Found" page with a link to the dashboard and a retry button. This replaces the browser's generic connection error.

For **HTTPS** the catch-all uses `ssl_reject_handshake on;`, so the browser sees a clean `ERR_SSL_UNRECOGNIZED_NAME_ALERT` connection error rather than a landing page. This is unavoidable: servlo cannot pre-issue a certificate covering arbitrary `*.test` hostnames because browsers (Chrome especially) reject TLD-level wildcard certificates with `ERR_CERT_COMMON_NAME_INVALID`. If you're hitting this on a domain you used to have linked, the fix is browser-side (clear site data / unregister the service worker), not server-side.

---

## Unlink behaviour

When you unlink a site that lives inside a parked directory, the vhost is removed but the registry entry is kept and marked as *ignored*; the watcher will not re-register it on its next scan. Running `servlo link` in that directory clears the ignored flag and restores the site.

Either way, unlinking also drops the site's per-site request-timing state: its rows in the durable request store, its entry in the persisted request-timing snapshot, and the running watcher's in-memory copy, so an unlinked site leaves no stale traffic history behind.

---

## Pausing sites

Pausing a site frees up resources without removing it from servlo. It is useful when you're switching focus between projects and want to stop workers and silence a site without fully unlinking it.

```bash
servlo pause              # pause the site in the current directory
servlo pause my-project   # pause a named site
```

When a site is paused:

- All running workers for that site are stopped (queue, schedule, reverb, stripe, and any custom workers)
- The nginx vhost is replaced with a minimal landing page that shows a **Resume** button
- Services no longer needed by any other active site are auto-stopped
- The paused state is persisted, so the site stays paused across `servlo start` / `servlo stop` cycles

The landing page's **Resume** button calls the servlo dashboard API directly, so you can unpause from the browser without opening a terminal.

```bash
servlo unpause              # resume the site in the current directory
servlo unpause my-project   # resume a named site
```

When a site is unpaused:

- The original nginx vhost is restored (including HTTPS if the site is secured)
- Any services referenced in the site's `.env` are started
- Workers that were running before the pause are restarted

Paused sites still appear in `servlo sites` output and the web UI. Their status is shown as `paused`.

### Running CLI commands on a paused site

You can run `php artisan`, `composer`, `servlo db:export`, and other exec-based commands on a paused site without unpausing it first. If any services the site needs (MySQL, Redis, etc.) were auto-stopped when the site was paused, servlo starts them automatically before running the command:

```
$ php artisan migrate
[servlo] site "my-project" is paused, starting required services...
  Starting mysql...

   INFO  Nothing to migrate.
```

On subsequent commands the services are already running, so no notice is printed. The site stays paused; the nginx vhost remains as the landing page and workers are not restarted.

Commands that benefit from this auto-start:

| Command | Notes |
|---|---|
| `php artisan <args>` / `servlo artisan <args>` | Any artisan command |
| `php <args>` / `servlo php <args>` | Any PHP script |
| `composer <args>` | Composer via the servlo shim |
| `servlo db:import` | Imports a SQL dump |
| `servlo db:export` | Exports a database |
| `servlo db:shell` | Opens an interactive DB shell |

---

## Workspaces

Once you have more than a handful of sites, one flat list stops being useful. Workspaces let you group sites the way you actually think about them, separating client work from experiments.

A workspace is purely organisational. It never touches nginx, domains, certificates or `.env`, and it never changes how a site is served. It is also not the same thing as a [site group](site-groups.md), which binds a main site's subdomains together and does rewrite vhosts and certificates. A site can belong to a group and a workspace at the same time.

Workspaces are a personal preference rather than project state, so they live in your global config at `~/.config/servlo/config.yaml` and are never written to `.servlo.yaml` or the site registry:

```yaml
workspaces:
  - name: Client Work
    sites: [astrolov, acme]
  - name: Side Projects
    sites: [blog]
```

A site that appears in no workspace is ungrouped. An empty workspace is fine and survives a restart, so you can create one before you have anything to put in it. The order of the list is the order the sections are shown in. Unlinking a site drops it from its workspace, so a different project linked under the same name later starts out ungrouped.

Only a group main is ever written to the list. A [group secondary](site-groups.md) always displays in its main's workspace, so it has no membership of its own and `servlo workspace assign` will point you at the main instead. The name `none` is reserved: it is how you ungroup a site from the command line, and it labels the ungrouped option in the picker.

### In the web UI

The sites sidebar renders one collapsible section per workspace, followed by the ungrouped sites and then the paused ones. Collapse state is remembered per browser.

Drag a site row between sections to move it. Dragging a [site group](site-groups.md) main carries its secondaries with it, since a secondary always shows in its main's workspace. Drag a workspace header to reorder the sections; that moves whole blocks and never changes the order of sites within them. Rename and delete live in the menu on each header, and deleting a workspace only ungroups its sites, it never removes them. The **Add workspace** button sits next to the sort control at the bottom of the list.

Each site's detail header also has a workspace picker, which can create a new workspace and move the site into it in one step.

The Sites Overview groups its tiles by workspace too. Empty workspaces are hidden there, since the sidebar is where you manage them, and each tile still shows its framework as a badge. Until you create your first workspace the overview keeps grouping by framework, the way it always has.

### In the TUI

Press `o` in the sites pane to cycle the sort order until it reads `sort: workspace`. Sites are then listed under a header per workspace, with the ungrouped ones trailing. The TUI shows workspaces but does not edit them; use the web UI or `servlo workspace`.

---
