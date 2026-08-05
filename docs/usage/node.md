# Node

## Commands

| Command | Description |
|---|---|
| `servlo node:install <version>` | Install a Node.js version globally |
| `servlo node:uninstall <version>` | Uninstall a Node.js version |
| `servlo node:use <version>` | Set the global default Node.js version |
| `servlo isolate:node <version>` | Pin Node version for cwd: writes `.node-version` and installs the version |
| `servlo node:manage` | Opt into servlo-managed Node: install the version-manager shims and a default version |
| `servlo node:unmanage` | Stop managing Node: remove servlo's shims (and, with fnm, the versions it installed) for a clean system |
| `servlo js:runtime [bun\|node\|auto]` | Pin the current site's JS runtime (or show it with no argument) |
| `servlo php:bun install [version]` | Install a musl bun inside the PHP-FPM container |
| `servlo php:bun remove` | Remove the in-container bun and clear its shared persistent volume |
| `servlo php:bun update [version]` | Update the container's bun in place (`bun upgrade`) |
| `servlo php:bun version` | Show the bun version installed in the PHP-FPM container |

---

## Usage

`servlo install` places shims for `node`, `npm`, and `npx` in `~/.local/share/servlo/bin/`, which is added to your `PATH`. You use them exactly as you normally would, servlo picks the right version automatically:

```bash
node --version
npm install
npx tsc --init
```

---

## Package manager

The package manager is the project's choice, not servlo's. servlo detects it and routes every install, dev and build through it, so a pnpm project is never quietly installed with npm.

Detection runs in this order:

1. A `packageManager` field in `package.json` (`"pnpm@9.1.0"`, `"yarn@4.2.0"`, `"npm@10"`, `"bun@1.1"`), the Corepack convention and the most explicit signal
2. The lockfile: `pnpm-lock.yaml` → pnpm, `yarn.lock` → yarn, `bun.lockb` / `bun.lock` → bun, `package-lock.json` → npm
3. npm, as the fallback

