# PHP

## Commands

| Command | Description |
|---|---|
| `servlo use <version>` | Set the global PHP version and build the FPM image if needed |
| `servlo isolate <version>` | Pin PHP version for cwd: writes `.php-version` and updates `.servlo.yaml` if it exists, then re-links |
| `servlo php:list` | List all installed PHP-FPM versions |
| `servlo php:rebuild [--local]` | Force-rebuild all installed PHP-FPM images; `--local` builds from source instead of pulling a base |
| `servlo fetch [version...] [--local]` | Pull pre-built PHP FPM base images from ghcr.io; `--local` builds from source instead |
| `servlo php:ext add <ext> [--apk-deps "pkg ..."]` | Add a custom PHP extension to every PHP image and rebuild the current version; `--apk-deps` lists extra Alpine packages the extension needs to build |
| `servlo php:ext remove <ext>` | Remove a custom PHP extension from every PHP image and rebuild |
| `servlo php:ext list` | List your declared extensions, and what each PHP version's image actually loaded |
| `servlo php:bun install [version]` | Install a musl bun inside the PHP-FPM container, into a persistent volume |
| `servlo php:bun remove` | Remove the in-container bun and clear its shared persistent volume |
| `servlo php:bun update [version]` | Update the container's bun in place (`bun upgrade`) |
| `servlo php:bun version [version]` | Show the bun version installed in the container |
| `servlo php:pkg add <package...>` | Install extra Alpine packages into every FPM image and rebuild the current version |
| `servlo php:pkg remove <package...>` | Remove extra Alpine packages from every FPM image and rebuild |
| `servlo php:pkg list` | List your declared packages, and what each PHP version's image actually installed |
| `servlo php:ports add <host:container...> [--php version]` | Publish a host port on the version's FPM container; a bare number publishes the same port straight through, and a busy host port shifts to the next free one |
| `servlo php:ports remove <host...> [--php version]` | Unpublish a host port from the version's FPM container |
| `servlo php:ports list [--php version]` | List the extra host ports published for a PHP version |
| `servlo php:ini [version]` | Open the user php.ini for a PHP version in `$EDITOR` |

If no version is given, the version is resolved from the current directory (`.php-version` or `composer.json`, falling back to the global default).

Versions are written as `major.minor`, but common spellings are accepted everywhere a version is typed: `php8.4`, `84` and `8.4.7` all normalize to `8.4`. Anything that does not resolve to a supported version is rejected up front, so a typo can never end up as the stored default and break image names.

Inside a linked site, the commands that run PHP in a container (`servlo php`, `servlo composer`, `servlo console`) use the version the site is registered on, which is the version its FPM container serves. That matters when a framework clamps the version at link time: a Laravel 13 project pinning `.php-version` to 8.1 is linked on 8.5, because Laravel 13 supports 8.3 to 8.5, and composer then runs on 8.5 too rather than resolving 8.1 from the file and quietly using a different PHP than the site itself.

When a command needs a version that is not installed and you decline the install, servlo offers to switch to one you already have and pins the choice.

So that the project agrees with what actually runs, `servlo link` pins the resolved version into `.php-version`, the same file `servlo isolate` and the dashboard's PHP dropdown write. A pin the framework does not support is rewritten to the version servlo runs, and a version outside the framework's range is clamped rather than accepted, so the file, the site registry and the container can never drift apart. Sites with no servlo-managed PHP version (host-proxy, and custom containers whose version comes from their Containerfile) are left untouched.

---

## Usage

`servlo install` places shims for `php` and `composer` in `~/.local/share/servlo/bin/`, which is added to your `PATH`. You use them exactly as you normally would, servlo routes them through the correct PHP-FPM container version automatically:

```bash
php artisan migrate
composer install
```

Because the `php` shim runs inside the PHP-FPM container, `php artisan` and `servlo artisan` are equivalent; both execute inside the same container with the same PHP version and extensions. Use whichever form you prefer.

