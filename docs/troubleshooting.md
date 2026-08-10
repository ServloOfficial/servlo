# Troubleshooting

When something isn't working, start with the built-in diagnostics:

```bash
servlo doctor   # full check: podman, systemd, DNS, ports, images, config
servlo status   # quick health snapshot of all running services
```

`servlo doctor` reports OK/FAIL/WARN for each check with a hint for every failure.

## Repairing findings automatically

`servlo doctor --fix` runs the same diagnostic and then offers to repair the findings it safely can. It confirms each fix before applying it, so you can pick and choose:

```bash
servlo doctor --fix             # confirm each repair
servlo doctor --fix --yes       # apply without prompting (heavy fixes still confirm)
servlo doctor --fix --dry-run   # list what would be repaired, change nothing
```

The fixes fall into three groups. servlo applies the safe ones itself, creating a missing data or config directory, enabling linger so services survive logout, installing the network-online drop-in, rebuilding a missing PHP image, and, after you confirm the heavier ones, reinstalling the services or reclaiming podman disk. Anything that needs `sudo` servlo never runs for you; it prints the exact command to copy. That covers installing podman, crun, fuse-overlayfs, the rootless network helpers, adding a subuid range, and the port-strategy commands, which need `sudo` and so are yours to run even though servlo knows them. Findings that are external state, a foreign process already holding port 80, a config file with a syntax error, are left untouched with their hint.

Reclaimable disk is listed separately as optional, because nothing is wrong when there is disk to reclaim. It runs the same interactive reclaim as `servlo cleanup`, so it takes the deep scope and can remove an unreferenced catalog image whoever pulled it, and the size doctor quotes is that same deep scope. If you run other podman workloads on the machine, run [`servlo cleanup --safe`](usage/cleanup.md) yourself instead. Optional fixes never count towards what a re-check reports as still outstanding.

## Filing a bug report