pnpm and yarn run through [Corepack](https://nodejs.org/api/corepack.html), which ships with the Node servlo manages, so neither needs a separate global install. Corepack is enabled on demand the first time a project needs it.

Installs use each manager's frozen-lockfile mode, so a servlo install never silently rewrites your lockfile:

| Manager | Install | Dev | Build |
|---|---|---|---|
| npm | `npm ci` | `npm run dev` | `npm run build` |
| pnpm | `pnpm install --frozen-lockfile` | `pnpm run dev` | `pnpm run build` |
| yarn | `yarn install --immutable` | `yarn dev` | `yarn build` |
| bun | `bun install` | `bun run dev` | `bun run build` |

This is what the project-setup wizard runs, and what the Vite host worker runs for HMR. The wizard's step labels follow the detected manager, so a pnpm project shows **pnpm install** rather than **npm ci**.

Nothing needs configuring. If you want to change the manager, change the project's `packageManager` field or its lockfile and servlo follows.

---

## Version resolution

1. `.servlo.yaml`: `node_version` field (explicit servlo override, highest priority)
2. `.nvmrc` in the project root
3. `.node-version` in the project root
4. `package.json`: `engines.node` field
5. Global default in `~/.config/servlo/config.yaml`

`.nvmrc` and `.node-version` are reduced to their major version, so `20.11.0` in either means Node 20. `.servlo.yaml` is not, so a full pin like `20.11.0` is used as written and reaches the version manager verbatim.

Every source has to be version shaped: letters, digits, dots, dashes, underscores and slashes, so `22`, `20.11.0`, `v18.20.4` and `lts/iron` all work. A value carrying anything else is ignored and resolution falls through to the next source. These values are put on the command line of the worker units servlo generates, and `.servlo.yaml` is committed to the repository, so a checkout can never decide what those units run.

To pin a project to a specific version:

```bash
cd ~/Servlo/my-app
servlo isolate:node 20
# writes .node-version and installs Node 20 via the active version manager
```

To install a version without pinning a project:

```bash
servlo node:install 22
```

---

## Default version

`servlo node:use <version>` sets the global default and stores it in `~/.config/servlo/config.yaml`. Sites without a pinned version use this default.

```bash
servlo node:use 22
```

Version numbers are normalised to the major only, so `22.11.0` and `22.14.1` are both treated as `22`, and only one entry per major appears in the UI and CLI.

---

## Version manager: fnm or nvm

Node version management runs through a version manager, and servlo supports two: [fnm](https://github.com/Schniz/fnm), the bundled default, and [nvm](https://github.com/nvm-sh/nvm), if you already have it installed.

- **fnm** is bundled and installed automatically. servlo writes `node` / `npm` / `npx` shims into `~/.local/share/servlo/bin/` so those commands reach fnm from any shell.
- **nvm** is never installed by servlo. If `servlo install` finds an existing nvm (via `$NVM_DIR` or `~/.nvm`), it picks nvm automatically when you decline servlo-managed Node, so Node stays yours and servlo follows your nvm instead of an fnm it would never install a version into. Answering yes keeps the bundled fnm; switch to nvm afterwards with `servlo node:manager nvm` or the dashboard. With nvm, servlo does **not** put node/npm/npx shims on PATH, your shell's nvm keeps owning those binaries (so `which node` and `nvm ls` behave normally). `servlo node` / `servlo npm`, host workers, and the dashboard still drive nvm for install, use, and defaults. servlo never touches the Node versions you installed yourself with nvm.

Installing servlo from a package rather than interactively answers the same question by detection, since a maintainer script has nobody to ask. An existing nvm takes it, exactly as declining does above. With no nvm, servlo manages Node through the bundled fnm and installs the default version during setup, so the package leaves you a working Node rather than one that appears the first time something reaches for it.

The choice is stored in `~/.config/servlo/config.yaml` under `node.manager` (`fnm` or `nvm`) and, for nvm, `node.nvm_dir` (so `servlo-ui` and the watcher find nvm even without your shell rc). Switch later with `servlo node:manager fnm|nvm` or from the dashboard's Node page, which shows an **fnm / nvm** toggle (hidden when nvm is not installed, since there is nothing to switch to). Switching to fnm downloads fnm on demand if an earlier nvm-only install skipped it. Switching updates PATH shims (write them for fnm, remove them for nvm) and re-syncs host workers so the new manager takes effect at once. `node:install` and `node:use` act on whichever manager is active; `node:uninstall` and `node:unmanage` only ever remove Node versions when servlo owns them (fnm) and leave your nvm-installed versions alone.

---

## Global npm packages

With **fnm**, `npm install -g <pkg>` works through the servlo shim. The package goes to a servlo managed prefix at `~/.local/share/servlo/node-global/`, and servlo writes a small wrapper script for every binary into `~/.local/share/servlo/bin/`, which is already on your `PATH` because `servlo install` adds it. After `npm install -g pm2` you can call `pm2` from any shell directly, no extra setup.

The wrapper exec's the real binary through the active version manager's default version, so globally installed tools always run on the default node version regardless of the project you are inside when you call them. If you need a specific version for a global tool, change the default with `servlo node:use <version>` before installing it.

`npm uninstall -g <pkg>` removes the wrapper as well. Files in `~/.local/share/servlo/bin/` that servlo did not create with its own marker comment are never touched, so the existing `node`, `npm`, `npx`, `php`, `composer`, and `laravel` shims in the same directory stay safe.

With **nvm**, bare `npm` is your nvm npm (no servlo shim). Use `servlo npm install -g …` when you want the managed prefix and PATH wrappers.

If you configured your own npm prefix — `npm_config_prefix` / `NPM_CONFIG_PREFIX` in the environment or a `prefix=` line in `~/.npmrc` — servlo respects it: `npm install -g` lands in *your* prefix, no wrappers are written, and your globals never depend on servlo. The managed prefix is only used when you have none of your own.

Globals that did land in the managed prefix are never silently lost. `servlo node:unmanage` lists them, leaves them on disk, and removes the now-dead wrappers so a reinstall with your own npm is not shadowed. `servlo uninstall` with data removal offers to reinstall them with your system npm before deleting anything, and otherwise prints the exact `npm install -g …` line to run afterwards.

The same mechanism applies to `composer global require`. Composer's global vendor/bin (`~/.config/composer/vendor/bin/` by default, respecting `COMPOSER_HOME` and `XDG_CONFIG_HOME`) is mirrored into `~/.local/share/servlo/bin/` after every `composer` run, with wrappers that exec the real bin through `servlo php` so `#!/usr/bin/env php` shebangs resolve against the FPM container. After `composer global require psy/psysh` you can call `psysh` from any shell directly. `composer global remove` cleans the wrapper too.

---

## System-managed vs servlo-managed Node

If `servlo install` detects an existing `node`, `npm`, or `npx` on your `PATH` or under a known version-manager directory (nvm, volta, mise, asdf, fnm), it asks **"Let servlo manage Node.js?"** before changing anything. An installed nvm triggers the question too, even with no Node versions in it yet, since nothing else would reveal it.

- **Answer yes**: servlo sets up the bundled fnm, picks the current LTS, and sets it as the default, writing the `node` / `npm` / `npx` shims into `~/.local/share/servlo/bin/`. Per-project version pinning works as described above (`.node-version` / `.nvmrc`). Switch it to your own nvm afterwards with `servlo node:manager nvm`.
- **Answer no**: servlo writes no node shims, removes any stale ones from a previous opt-in, and stays out of Node on `PATH`. Sites use whatever `node` your shell resolves; per-project pinning is your version manager's job. When you have nvm, that is what `servlo npm`, `servlo npx`, and site setup run through, so they follow your nvm versions rather than failing on an empty fnm, host workers get the version pinned for the site out of the same nvm, and no fnm is downloaded at all. The dashboard's Node tab disables the install controls and points back at `servlo install` if you change your mind.

With unmanaged Node, host workers (Vite and other `host: true` workers) run against a controlled `PATH` that never includes your login shell's version-manager hooks. servlo therefore resolves where your `node` and `npm` actually live when it generates the worker unit and bakes that directory into it, so a shell-hooked nvm Node or a snap Node works even after a reboot. When `node.manager` is `nvm`, that install wins and the worker gets the same version servlo resolves for the site (`.servlo.yaml`, `.nvmrc`, `.node-version`, `package.json` engines, then the global default), falling back to your nvm default when that version is not installed, since servlo never installs into your nvm. That matters because `servlo npm` in the project runs the same version, and a worker on a different one would run packages that were installed under another Node. Otherwise it checks `PATH` first, plus the install prefixes a user unit's restricted PATH leaves out, so a unit written by the dashboard resolves the same Node as one written by the CLI, then the known version-manager directories (nvm, volta, mise, asdf, a self-installed fnm), then snap and linuxbrew. If nothing resolvable is found (and bun isn't installed either), the worker is held back with a message telling you to install Node or run `servlo install`, instead of crash-looping on `npm: command not found`.

`servlo node:install` / `node:use` / `node:uninstall` warn and require confirmation if you run them on a host where servlo isn't currently managing Node, and opt you in on accept so CLI matches the install flow.

You can flip the choice at any time without re-running the whole installer:

- `servlo node:manage` opts in (writes fnm shims when using fnm, or just records managed mode for nvm) and installs a default version.
- `servlo node:unmanage` removes any node/npm/npx shims and, when servlo owns the manager (fnm), uninstalls the Node versions it installed, leaving a clean system so your own Node (or bun) is used directly. With nvm it only clears managed mode: your nvm versions stay put.

Both also regenerate any host worker units (Vite and other `host: true` workers) so they switch between the managed Node, your system Node, and bun to match the new state. The dashboard and Settings exposes the same toggle: the Node page shows a **Let servlo manage Node** / **Stop managing** button.

The question is asked once and then remembered in `~/.config/servlo/config.yaml` (`node.managed`, alongside `node.manager` for the fnm/nvm choice). After that, neither `servlo install` nor `servlo update` asks again or undoes your choice, you change it only with `servlo node:manage` / `servlo node:unmanage`. A config predating this adopts whatever servlo is currently doing (shims present means managed) as the remembered choice without prompting, so existing installs are never re-asked.

---

## bun

servlo works with [bun](https://bun.sh) as a drop-in alternative to the Node + npm toolchain. servlo never installs or version-manages bun: you install it yourself (`curl -fsSL https://bun.sh/install | bash`) and update it with `bun upgrade`. servlo only detects it and routes work through it.

### When servlo uses bun

On the host, servlo runs install, dev (Vite), and build through bun instead of npm when either of these is true:

1. **The project uses bun**, detected from a `bun.lockb` / `bun.lock` / `bunfig.toml` file or a `packageManager: bun` field in `package.json`. The Vite host worker runs `bun run dev`, installs run `bun install`, and builds run `bun run <script>`.
2. **There is no Node available** (you ran `servlo node:unmanage` and have no system Node) but bun is installed. bun then becomes the fallback JS runtime for every project, since it can run the same `package.json` scripts.

If a project looks like a bun project but bun isn't installed, servlo falls back to npm and prints a one-line install hint. Node-managed projects keep using Node unless they opt into bun via a lockfile.

### Pinning the runtime per project

bun is not a perfect Node drop-in: apps with native N-API addons (NestJS with some dependencies, and similar) can crash on bun because its libuv coverage is incomplete. Pin the runtime in `.servlo.yaml` to override detection:

```yaml
js_runtime: node   # or "npm": always use Node/npm, never bun (opts out of the no-Node fallback too)
# js_runtime: bun  # always use bun, even with Node managed and no bun.lockb
```

Use `js_runtime: node` for a site that must run on Node (then install Node on your machine or let servlo manage it), while other sites still use bun. Leave it unset to auto-detect.

You don't have to edit the file by hand. From the site's directory, `servlo js:runtime bun` and `servlo js:runtime node` write the same `js_runtime` field for you, and `servlo js:runtime auto` clears it back to auto-detect. Each one re-syncs the site's host workers so a running Vite/dev worker switches runtime straight away, exactly like the dashboard's bun/Node toggle. Run `servlo js:runtime` with no argument to see the current setting and what it resolves to.

### Lifecycle

Detection is live for display (the dashboard and Settings show a `🥟 bun <version>` chip and switch the runtime label to **JS Runtime** when bun is active) and for any worker generated after bun appears. Existing host worker units are static, so they keep their old command until regenerated. Regeneration happens on:

- `servlo link` / `servlo setup` for that site,
- `servlo node:manage` / `servlo node:unmanage` (rewrites every host worker),
- `servlo update`, which re-syncs host workers to the current runtime when bun is installed (only workers whose command actually changes are restarted).

So if you install bun after a site is already running, the UI reflects it immediately, and a `servlo update` (or re-link) switches the running Vite worker onto bun.

### bun inside the PHP-FPM container

The host bun can't run inside the container (it's built for your host's libc, the container is Alpine/musl), so the container gets its own bun:

```bash
servlo php:bun install        # installs a musl bun into the container, via the bundled npm
servlo php:bun version        # shows what's installed
servlo php:bun remove         # deletes it and clears the volume
```

bun is installed into a persistent volume (`~/.local/share/servlo/bun` mounted at `/root/.bun`), shared across every PHP version and **kept across image rebuilds and pulls** (it lives in the volume, not the image, so a new base image never reinstalls it). The container puts it on `PATH`. Update it in place with `servlo php:bun update`. When bun is installed on the host, `servlo link` / `servlo setup` also installs it into the container automatically. `servlo php:bun remove` clears the volume so the next install starts clean; because the volume is shared it removes bun for every PHP version at once, and the container need not be running.
