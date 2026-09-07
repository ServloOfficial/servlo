# Start, stop and coming back after a reboot

The lifecycle commands for the whole stack: nginx, PHP-FPM, services, workers,
the dashboard and the watcher.

::: danger Stopping servlo takes every site on this server offline
This is a server other people's sites are served from. `servlo stop` is not a
way to free up memory — it is an outage, for every site at once, until somebody
runs `servlo start`. There is no per-site stop here: to take one site down and
leave the rest serving, use `servlo pause <name>`, which swaps that site's vhost
for a holding page and stops only its workers.
:::

::: tip You should not need `servlo start` at all
`servlo install` starts everything, and autostart keeps it that way across
reboots. Reaching for `servlo start` means something already went wrong — a
`servlo stop` somebody ran, a reboot with autostart turned off, or containers
killed by hand. On a healthy server it is a no-op.
:::

---

## Commands at a glance

| Command | Stops | Starts |
|---|---|---|
| `servlo start` | nothing | nginx, watcher, all PHP-FPM containers in use, services that were running before stop, queue / schedule / reverb / messenger workers, Web UI |
| `servlo stop` | All containers and workers above. Leaves the watcher and Web UI alone. | nothing |
| `servlo quit` | Everything `servlo stop` does, **plus** the Web UI and watcher. | nothing |

Both take the sites down. The difference is what is left running to bring them
back: after `servlo stop` the dashboard and the watcher are still up, so you can
start the server again from a browser. After `servlo quit` nothing is left and
you need a shell on the machine. Use `quit` before a reinstall or a major
update, and prefer `stop` otherwise for exactly that reason.

---

## `servlo start`

```bash
servlo start
```

Walks the install in dependency order:

1. Pre-flight: warns about **port conflicts**. The ports checked are the ones the units it is about to start publish, so nginx's HTTP and HTTPS ports (80 and 443 unless you changed them) plus a port for each installed service. A port already held by the servlo container that owns it is not a conflict.
2. Rebuilds or pulls any missing container images (e.g. after a `podman rmi` or a podman cleanup).
3. Boots core: `servlo-nginx` and `servlo-watcher`.
4. Boots every PHP-FPM container that has at least one site referencing its version. Unused PHP versions stay stopped.
5. Boots all installed services that are **not** marked as manually paused (see [Manually stopped services](services.md#manually-stopped-services) for the pause-state contract).
6. Restores per-site workers (`servlo-queue-*`, `servlo-schedule-*`, `servlo-reverb-*`, `servlo-messenger-*`, custom workers) from the `workers` list saved in each site's `.servlo.yaml`.
7. Starts the Web UI (`servlo-panel.service`, which runs `servlo serve-ui`).

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

Stops everything `servlo start` started **except** the Web UI and the watcher; those keep running so the dashboard stays reachable to bring servlo back up.

A few important details:

- **Manually paused services are remembered.** If you stopped Meilisearch earlier with `servlo service stop meilisearch`, then `servlo stop` + `servlo start` will not bring Meilisearch back. The pause flag survives the cycle.
- **Pinned services start anyway.** A `servlo service pin <name>` overrides auto-stop logic; pinned services are always started by `servlo start` regardless of which sites are active.
- **Worker state is preserved.** Workers running before `servlo stop` are restarted by the next `servlo start`; workers you manually stopped stay stopped.

---

## `servlo quit`

```bash
servlo quit
```

The full off-switch:

1. Runs everything `servlo stop` does.
2. Stops `servlo-panel` (the Web UI).
3. Stops `servlo-watcher`.

After `servlo quit` there are no servlo processes left running. This is the right command before a reinstall, a system reboot, or before pulling a major update.

## Coming back after a reboot

A server reboots — a kernel update, a hypervisor migration, a power event — and
every site has to come back without anybody logging in to make it happen. That
is what autostart is, and **it is on by default**.

The word "login" appears in some of servlo's own help text here and is
misleading. Servlo's units are systemd *user* units, and `servlo install` enables
[linger](/getting-started/requirements#why-linger-matters) precisely so they do
not wait for a login: with linger on, systemd starts them at boot and keeps them
running when no one is connected. Closing your SSH session does not stop the
sites, and nobody has to open one to bring them back.

Autostart is a single switch over every servlo-owned systemd user unit:

- the dashboard (`servlo-panel.service`) and project watcher (`servlo-watcher.service`)
- every container quadlet (`servlo-mysql`, `servlo-nginx`, `servlo-redis`, `servlo-postgres`, `servlo-php*-fpm`, `servlo-meilisearch`, `servlo-minio`, `servlo-rustfs`)
- every per-site worker, queue, schedule, horizon and reverb unit

```bash
servlo autostart enable      # come back automatically after a reboot
servlo autostart disable     # do not
```

`enable` runs `systemctl --user enable` across the full set; `disable` runs the
matching `disable`, strips the `[Install]` section from the container quadlets so
the podman generator stops wiring them into `default.target`, and stops them. The
dashboard's toggle is the same switch.

::: warning Disabling this means the sites do not come back
On a production server the reboot you did not plan is the one this exists for.
With autostart off, a reboot leaves every site down until somebody notices and
runs `servlo start` by hand. There is a CI job whose whole purpose is to reboot a
machine and assert that every site, service and worker returns unaided; turning
this off opts out of that guarantee. Turn it off for a machine you are
deliberately keeping quiet, not for one serving anybody.
:::

---

## From the dashboard

The dashboard — reached at `https://<this-server-ip>:7073`, or at the domain you
attached with `servlo panel domain set`, not at loopback unless you are on the
machine itself — has **Start** and **Stop** buttons in the header:

- **Start** appears only when one or more core services (nginx, PHP-FPM) are not running. Clicking it calls `servlo start` via the API.
- **Stop** is always visible while servlo is running. Clicking it calls `servlo stop`.

These map one-to-one to the CLI commands above, no special UI-only behaviour.

---

## Status & verification

```bash
servlo status
```

Shows a live snapshot: nginx, PHP-FPM containers, watcher, host tools, services, and certificate expiry. Run it after every `servlo start` to confirm everything is healthy. See [Troubleshooting](../troubleshooting.md) if anything is reported as down.

The `[Tools]` section lists the host binaries servlo manages (Composer, fnm) with their installed versions, and flags any that differ from the versions servlo currently pins. The same information appears in the web UI under System > Tools, where the pending version on a tool's card is a button that updates that one tool. Apply pending updates from the terminal with:

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
  fnm:
    version: v1.39.0
    url: https://github.com/Schniz/fnm/releases/download/{version}/{asset}
    assets:
      linux/amd64: fnm-linux.zip
    digests:
      linux/amd64: 6d31c65b03972c6dc4a14ab429f2928300518b26503f58723e532d1b0a3bbb52
```

Where a digest is given the download is checked against it and rejected on a mismatch. The field is optional, so a manifest without it installs exactly as before, and binaries that predate the field ignore it. Either way a download is written to a temporary file and moved into place only once it is complete, so a failed or rejected download never replaces a working binary.

---

## Cheat sheet

| Situation | Command |
|---|---|
| Just installed servlo | Nothing, `servlo install` already started everything |
| Bringing the sites back after somebody ran `servlo stop` | `servlo start` |
| Reboot with autostart disabled, sites are down | `servlo start`, then turn autostart on |
| Reboot with autostart enabled | Nothing, the sites come back on their own |
| Take one site down, leave the rest serving | `servlo pause <name>` |
| Full shutdown before a reinstall | `servlo quit` |
| Verify everything's healthy | `servlo status` |
| Update Composer and fnm to their pinned versions | `servlo tools:update` |
| Uninstall a service entirely (data preserved) | `servlo service remove <name>` |
| Uninstall and wipe data | `servlo service remove <name> --purge` |
| Reinstall a service in place | `servlo service reinstall <name>` |
| Reinstall with fresh data + reprovision linked sites | `servlo service reinstall <name> --reset-data` |
