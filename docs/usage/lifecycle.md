# Start, Stop & Autostart

Day-to-day lifecycle commands for the entire servlo stack: DNS, nginx, PHP-FPM containers, services, workers, the Web UI and the watcher.

::: tip You don't need to run `servlo start` after installing
`servlo install` already starts everything for you on first run: it boots `servlo-dns`, `servlo-nginx` and the `servlo-watcher`. Services like MySQL or Redis are started on demand the first time something needs them (`servlo service start`, `servlo init`, or `servlo env`). Reach for `servlo start` only after a `servlo stop`, a reboot without autostart enabled, or after you've manually killed containers.
:::

---

## Commands at a glance

| Command | Stops | Starts |
|---|---|---|
| `servlo start` | nothing | DNS, nginx, watcher, all PHP-FPM containers in use, services that were running before stop, queue / schedule / reverb / messenger workers, stripe listeners, Web UI |
| `servlo stop` | All containers and workers above **except** `servlo-dns`. Leaves the watcher, Web UI, and the DNS forwarder alone. | nothing |
| `servlo quit` | Everything `servlo stop` does, **plus** the DNS forwarder, Web UI and watcher. | nothing |

`servlo stop` is the everyday "give my laptop back its CPU" command. `servlo quit` is a full shutdown: use it before a reinstall, a system reboot without autostart, or when you really want servlo out of the way.

---

## `servlo start`

```bash
servlo start
```

Walks the install in dependency order:

1. Pre-flight: checks for **port conflicts** on 53, 80, and 443; refuses to start if another process is bound.
2. Rebuilds or pulls any missing container images (e.g. after a `podman rmi` or a podman cleanup).
3. Boots core: `servlo-dns`, `servlo-nginx`, `servlo-watcher`.
4. Boots every PHP-FPM container that has at least one site referencing its version. Unused PHP versions stay stopped.
5. Boots all installed services that are **not** marked as manually paused (see [Manually stopped services](services.md#manually-stopped-services) for the pause-state contract).
6. Restores per-site workers (`servlo-queue-*`, `servlo-schedule-*`, `servlo-reverb-*`, `servlo-messenger-*`, custom workers) and stripe listeners (`servlo-stripe-*`) from the `workers` list saved in each site's `.servlo.yaml`.
7. Starts the Web UI (`servlo-ui`).

A live spinner shows the per-unit progress. If a single SSL vhost references a missing certificate file, servlo switches that site back to HTTP automatically and continues; one broken cert no longer blocks the whole nginx start.

::: info After a reinstall
If you ran `servlo uninstall` and then reinstalled, worker units and service quadlets are recreated by `servlo start` from each site's `.servlo.yaml`. Sites with a committed `.servlo.yaml` come back fully wired up. Sites without one need their workers restarted manually.
:::

::: info Deleted project directories are auto-cleaned
`servlo-watcher` removes sites from `sites.yaml` whenever their project directory disappears on disk. Two paths do this:

- **Instant**: fsnotify on every parked directory (configured via `servlo park`). When a direct subdirectory gets deleted, the corresponding site is unlinked within milliseconds.
- **Periodic**: every 30 seconds the watcher sweeps the full site registry (parked and non-parked) and removes any site whose path no longer exists. The UI refreshes via the sites eventbus so the dashboard reflects the removal without a manual page reload.

Both paths skip `Ignored: true` sites, those are explicitly parked by the user (e.g. via `servlo unpark` leaving a tombstone) and must not be reaped.
:::

---

## `servlo stop`

```bash
servlo stop
```

Stops everything `servlo start` started **except** the Web UI, watcher and the `servlo-dns` forwarder; those keep running so the dashboard stays reachable to bring servlo back up.

A few important details:

- **The DNS forwarder stays up.** `servlo-dns` is treated as install-level plumbing: the system resolver keeps pointing `.test` at it until `servlo uninstall`, so stopping it would leave the resolver aimed at a dead port and make `.test` lookups stall. It is only torn down by `servlo quit` or `servlo uninstall`.
- **Manually paused services are remembered.** If you stopped Mailpit earlier with `servlo service stop mailpit`, then `servlo stop` + `servlo start` will not bring Mailpit back. The pause flag survives the cycle.
- **Pinned services start anyway.** A `servlo service pin <name>` overrides auto-stop logic; pinned services are always started by `servlo start` regardless of which sites are active.
- **Worker state is preserved.** Workers running before `servlo stop` are restarted by the next `servlo start`; workers you manually stopped stay stopped.

---

## `servlo quit`

```bash
servlo quit
```

The full off-switch:

1. Runs everything `servlo stop` does.
2. Stops `servlo-ui` (Web UI).
3. Stops `servlo-watcher`.
4. Stops the `servlo-dns` forwarder. Unlike `servlo stop`, quit is a full teardown, so it takes DNS down too. The watcher is stopped first (step 3) because it is the only thing that would restart `servlo-dns`.

After `servlo quit` there are no servlo processes left running. This is the right command before a reinstall, a system reboot, or before pulling a major update.

## Autostart on login

Servlo can boot itself every time you log in. Autostart is a single switch over every servlo-owned systemd user unit on the machine:

- the dashboard (`servlo-ui.service`) and project watcher (`servlo-watcher.service`)
- every container quadlet (`servlo-mysql`, `servlo-nginx`, `servlo-redis`, `servlo-postgres`, `servlo-dns`, `servlo-php*-fpm`, `servlo-mailpit`, `servlo-meilisearch`, `servlo-minio`, `servlo-rustfs`)
- every per-site worker, queue, schedule, horizon, reverb, and stripe-listen unit

```bash
servlo autostart enable      # boot servlo on every login
servlo autostart disable     # stop booting on login
```

`servlo autostart enable` runs `systemctl --user enable` on the full set; `servlo autostart disable` runs the matching `disable`. The dashboard's enabled state is the canonical "is autostart on" indicator.

---

## From the Web UI

The dashboard at `http://127.0.0.1:7073` has **Start** and **Stop** buttons in the header:

- **Start** appears only when one or more core services (DNS, nginx, PHP-FPM) are not running. Clicking it calls `servlo start` via the API.
- **Stop** is always visible while servlo is running. Clicking it calls `servlo stop`.

These map one-to-one to the CLI commands above, no special UI-only behaviour.

---

## Status & verification

```bash
servlo status
```

Shows a live snapshot: DNS reachability, nginx, PHP-FPM containers, watcher, host tools, services, certificate expiry, and LAN exposure. Run it after every `servlo start` to confirm everything is healthy. See [Troubleshooting](../troubleshooting.md) if anything is reported as down.

The `[Tools]` section lists the host binaries servlo manages (Composer, fnm, mkcert) with their installed versions, and flags any that differ from the versions servlo currently pins. The same information appears in the web UI under System > Tools, where the pending version on a tool's card is a button that updates that one tool. Apply pending updates from the terminal with:

```bash
servlo tools:update
```

The pins are read from a manifest that is cached for a day, so a newly published pin can take that long to show up on its own. "Check for updates" on the Tools page re-reads it immediately, and a tool that falls behind also raises an `update_available` notification.

Tools that are already at their pinned version are left untouched, and tools that are deliberately absent (fnm on an nvm-managed setup) are skipped. A tool whose version shows as unknown, for example Composer after a `composer self-update`, is re-downloaded at the pinned version.

Each tool is an independent download, so one that cannot be updated does not stop the others. The run carries on and closes with a count of what failed.

### Where the pins come from

The pinned versions live in `internal/tools/tools.yaml`. A copy is embedded in the binary as the offline fallback, and the published one is fetched before a download so a bad pin can be fixed without a release.

Because that file reaches every install without going through a release, a published pin is only honoured when its URL points at a host servlo downloads tools from: `getcomposer.org`, `github.com`, and GitHub's asset hosts. Anything else falls back to the embedded pin. Set `SERVLO_TOOLS_HOSTS` to a comma-separated list to allow additional hosts, and `SERVLO_TOOLS_URL` to fetch the manifest from somewhere other than GitHub.

A pin may also carry a `digests` map alongside `assets`, giving the sha256 of each platform's asset:

```yaml
tools:
  mkcert:
    version: v1.4.4
    url: https://github.com/FiloSottile/mkcert/releases/download/{version}/{asset}
    assets:
      linux/amd64: mkcert-{version}-linux-amd64
    digests:
      linux/amd64: 6d31c65b03972c6dc4a14ab429f2928300518b26503f58723e532d1b0a3bbb52
```

Where a digest is given the download is checked against it and rejected on a mismatch. The field is optional, so a manifest without it installs exactly as before, and binaries that predate the field ignore it. Either way a download is written to a temporary file and moved into place only once it is complete, so a failed or rejected download never replaces a working binary.

---

## Cheat sheet

| Situation | Command |
|---|---|
| Just installed servlo | Nothing, `servlo install` already started everything |
| Coming back to your laptop after `servlo stop` | `servlo start` |
| Reboot, autostart disabled | `servlo start` |
| Reboot, autostart enabled | Nothing, happens automatically |
| Free up CPU / RAM during a heavy build | `servlo stop` |
| Full shutdown before a reinstall | `servlo quit` |
| Verify everything's healthy | `servlo status` |
| Update Composer and fnm to their pinned versions | `servlo tools:update` |
| Uninstall a service entirely (data preserved) | `servlo service remove <name>` |
| Uninstall and wipe data | `servlo service remove <name> --purge` |
| Reinstall a service in place | `servlo service reinstall <name>` |
| Reinstall with fresh data + reprovision linked sites | `servlo service reinstall <name> --reset-data` |