If you need help on the [issue tracker](https://github.com/realrashid/servlo/issues), run:

```bash
servlo bug-report
```

This writes a single plain-text file (default: `./servlo-bug-report-<timestamp>.txt`) containing the full `servlo doctor` output, your `config.yaml` and `sites.yaml`, the state of every `servlo-*` systemd unit, recent journal and container logs for servlo's own infra units, listening sockets on the servlo ports, and a curated set of environment variables.

What gets filtered before it lands on disk:

- Site `.env` files are excluded outright.
- Home paths render as `$HOME` and the username as `$USER`.
- Site names, domains and parked-directory paths are replaced with `site-1`/`site1.<tld>`/`$PARK_1` placeholders. Pass `--show-real-names` to keep the raw values for local debugging.
- Logs are kept only for servlo's own infra (`servlo-nginx`, `servlo-ui`, `servlo-dns`, `servlo-watcher`, etc.). Preset services (mysql, redis, meilisearch, gotenberg, …), FPM containers and per-site workers still appear in the unit-state and container tables but their logs are dropped, they were producing repetitive request-shaped noise that didn't help triage.
- Custom services and per-site custom / FrankenPHP containers are omitted entirely so the report doesn't expose user app identifiers.
- Nginx structured error lines have their `request:` / `upstream:` / `referrer:` URI fields redacted, and HTTP access lines are dropped.

Skim the file before posting (it's plain text, open it in any editor) and attach it to your GitHub issue.

Override the destination with `--output`, change how many log lines per service to include with `--log-lines`, or keep raw site names with `--show-real-names`:

```bash
servlo bug-report --output /tmp/report.txt --log-lines 500
servlo bug-report --show-real-names
```

## Profiling servlo-ui

If servlo-ui itself is using more CPU or memory than it should, you can capture a Go profile from the running process and attach it to an issue. Profiling is off until you turn it on, and servlo-ui only serves it to the local machine.

Create the marker, capture, then remove it:

```bash
touch ~/.local/share/servlo/run/pprof.enabled
curl -o /tmp/servlo-cpu.prof 'http://127.0.0.1:7073/debug/pprof/profile?seconds=30'
rm ~/.local/share/servlo/run/pprof.enabled
```

The marker is read on every request, so nothing needs restarting. That matters when you are chasing a daemon that is busy right now, because restarting it would throw away the state worth capturing. Drive the site or wait for the symptom while the thirty seconds are running, otherwise you profile an idle process.

Other useful captures, once the marker is in place:

```bash
curl -o /tmp/servlo-heap.prof http://127.0.0.1:7073/debug/pprof/heap
curl 'http://127.0.0.1:7073/debug/pprof/goroutine?debug=1' | head -50
```

Read a profile by passing the binary alongside it, or just attach the file to the issue:

```bash
go tool pprof -top ~/.local/bin/servlo /tmp/servlo-cpu.prof
```

Release binaries are stripped, so the plain-text views (`?debug=1`) print raw addresses rather than function names. Pass the binary as above and `go tool pprof` resolves them anyway, so a profile captured from an ordinary install is still readable.

Remove the marker when you are done. While it exists, anyone who can reach the machine's loopback interface can read goroutine stacks, the process command line, and heap contents, and can start a profile that occupies a core for as long as they ask for.

---

::: details "Secure Connection Failed" after the host wakes from suspend or hibernate
After a long suspend or hibernate, rootless podman networking can come back in a bad state: the servlo-nginx container loses its host port forward (or stops), so nothing listens on 443 and the browser shows a generic "Secure Connection Failed" for your sites.

On Linux the watcher now restarts nginx automatically. It notices the host has resumed from a real wall-clock gap in its tick loop (the timer is frozen while the machine is suspended), and on that one tick it checks whether servlo-nginx is accepting on its HTTPS port and restarts it if the listener died. Keying off the resume event rather than a continuous poll means it acts exactly once and can never fight a `servlo start` you ran yourself, since a start does not suspend the machine.

One case it still leaves for `servlo start` rather than acting from a background timer: a host whose IPv6 support changed across the wake, since the servlo network must be recreated and that rebuilds every container. The same applies in the rare case the watcher itself was not running at the moment of resume.
:::

::: details Nginx not serving a site
Check that nginx and the PHP-FPM container are running, then inspect the generated vhost:

```bash
servlo status                         # check nginx and FPM are running
podman logs servlo-nginx              # nginx error log
cat ~/.local/share/servlo/nginx/conf.d/my-app.test.conf   # check generated vhost
```
:::

::: details My custom nginx directive disappeared after an update
Don't edit `~/.local/share/servlo/nginx/conf.d/*.conf` directly. Servlo regenerates those files on `servlo link`, `servlo secure`, `servlo site rebuild`, and every `servlo install` (which `servlo update` re-execs). Drop your snippet in `~/.local/share/servlo/nginx/custom.d/{domain}.conf` instead, the generated vhost ends with an `include` for that file, and servlo never writes into `custom.d/`. See [Nginx Overrides](./usage/nginx-overrides.md) for examples.
:::

::: details PHP-FPM container not running
Check the systemd unit status and logs:

```bash
systemctl --user status servlo-php84-fpm
systemctl --user start servlo-php84-fpm
podman logs servlo-php84-fpm
```

If the image is missing (e.g. after `podman rmi`):

```bash
servlo php:rebuild
```
:::

::: details `podman exec` fails with "chdir: No such file or directory"
This happens when your project is outside your home directory (e.g. `/var/www/`, `/opt/projects/`). The PHP-FPM and nginx containers only mount `$HOME` by default.

Servlo handles this automatically: when you `servlo link`, `servlo park`, or run any exec command (`servlo php`, `composer`, `laravel new`) from an outside path, servlo adds the volume mount and restarts the affected containers.

If you see this error on an older servlo version, update to the latest and re-link the site:

```bash
servlo update
servlo unlink && servlo link
```

To verify the mounts are in place:

```bash
grep Volume ~/.config/containers/systemd/servlo-nginx.container
grep Volume ~/.config/containers/systemd/servlo-php*-fpm.container
```

You should see your project path listed alongside the `%h:%h` mount. The quadlet is only half the answer though, a container keeps the mounts it booted with, so check the running one too:

```bash
podman inspect servlo-php84-fpm --format '{{range .Mounts}}{{.Source}}
{{end}}'
```

If the path is in the quadlet but not in that output, `servlo restart` picks it up.
:::

::: details nginx fails to start with "statfs /path: no such file or directory"
Podman refuses to start a container whose bind-mount source is gone, so a directory outside `$HOME` that servlo mounted while it existed, and that has since disappeared, stops the container dead. A Git branch checkout that removes a project subdirectory is the usual cause, and because nginx serves every site, one missing directory takes the whole stack down.

`servlo start` repairs this for you: it drops the stale mounts before starting anything and tells you which path and site was responsible.

```
WARN: /var/www/erp/Modules/Accounts no longer exists (site erp), removed from servlo-nginx
```

The mount comes back on its own once the directory is there again and you run any command from it, or after `servlo restart`.
:::

::: details Permission denied on port 80/443
Rootless Podman cannot bind to ports below 1024 by default. `servlo install` prints the commands that fix it and records which strategy applies, but it never runs them for you, so this is what you see if they were never run or a kernel update reset the sysctl.

```bash
sudo sysctl -w net.ipv4.ip_unprivileged_port_start=80
sudo sh -c 'echo net.ipv4.ip_unprivileged_port_start=80 > /etc/sysctl.d/99-servlo-ports.conf'
```

Both lines matter: the first takes effect now and is lost at reboot, the second survives a reboot and does nothing until one.

`servlo doctor` re-checks this on every run. If your kernel does not expose `ip_unprivileged_port_start` at all, servlo picks the nftables strategy instead and prints a different set of commands; see [Port binding](/reference/architecture#port-binding).
:::

::: details Watcher service not running
The watcher monitors parked directories, site config files, and DNS health. If sites aren't being auto-registered or queue workers aren't restarting on `.env` changes:

```bash
servlo status                            # shows watcher running/stopped
systemctl --user start servlo-watcher   # start it from the terminal
# or use the Start button in the UI under System > Watcher
```

The watcher reports itself ready as soon as its watch loops are live and does its boot reconciliation (registering parked projects) after that, in the background, so a slow reconcile can't hold the unit below its start timeout and put systemd in a restart loop.

To see what the watcher is doing:

```bash
journalctl --user -u servlo-watcher -f
# or open the live log stream in the UI under System > Watcher
```

For verbose output (DEBUG level), set `SERVLO_DEBUG=1` in the service environment:

```bash
systemctl --user edit servlo-watcher
# Add:
# [Service]
# Environment=SERVLO_DEBUG=1
systemctl --user restart servlo-watcher
```
:::

::: details `servlo secure` fails with an authorization error
Let's Encrypt validates over HTTP-01: it connects to your domain on port 80 and fetches a token Servlo just published. An authorization error means that fetch did not return what it should have. In rough order of likelihood:

- **The domain does not resolve to this server.** Check with `dig +short example.com` and compare against the droplet's public address. A record you added minutes ago may not have propagated yet.
- **Port 80 is not reachable from the internet.** A cloud firewall or security group in front of the droplet will not show up in `servlo doctor`, which can only see the host.
- **Something else answers first.** Another web server bound to 80, or a CDN or proxy in front of the domain that serves its own content for `/.well-known/`.
- **nginx is not running.** `servlo status` shows it; the challenge is served by the same nginx that serves the sites.

Work the problem against staging rather than production. Production locks you out of retrying a domain after five failures, and a DNS record that has not propagated will burn all five:

```bash
servlo secure --staging
```

Switch back with `servlo secure --staging=false` once it issues.
:::

::: details PHP image build is slow on first run
servlo pulls a pre-built base image from ghcr.io when one is published for your PHP version and finishes in ~30 seconds. Otherwise it builds from the official `php:<version>-fpm-alpine` and compiles the extensions, which is slower but produces the same image; the build prints which of the two it is doing and why.

If a pull that should have worked falls back anyway, the most common cause is being logged into ghcr.io with expired or unrelated credentials; the registry rejects the authenticated request even though the image is public.

servlo handles this automatically since v1.3.4 by always pulling anonymously. If you are on an older version, running `podman logout ghcr.io` before the build will fix it.
:::

::: details Nginx fails to start (missing certificates)
`servlo start` automatically detects SSL vhosts that reference missing certificate files and repairs them before starting nginx:

- **Registered sites**: the site is switched back to HTTP and the vhost is regenerated. The registry is updated (`Secured = false`).
- **Orphan SSL vhosts**: configs left behind by unlinked sites with missing certs are removed.

Repaired items are printed as warnings during startup:

```
  WARN: missing TLS certificate for myapp.example.com, switched to HTTP
```

To re-enable HTTPS after the automatic repair, run `servlo secure <name>`.

If nginx still fails to start, check the logs:

```bash
journalctl --user -u servlo-nginx -n 30 --no-pager
```
:::

::: details Port conflicts on `servlo start`
`servlo start` checks for port conflicts before starting containers. If another process is already using a required port, you'll see a warning:

```
Port conflicts detected:
  WARN: port 80 (nginx HTTP) already in use, may fail to start (check: ss -tlnp sport = :80)
```

Common culprits are Apache, another nginx instance, or a previously running servlo that wasn't stopped cleanly. Find and stop the conflicting process:

```bash
# Linux
ss -tlnp sport = :80

# macOS
lsof -nP -iTCP:80 -sTCP:LISTEN
```

The exact command servlo suggests in `servlo doctor` and `servlo start` output is already platform-correct, so you can copy it from there.

`servlo doctor` also checks for port conflicts as part of its full diagnostic, and adds a dedicated **[Stopped service ports]** section that flags installed services whose host port is already bound by another process. The same warning is shown next to the inactive status pill in the web UI, so you can spot the conflict without running anything: most often this is a system-installed service (Postgres, MySQL, Redis) listening on the default port. Stop the conflicting process and the warning clears on the next snapshot refresh.
:::

::: details Workers missing after reinstall
If you ran `servlo uninstall` and then reinstalled, worker units and service quadlets are deleted during uninstall. Running `servlo start` after reinstalling automatically restores them from the `workers` list saved in each site's `.servlo.yaml`. If `.servlo.yaml` does not exist or was not committed, you will need to start workers again manually (`servlo queue:start`, etc.).

To check what was restored:
```bash
servlo status   # shows all active workers and services
```
:::

::: details Workers failing or crash-looping
Check `servlo status`, the Workers section lists all active, restarting, or failed workers. In the web UI, failing workers show a pulsing red toggle and a **!** on their log tab.

To inspect the error:

```bash
journalctl --user -u servlo-queue-my-app -f    # or servlo-horizon-my-app, servlo-schedule-my-app
```

Common causes:
- Missing Redis when `QUEUE_CONNECTION=redis`, start it with `servlo service start redis`
- Missing dependencies after a fresh clone, run `servlo setup` to install them
- Bad `.env` values, run `servlo env` to reset service connection settings

When you unlink a site, crash-looping workers are automatically detected and stopped.
:::

::: details Error: NetworkUpdate is not supported for backend CNI: invalid argument
Your system is likely configured to use the older CNI backend, which lacks support for the requested network operation. Edit or create the Podman configuration file at `/etc/containers/containers.conf` and add or modify the `network_backend` setting to `netavark`:

```toml
[network]
network_backend = "netavark"
```

To ensure a clean switch and recreate the networks with the new backend, reset the Podman storage. **Warning**: this will wipe all existing containers, pods, and networks:

```bash
podman system reset
```
:::

::: details Error: unknown flag: --dns (during `servlo install`)
Symptom: `servlo install` aborts at the `podman network create` step with `Error: unknown flag: --dns`.

Cause: your podman is older than 4.5. The `--dns` flag on `podman network create` was added in podman 4.5 (April 2023), and servlo needs it to write upstream DNS servers into netavark's per-network JSON atomically (otherwise the post-create `network update --dns-add` path crashes on Ubuntu 24.04's netavark <1.11). Distributions that ship podman older than 4.5: Ubuntu 22.04 / Zorin 17 (3.4.4), Debian 12 (4.3.1), Debian 11 (3.0.1).

Fix: upgrade podman to 4.5 or newer. On Ubuntu 22.04 and Zorin 17 the main archive doesn't ship a new enough podman, but the [Kubic libcontainers OBS repo](https://podman.io/docs/installation#ubuntu-2204-2104-2010-2004) does (it's the path podman's own docs recommend). On Debian 12 enable bookworm-backports and run `sudo apt install -t bookworm-backports podman`. See the [requirements page](getting-started/requirements.md#podman-4-5-minimum) for the full distro/version table.
:::

::: details Error: unable to parse ip fe80::...%18 specified in AddDNSServer: invalid argument
Your host's DNS configuration includes a zoned link-local IPv6 nameserver, typically advertised by your router via SLAAC + RDNSS. The zone identifier (`%18` is a kernel interface index) is meaningless inside a container's network namespace, and netavark refuses to accept it.

Servlo 1.18+ filters these addresses automatically before handing them to podman. If you're still on 1.17 or older, upgrade with `servlo update` and rerun `servlo install`. The filter is conservative: only zoned link-local (`fe80::...%iface`) addresses are dropped; globally routable IPv6 nameservers (e.g. `2606:4700:4700::1111`) are preserved.

When filtering empties the entire DNS list, servlo falls back to pasta's standard forwarder (`169.254.1.1`), which bridges into the host's resolver and preserves `.test` routing.
:::

::: details Services fail to start with "aardvark-dns failed to bind [fd00:1e7d::1]:53"

Symptom: after `servlo install`, a subset of service containers (commonly `servlo-nginx`, `servlo-postgres`, `servlo-meilisearch`) fail to start. Journal shows:

```
Error: netavark: error while applying dns entries: IO error: aardvark-dns failed to start
Error starting server failed to bind udp listener on [fd00:1e7d::1]:53:
IO error: Cannot assign requested address (os error 99)
```

Cause: the host advertises IPv6 in the kernel but has no routable v6 address on any interface, only `::1` and `fe80::`, so netavark can't hold the ULA gateway on the rootless bridge, and aardvark-dns bind fails with `EADDRNOTAVAIL`. Typical in headless QEMU/KVM VMs and networks without v6 DHCP.

Servlo 1.18+ detects this on every `servlo install` by reading `/proc/net/if_inet6` (any non-loopback, non-link-local v6 address counts as usable) and falls back to a v4-only `servlo` network. An existing dual-stack network on a v6-less host is recreated as v4-only automatically. Force it:

```bash
servlo install
# look for: "Recreated servlo network as v4-only (host has no usable IPv6)."
```

If the host later gains v6 connectivity, the next `servlo install` will recreate the network as dual-stack again.

If you'd rather skip the dual-stack code path entirely, even on a v6-capable host, opt out:

```bash
servlo install --no-ipv6
# or persistently via shell rc:
export SERVLO_DISABLE_IPV6=1
```

Either path writes `~/.local/share/servlo/ipv6-probe-failed-servlo`, which `EnsureNetwork` honors on every code path (initial create, migration, recreate). To re-enable dual-stack, delete that marker file and re-run `servlo install`.
:::

::: details Every DNS lookup inside a servlo container stalls ~5 seconds
Symptom: pages that hit the database or any container-to-container hostname feel slow, and `time dig <anything> @<container>` takes roughly five seconds before returning an answer. The network looks fine in `podman network inspect servlo` (both IPv4 and IPv6 subnets present), but aardvark-dns's on-disk config has the v6 gateway absent from its listen-ips line.

Cause: `podman network rm` doesn't clean up `$XDG_RUNTIME_DIR/containers/networks/aardvark-dns/<name>` between rm and recreate, so a network that was originally v4-only can leave aardvark with a v4-only listen header even after the network is recreated dual-stack. The container's generated resolver file still lists the v6 gateway as the primary nameserver, queries to it time out (~5s), then glibc falls back to the v4 gateway.

Servlo 1.18+ detects this drift on `servlo install` (aardvark listen line is v4-only despite the network being dual-stack) and self-heals by recreating the network with the stale aardvark state wiped. If you're on an earlier 1.18 build or the heal didn't fire, force it:

```bash
servlo install
```

Manual verification:

```bash
cat "${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/containers/networks/aardvark-dns/servlo" | head -1
# expect both gateways, e.g.: fd00:1e7d::1,10.89.7.1 169.254.1.1
# if only 10.89.7.1 is present, the drift fix didn't run — re-run servlo install
```
:::

::: details Every container takes 90 seconds to start (Fedora Silverblue and other atomic images)
Symptom: `servlo start` sits there for a minute and a half per container, and after a reboot nothing is serving until well over a minute in. `systemctl --user list-units --state=failed` shows `podman-user-wait-network-online.service` failed with a timeout, and `servlo doctor` reports the `podman network-online wait` check as a warning.

Cause: podman's quadlet generator makes every rootless container `Wants=` and `After=podman-user-wait-network-online.service`, a unit that polls the system's `network-online.target` until it gives up after 90 seconds. That target is only reached when some unit pulls it in, and on atomic images (Silverblue, Kinoite, Bazzite, CoreOS) nothing does, so the wait can never succeed. Every container start, and the boot itself, pays the full timeout.

Servlo detects this on `servlo start` and `servlo install` and writes a drop-in that turns the wait into a no-op, since servlo publishes on loopback and needs no routable network:

```bash
servlo start   # writes ~/.config/systemd/user/podman-user-wait-network-online.service.d/10-servlo-no-network-wait.conf
```

The override only lands on hosts where `network-online.target` is genuinely inactive; on an ordinary distro the wait is left alone. To confirm the target is the one at fault:

```bash
systemctl is-active network-online.target   # "inactive" here means every quadlet start pays 90s
```

To go back to podman's stock behaviour, delete the drop-in and run `systemctl --user daemon-reload`.
:::

::: details Podman Machine overlay-storage error (macOS)
Symptom: on macOS, `servlo start` fails and **every** container start reports a graph-driver / overlay error:

```
exit status 125: Error: getting graph driver info "<id>":
readlink /var/lib/containers/storage/overlay: invalid argument
```

Cause: the macOS host was shut down ungracefully (forced power-off, battery death, kernel panic) while the Podman Machine VM was still running. The VM's container storage is left with a stale overlay mount and corrupt container layers, so no container can start until the storage is remounted and the stale containers are rebuilt.

`servlo start` detects this and **self-heals automatically** on the first run: it restarts the Podman Machine to remount the storage, force-removes the stale `servlo-*` containers so they rebuild on fresh storage, and retries the start pass once. Your data is safe throughout: servlo bind-mounts every database and site directory to the host, not into the VM.

If the automatic recovery isn't enough (it prints guidance pointing here), recreate the VM:

```bash
servlo machine reset
```

This stops the VM, removes it, and re-initialises it. Databases and site data are preserved (they live on the host); container images are rebuilt automatically on the next `servlo start`. See [Start, Stop & Autostart → `servlo machine reset`](usage/lifecycle.md#servlo-machine-reset-macos).
:::