Prefer typing `servlo php` explicitly and keeping `php` pointed at a host install? Run `servlo path:disable`: it removes servlo's shims dir from your shell PATH and keeps installs and updates from re-adding it, while every `servlo …` command works unchanged (child processes servlo spawns still resolve the shims internally). `servlo path:enable` reverses it. One thing to know either way: the shimmed `php` runs inside the container, so a PHP script that `exec()`s host tools sees the container's PATH, not your shell's — with the shim disabled, a host `php` behaves like any other host process.

### Shortcuts and `vendor/bin` fallback

For common workflows there are a few built-in shortcuts:

- `servlo a [args...]`: short alias for `servlo artisan` (also `servlo console`)
- `servlo test [args...]`: runs `servlo artisan test`

In addition, any composer-installed binary in the project's `vendor/bin` directory is callable directly as `servlo <name>`. For example, with the usual Laravel dev tooling installed:

```bash
servlo pest
servlo pint
servlo phpstan analyse
servlo rector process
```

These run inside the project's PHP-FPM container with the project's working directory mounted, so configuration files (`pest.xml`, `pint.json`, `phpstan.neon`, etc.) are picked up automatically. Real servlo commands always take precedence; if you have a `vendor/bin/composer`, `servlo composer` still resolves to the built-in command.

---

## Version resolution

When serving a request, Servlo picks the PHP version for a project in this order:

1. `.servlo.yaml` in the project root: `php_version` field (explicit servlo override)
2. `.php-version` file in the project root (plain text, e.g. `8.2`)
3. `composer.json`: `require.php` constraint, resolved to the best installed version (e.g. `^8.4` with PHP 8.4 and 8.5 installed resolves to `8.5`)
4. Global default in `~/.config/servlo/config.yaml`

When `.php-version` changes on disk, the servlo watcher automatically updates the site registry and regenerates the nginx vhost, no manual reload needed.

To pin a project permanently:

```bash
cd ~/sites/myapp.example.com
servlo isolate 8.5
```

This writes `.php-version: 8.5` (so CLI `php`, asdf, and other tools see the right version) and, when `.servlo.yaml` already exists in the project, also updates its `php_version` field to keep servlo's priority-1 override in sync. The site is re-linked automatically so nginx picks up the new version immediately.

The UI PHP version selector follows the same rules; it always writes both files when applicable.

The composer constraint is matched against all installed PHP versions using full semver rules (`^`, `~`, `>=`, `<`, `||`, `*`). The highest installed version that satisfies the constraint wins. If no installed version matches, the literal minimum from the constraint is used (and the FPM will be built on first use).

::: tip Overriding a `composer.json` constraint
If `composer.json` requires `^8.3` but you need to run the project on a specific version, `servlo isolate 8.5` is the right tool. It writes `.php-version` which takes priority over the composer constraint. Running `servlo use 8.5` alone won't help; that only sets the global fallback, which loses to the composer constraint.
:::

To change the global default (applies to all projects that don't have a per-project pin):

```bash
servlo use 8.5
```

## Framework PHP ranges

Each framework version declares the PHP range it supports (for example Laravel 11 supports PHP 8.2 to 8.4). Servlo clamps the resolved PHP version into that range so a site never runs on a version its framework can't boot, and the PHP version picker in the dashboard and the TUI shows out-of-range versions as disabled rather than hiding them, so the constraint is visible.

The range comes from the framework definition that matches your project. Servlo detects the framework major version from `composer.json` (for Laravel, the `laravel/framework` constraint) and loads that version's definition.

### Legacy projects

When a project's framework version predates every definition servlo ships, servlo serves it with the **lowest** available definition instead of the latest. A Laravel 6 project, for instance, is served by the Laravel 10 definition rather than Laravel 13.

In that case the version is a best-effort guess: the definition targets a newer framework than your project, so its PHP range is **not** enforced. PHP clamping is relaxed and every installed version stays selectable, which lets a legacy Laravel 6 app keep running on PHP 7.4 even though the Laravel 10 definition asks for 8.1 and up. Pin the version you want with `servlo isolate 7.4`.

Newer-than-shipped versions fall back to the latest definition as before, with its range enforced normally.

---

## FPM lifecycle

Servlo automatically manages which PHP-FPM containers are running based on which versions are actually needed by your sites.

**`servlo start`**: only starts FPM containers for versions referenced by at least one site (active or paused). Unused versions are left stopped.

**Auto-stop**: when you unlink a site, servlo checks every installed PHP version. If no remaining active (non-ignored, non-paused) site uses a version, its FPM container is stopped. The version itself stays installed; the container is just not running.

**Paused sites count**: a site that is paused still counts as using its PHP version, so that version's FPM container is not stopped. When the site is resumed, FPM is guaranteed to be running.

**Auto-start**: FPM is started automatically when you link a site (`servlo link`, `servlo park`, `servlo isolate`) or change the global default (`servlo use`). When unpausing a site, servlo also ensures the required FPM container is running before restoring the nginx vhost.

**Build on first use**: when a link lands on a PHP version this machine has never built (an older framework clamps to a version below the ones you have, say), `servlo link` builds that version's image before starting it, so the site serves rather than answering 502. The build streams its progress as a link step. If the build cannot run (an unattended `servlo park` sweep withholds builds) or fails, the site is still registered and servlo names the one command that finishes the job, `servlo php:rebuild <version>`.

**Manual control**: unused PHP versions (no active sites) can be started and stopped manually from the dashboard (System > PHP > Start / Stop). From the CLI:

```bash
systemctl --user start  servlo-php84-fpm
systemctl --user stop   servlo-php84-fpm
```

**`servlo status`**: stopped FPM containers for unused versions are reported as a warning, not an error.

## Pre-built images

servlo ships pre-built PHP-FPM base images on ghcr.io for all supported versions (7.4 and 8.0–8.5), covering both `amd64` and `arm64`. When you run `servlo fetch` or `servlo php:rebuild`, servlo pulls the matching base image and layers only your custom extensions and packages on top, bringing first-time build time from ~5 minutes down to ~30 seconds.

The base image tag is derived from the embedded Containerfile, so servlo always pulls the exact image that matches the version of servlo you have installed. If the pull fails (no internet, image not yet published) servlo falls back to a full local build transparently.

The images are public, so no ghcr.io login is required. servlo pulls them anonymously even if you are already logged into ghcr.io, to avoid authentication errors from expired or unrelated credentials.

`servlo start` checks all required images before starting containers. If any are missing (e.g. after `podman image rm`), it rebuilds or pulls them automatically using the same parallel spinner UI, so containers always start against a valid image.

To build entirely from source instead:

```bash
servlo fetch --local
servlo fetch --local 8.5
servlo php:rebuild --local
```

### When the base image is refreshed

The base image tag is a hash of the recipe, so an upstream `php:X.Y-fpm-alpine` refresh, including a security patch Alpine has already shipped, republishes the same tag with new content. Nothing about your machine changes when that happens, so servlo records the digest of the base each image was built from and compares it against what the registry serves now, a manifest lookup with no pull. When they differ, the version is flagged as having an update available.

You see it in three places. **System → PHP** marks the version card with an up arrow and offers "Rebuild on the new base" as its first action, which streams the rebuild the same way an install does. The same menu has **Check for updates**, which bypasses the cached digest and asks the registry right now. And `servlo doctor` reports the version as a warning with `servlo php:rebuild <version>` as the fix, so `servlo doctor --fix` picks it up too. If push notifications are on, a version that becomes stale while the dashboard is open announces itself like a service update does.

The check is cached for six hours and never runs on the dashboard's critical path, so an offline machine stays quiet rather than reporting a false update. A version whose image was built locally (`--local`) has no recorded base and is never flagged: there is no published image behind it to compare against.

On the publishing side, an upstream refresh rebuilds the hash tag main computes plus the ones the last two stable releases resolve to, each from its own git checkout. Staying a release or two behind still gets you the patched base without updating servlo first.

---

## Legacy PHP versions

PHP 7.4 and 8.0 are available as a frozen legacy tier for old projects (Laravel 6–8 on 7.4, Laravel 8–9 on 8.0). They build from the same Alpine-based recipe as the current versions, including ICU full locale data, but with a few caveats:

- They are end-of-life upstream and get no security patches. Use them only for local work on legacy apps.
- The `mongodb` extension is unavailable (it requires PHP 8.1+), and so is `random` (a PHP core extension only from 8.2); everything else in the standard bundle is present.
- The base image is Alpine 3.16, so the bundled Node.js is 16.x.

Use them like any other version:

```bash
servlo use 7.4
servlo isolate 8.0
servlo fetch 7.4 8.0
```

---

## Custom extensions

The default servlo FPM image ships ~30 extensions covering the vast majority of Laravel projects (`bcmath`, `bz2`, `calendar`, `curl`, `dba`, `exif`, `ftp`, `gd`, `gmp`, `igbinary`, `imagick`, `intl`, `ldap`, `mbstring`, `mongodb`, `mysqli`, `opcache`, `pcntl`, `pdo_mysql`, `pdo_pgsql`, `pdo_sqlite`, `redis`, `soap`, `shmop`, `sockets`, `sqlite3`, `sysvmsg`, `sysvsem`, `sysvshm`, `xsl`, `zip`, and more).

Two of those names are version-gated, because the image genuinely cannot build them everywhere: `random` is a PHP core extension only from 8.2, and `mongodb` builds only on 8.1 and up. On older versions they are not part of the bundle, and `servlo park` warns when a project requires one rather than staying quiet and letting `composer install` fail its platform check.

To add an extension that isn't in the bundle:

```bash
servlo php:ext add swoole
```

Extensions belong to you, not to a PHP version. One declared set applies to every PHP image servlo builds, so a site that changes version keeps them. The version you are on is rebuilt and verified straight away; other installed versions carry the old set until they are rebuilt, and servlo says which ones those are.

Those deferred versions are rebuilt by the next command that touches them, which is usually `servlo use`, `servlo link`, `servlo fetch`, `servlo unpause` or `servlo start`. When that happens to a version whose container is already running, servlo restarts the container onto the image it just built, so the running PHP always matches what `servlo php:ext list` and the dashboard report for it.

Extensions are persisted in `~/.config/servlo/config.yaml` under `php.extensions`, so they survive `servlo php:rebuild`.

After the rebuild, servlo checks that the extension actually loaded (`php -m`); if the PECL build failed, `servlo php:ext add` exits with an error and removes the extension from the config again, rather than reporting success for an extension that isn't there. A rebuild that fails outright is reverted the same way, so a name that cannot build is not left declared and retried by every command after it.

#### What each version actually loaded

A declared set cannot always be honoured. `mongodb` does not build below 8.1, and the legacy 7.4 and 8.0 images are Alpine 3.16, where some packages do not exist. servlo records what each version's image really loaded, verified after its build, and never advertises what an image does not have:

```bash
servlo php:ext list
```

```
Declared, for every PHP version:
  - mongodb
  - swoole

Per version:
  PHP 7.4  swoole (cannot load: mongodb)
  PHP 8.1  image predates this set, run 'servlo php:rebuild 8.1'
  PHP 8.4  mongodb, swoole
  PHP 8.5  mongodb, swoole
```

The three states are different problems. An image that **predates the set** was built before you declared something, and a rebuild fixes it. Something an image **cannot load** did not build on that version, and a rebuild will not change that. A version with no image at all is not listed. Nothing is reported as present unless that image really has it.

The dashboard shows the same thing, plus every module the image loads, under **System → PHP → Extensions**. The module list is `php -m` read from the image itself, so it is what your code will actually see; it is fetched when you open the tab and cached against the image, so a rebuild refreshes it and nothing else pays for it. The TUI's System view carries a shorter `Extras · PHP <version>` line per version, from the same recorded data.

Changing a site's PHP version is exactly when it would silently lose an extension, so servlo checks the target image at that moment. An image built before you declared something can be brought up to date with a rebuild:

```
$ servlo isolate 8.3
 ✓ PHP pinned to 8.3
 ⚠ PHP 8.3's image predates your custom extensions and packages
       run 'servlo php:rebuild 8.3' to bring it up to date
```

An extension that genuinely cannot build on that version is reported differently, because no rebuild will fix it.

One that loaded on **no version at all** is reported differently again. A version boundary shows up on some versions and not others, so an extension missing from every one of them is usually the build failing rather than the versions refusing it, and `servlo php:ext list` says so instead of reading it as a capability gap.

Some extensions need extra Alpine packages to compile. servlo already knows the ones for `imap` (`imap-dev krb5-dev openssl-dev c-client`); for anything else, pass them with `--apk-deps`:

```bash
servlo php:ext add ssh2 --apk-deps "libssh2-dev"
servlo php:ext add imap                                  # deps known to servlo, no flag needed
```

The packages are saved alongside the extension in `~/.config/servlo/config.yaml` (under `php.ext_apk_deps`), so they reapply on every `servlo php:rebuild`.

```bash
servlo php:ext list                # show your custom extensions and their apk deps
servlo php:ext remove swoole       # remove from every version and rebuild
```

### Per-site PHP settings

Every site gets its own PHP-FPM pool. That is what makes a PHP setting belong to a site rather than to a PHP version: one site can have a 256M upload limit and a 512M memory limit while every other site on the same version keeps the defaults.

There is still one FPM master process per PHP version, because a container per site would be twenty containers on a twenty-site droplet. What is per site is the pool inside it. Servlo writes one pool file per site to `~/.local/share/servlo/fpm-pools/<container>/<site>.conf`, mounted read-only into that container as `php-fpm.d`, and each pool listens on its own unix socket at `~/.local/share/servlo/run/fpm/<site>.sock`. The site's nginx vhost passes to that socket, so a request arrives at the pool carrying that site's settings.

The directory is per container rather than one for all of them on purpose. An FPM master defines every pool it can see and binds every socket those pools listen on, so two masters sharing a directory would each define the other's sites and whichever started last would answer for all of them, quietly serving an 8.3 site on 8.4.

Writing or removing a pool reloads the master with `SIGUSR2` rather than restarting the container. FPM re-reads its configuration, starts new workers and lets the running ones finish, so changing one site's upload limit does not interrupt another site's checkout.

#### Changing a site's PHP version

Switching a site to another PHP version moves its pool with it, into the directory belonging to the new version's container, and removes the one it left behind. Both masters are reloaded: the version the site came from so it stops defining a pool for a site it no longer serves, and the version it moved to so it picks the pool up. The vhost is rewritten to the same socket in the same step, and it is rewritten after the pool exists rather than before, because the vhost points at the socket only once there is a pool listening on it.

Neither reload is a restart, so no other site on either version drops a request while this happens.

#### Changing them from the panel

Open a site, then **Settings**. Three fields:

| Field | What it writes |
|---|---|
| Max upload size | PHP `upload_max_filesize`, PHP `post_max_size`, nginx `client_max_body_size` |
| Max execution time | PHP `max_execution_time`, nginx `fastcgi_read_timeout` and `fastcgi_send_timeout` |
| Memory limit | PHP `memory_limit` |

Three fields rather than six on purpose. Max upload size is one decision that has to reach three directives: raise `upload_max_filesize` alone and `post_max_size` refuses the request, raise both and nginx refuses it before PHP is even asked. Whichever one stayed low is the one that fails, and it reads to an operator as the setting not working. Max execution time is the same shape, in two places instead of three: whichever of PHP and nginx gives up first is the one the visitor experiences, so an import set to 600 seconds in PHP alone still dies at nginx's 60.

Saving writes the pool and the vhost together from the same saved values, runs `nginx -t` before committing the vhost, and reloads both. Leaving a field empty means the default: the directive is removed rather than pinned to whatever it last was.

The same settings live in the site registry at `~/.local/share/servlo/sites.yaml` as `max_upload_mb`, `max_execution_seconds` and `memory_limit_mb`.

Two production settings are pinned in every pool and cannot be turned back on from an application's own `ini_set`:

```
php_admin_value[display_errors] = Off
php_admin_value[expose_php] = Off
```

A site that sets nothing gets a pool with only those, and everything else still comes from the layered ini files below. A site created before per-site pools existed has no pool file, and keeps being served by the shared container until it gets one.

#### A site that runs its own container

A FrankenPHP site has no pool, because it is not served by the shared FPM container. The same three settings go into `~/.local/share/servlo/php/sites/<site>/96-servlo-site.ini`, which that site's own container mounts into its `conf.d`. Servlo owns that file and rewrites it on every save, and the site's container is restarted so the change takes effect, which it is not if only the file changes.

The numbering is the ranking. `95-servlo-shared.ini` is the baseline for every site, `96-servlo-site.ini` is this site's settings from the panel, and `98-user.ini` is whatever you wrote by hand, which loads last and wins. Editing `96` is pointless: the next save overwrites it.

A custom-container site and a host-proxy site get neither. They run something servlo did not build, so there is no file of servlo's their runtime would read; on those the settings reach nginx alone, which is all servlo has to reach.

### php.ini settings

Each PHP version has a user-editable ini file at `~/.local/share/servlo/php/<version>/98-servlo-user.ini`, mounted read-only into the FPM container. Edit it with:

```bash
servlo php:ini          # detected/default version
servlo php:ini 8.3      # explicit version
```

This opens the file in `$EDITOR` (falls back to `nano`/`vim`). Saving restarts the affected FPM containers automatically, so the change applies straight away.

The file is created automatically with commented-out examples when servlo first sets up the PHP version.

#### Shared settings across versions

A setting placed in a per-version file only applies to that version, so a site that changes PHP version silently loses it. For a setting you want everywhere, edit the shared file instead:

```bash
servlo php:ini shared   # applies to every PHP version
```

The shared file lives at `~/.local/share/servlo/php/shared/95-shared.ini` and is mounted into every PHP container (FPM, custom-image FPM, and FrankenPHP) below the per-version `98-servlo-user.ini`. Because `conf.d` loads alphabetically and the last file wins, layering happens for free:

- A key set only in the shared file applies to all versions.
- A key set in both files takes the per-version value on that version, and the shared value everywhere else.

Nothing is merged and nothing is migrated: your existing per-version files keep working and keep winning. A key becomes shared only when you put it in the shared file. An unknown or removed directive on a given version is ignored (a startup notice, not a fatal), so a version-specific setting never breaks the others.

In the web UI, open **System → PHP**, pick a version, and use the **Editing** dropdown at the top of its php.ini tab to switch to the shared file.

### Locales and internationalisation

The FPM image is Alpine-based, so it uses musl libc rather than glibc. Two consequences worth knowing:

- **`ext-intl` (`NumberFormatter`, `IntlDateFormatter`, Laravel's `Number::currency()`, `money` formatting) works for every locale.** The image bundles ICU's full CLDR locale database (`icu-data-full`), so `new NumberFormatter('nl_NL', NumberFormatter::CURRENCY)` correctly produces `€ 13.943,20`. This is the recommended way to do locale-aware formatting and it does not depend on the system locale at all.
- **The C-library `setlocale()` / `localeconv()` path stays in the C locale.** musl does not implement locale-specific `LC_NUMERIC` / `LC_MONETARY` rules, so `setlocale(LC_ALL, 'nl_NL')` will return a value but `localeconv()` keeps returning `.` / empty separators, and `number_format()` without explicit separators won't switch. Pass separators explicitly (`number_format($n, 2, ',', '.')`) or use `ext-intl`.

If a library you depend on calls `setlocale()` and branches on whether it succeeded, adding the `musl-locales` / `musl-locales-lang` apk packages makes the call return a value, but it still will not change number or currency formatting.

---

## Custom image (Containerfile)

When `php:ext` and per-version ini tweaks are not enough and a single site needs its own bespoke image (an extra system toolchain, a patched binary, arbitrary build steps), you can give that PHP site its own `Containerfile.servlo`. servlo builds a per-site image and serves the site by fastcgi from a dedicated FPM container, instead of the shared `servlo-php<ver>-fpm` one. It is the same `container:` key used for [custom containers](../getting-started/containers.md), with one difference: **no port**. A `container:` block with a port is a reverse-proxied app; a `container:` block with no port on a PHP project is served by fastcgi from your image.

Your `Containerfile.servlo` must build `FROM` the servlo base image for the site's PHP version, so it keeps php-fpm, the bundled extensions, and the pool config. That `:local` tag is servlo-managed and rebuilt on updates, so the `FROM` stays valid:

```dockerfile
FROM servlo-php84-fpm:local
RUN apk add --no-cache htop vim
```

```yaml
# .servlo.yaml
domains:
  - myapp
container:
  containerfile: Containerfile.servlo
```

Then `servlo link`. servlo builds `servlo-custom-myapp:local`, runs a dedicated FPM container `servlo-cfpm-myapp`, and points nginx fastcgi at it. The per-site container reuses every servlo mount, so the debug bridge works exactly as on a normal PHP site, and `servlo php`, `artisan`, `composer` and queue/horizon workers all run inside it.

The PHP version is fixed by the `FROM` line, not by `.php-version` or the dashboard, so the version selector is shown read-only for these sites. To change the version, edit the `FROM` and relink.

```bash
servlo rebuild        # rebuild the per-site image after editing Containerfile.servlo
servlo restart        # restart the container without rebuilding
```

::: info PHP projects only
A no-port `container:` is for PHP projects served by fastcgi. For a non-PHP app (Node, Python, Go) give the `container:` block a `port` so nginx reverse-proxies to it; see the [containers walkthrough](../getting-started/containers.md).
:::

::: warning One container per site
Each custom-image PHP site runs its own FPM container rather than sharing the per-version one, so it uses more memory in the Podman VM. Reach for it only when a site genuinely needs its own image; for adding an extension or a package to every site on a version, `php:ext` stays lighter.
:::

---

### Reachable ports

By default a TCP port you open by hand inside the container is not reachable at `localhost:PORT` on the host, because the PHP-FPM container has no published host ports of its own. Framework servers like [Reverb](queue-workers) or an in-container Vite worker are reachable, but only because a worker exposes them through the nginx proxy on the site's domain, not as a raw localhost port. When you instead want to run a process directly in the container (a Vite dev server, a websocket, an ad-hoc HTTP listener) and hit it straight from a host browser or tool, publish the port on the version's FPM container:

```bash
servlo php:ports add 5173        # localhost:5173 -> container 5173 (same port through)
servlo php:ports add 8080:80     # localhost:8080 -> container 80
servlo php:ports list
servlo php:ports remove 5173
```

The same list is available in the dashboard under **System > PHP > (version) > Ports**. If the host port is already taken (by a servlo service, another PHP version's list, or any other listener) servlo shifts it to the next free one and tells you where it landed, so an add never fails on a collision. Ports bind loopback and stay there, like every servlo port that is not nginx. Changing the list restarts that version's FPM container, so PHP bounces for every site on that version.

This is a per-version pool, not per site. There is one shared FPM container per PHP version serving every site on it, so a published port maps to whichever single process binds it inside, and two sites wanting the same in-container port on the same version collide. It is a power-user escape hatch: for anything you can reach for a blessed path instead, prefer host-proxy (run the dev server on the host) or a worker with a proxy, which both scope cleanly to a single site.

---

### Composer.json detection

When you run `servlo park` or `servlo link`, Servlo reads `composer.json` and warns if any `ext-*` requirements are not covered by the bundled or installed extension set:

```
[!] my-app requires PHP extensions not in the image: swoole
    Run: servlo php:ext add swoole
```

An extension can also be one the site's PHP version cannot have at all, rather than one that is merely absent from the image. `random` is a PHP core extension only from 8.2, and `mongodb` builds only on 8.1 and up, so on an older site no image rebuild can supply them and `servlo php:ext add` would fail after several minutes of building. Servlo says so up front instead of offering an install that cannot work:

```
[!] my-app requires ext-random, which is not available on PHP 8.1 (first shipped on 8.2)
    servlo php:ext add cannot build it. Move the site to PHP 8.2 or newer, or require a polyfill package instead.
```

A requirement can also fail even though the extension is in the image, because composer names a few extensions differently from the name they are installed under. Composer builds its `ext-*` names from the module name PHP reports, and OPcache reports itself as `Zend OPcache`, so composer publishes `ext-zend-opcache` and never `ext-opcache`. A `composer.json` asking for `ext-opcache` therefore fails its platform check on `composer install` even though OPcache is loaded, and `servlo php:ext add opcache` will not help because nothing is actually missing. Servlo recognises both spellings and tells you which one composer wants:

```
[!] my-app requires ext-opcache, which composer publishes as ext-zend-opcache
    The extension is in the image; composer install will still fail its platform check.
    Require ext-zend-opcache in composer.json instead.
```
