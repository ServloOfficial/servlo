# Architecture

All containers join the rootless Podman network `servlo`. Communication between Nginx and PHP-FPM uses container names as hostnames.

::: info Platform requirement
On Linux, servlo requires systemd. Every container runs as a Podman Quadlet (systemd unit), every worker as a systemd user service, and the autostart flow uses systemd user linger. Non-systemd distros (OpenRC, runit, s6, sysvinit) are not supported.
:::

## Request flow

```
                        DNS you control
                              │
  Browser ──── port 80/443 ──▶ servlo-nginx
                              (nginx:alpine)
                                  │
                      fastcgi_pass :9000
                                  │
                                  ▼
                            servlo-php84-fpm
                          (locally built image)
                                  │
                              reads (bind mount)
                                  │
                                  ▼
                       ~/sites/myapp.example.com (or any path)
```

## Components

| Component | Technology |
|---|---|
| CLI | Go + Cobra, single static binary |
| Web server | Podman Quadlet (`nginx:alpine`) |
| PHP-FPM | Podman Quadlet per version (locally built image with all Laravel extensions) |
| PHP CLI | `php` binary inside the FPM container (`podman exec`) |
| Composer | `composer.phar` via bundled PHP CLI |
| Node | [fnm](https://github.com/Schniz/fnm) binary, version per project |
| Services | Podman Quadlet containers |
| TLS | Let's Encrypt over HTTP-01, behind a certificate issuer interface |
| Notifications | Single in-process notifier inside `servlo-ui` dispatches every kind (mail, worker failures, finished service ops, service updates) through both WebSocket (open tabs at `/api/ws`) and Web Push (closed PWA / minimised tabs). Per-install VAPID keys at `~/.local/share/servlo/vapid-{private,public}.key`; subscription store at `push-subscriptions.json` with per-category preferences. See features/notifications.md. |

## Key design decisions

**Rootless Podman**: all containers run without root privileges, and servlo keeps no root process at all. Where a step genuinely needs privilege, servlo prints the exact command and leaves running it to you.

### Port binding

Rootless podman cannot publish a privileged port, so nginx reaches 80 and 443 one of two ways. Which one applies is decided at install and recorded under `ports.strategy` in `~/.config/servlo/config.yaml`, together with the nginx ports it implies, so `servlo doctor` checks the same thing later instead of guessing.

**`sysctl`**, the default. `net.ipv4.ip_unprivileged_port_start` is lowered to 80, and nginx publishes 80 and 443 directly. 80 rather than 0: it is the lowest value that admits both ports while leaving ssh, smtp and the rest of the low range privileged.

**`nftables`**, the fallback for a kernel that does not expose that sysctl. nginx publishes 8080 and 8443, and nftables redirects 80 and 443 into them. The rules live in `/etc/nftables.d/servlo-ports.conf`, included from `/etc/nftables.conf` so `nftables.service` restores them at boot. Both a prerouting and an output chain are needed: traffic the server originates to its own site never passes prerouting, so without the output chain a local request to port 80 reaches nothing.

Servlo never applies either one itself. The install prints the commands, and a server that has not had them run yet installs fine and simply does not serve on 80 until it does.

`servlo doctor` re-checks the recorded strategy on every run, in two halves that fail independently:

- **Live**: can nginx bind its ports right now.
- **Persistent**: will it still be able to after a reboot.

The dangerous combination is live but not persistent. A `sudo sysctl -w` with no drop-in written, or nftables rules loaded by hand with no include in `/etc/nftables.conf`, serves perfectly until the machine restarts and then silently stops. Doctor fails on that while the server is still up, and prints the command that pins it. Both halves are checked with things an unprivileged user can read, since doctor runs as the operator rather than as root.

**Dual-stack networking**: the servlo podman bridge is created with both an IPv4 and an IPv6 ULA subnet (`fd00:1e7d::/64`) when the host has a usable IPv6 address (anything outside `::1` and `fe80::/10`). On hosts that advertise IPv6 in the kernel but have no routable v6 on any interface, typical in headless QEMU/KVM VMs, containers, and networks without v6 DHCP, netavark can't reliably hold the ULA gateway on the rootless bridge, so aardvark-dns fails to bind `[fd00:1e7d::1]:53` and service containers exit on start. Servlo detects this by reading `/proc/net/if_inet6` and `/proc/sys/net/ipv6/conf/all/disable_ipv6`, and when no usable v6 is present the `servlo` network is created v4-only instead. Existing networks whose schema doesn't match the current host (dual-stack on a v6-less host, or v4-only on a host that now has v6) are recreated in place on the next `servlo install`: attached containers stop, the network is recreated with the right schema, the previous `network_dns_servers` list is restored, and the containers restart. When dual-stack is in use, nginx vhosts listen on `0.0.0.0` and `[::]`, and every managed `PublishPort` in service quadlets is paired (a `127.0.0.1:5432` bind also gets a `[::1]:5432`). To opt out, set an explicit subnet via `podman network create` before `servlo install` runs, or override the `servlo-*` quadlets to remove the `[::]` / `[::1]` lines before they're written.

Binding symmetry is preserved across stacks: `127.0.0.1` maps to `[::1]` and `0.0.0.0` maps to `[::]`, so a loopback-only service on v4 stays loopback-only on v6. Services bound through pasta (quadlets without a `Network=` line) remain v4-only because pasta can't bind v6 ports in the current version. One pitfall to be aware of: host firewall rules that only filter IPv4 (iptables without matching ip6tables, or a firewall UI that only surfaces v4) leave v6 ports reachable even when the equivalent v4 rule blocks them. This is covered in more detail under Security caveats.

**Podman Quadlets**: containers are defined as systemd unit files (`.container` files) managed by the Quadlet generator. This means `systemctl --user start servlo-nginx` works like any other systemd service, and containers restart on failure and at login.

**Shared nginx**: a single nginx container serves all sites via virtual hosts. nginx uses a Podman-network-aware resolver to route `fastcgi_pass` to the correct PHP-FPM container by hostname.

**Shared hosts files**: containers resolve the domains of sites on this server through two generated files that servlo bind-mounts as `/etc/hosts`. Every domain in the site registry is written pointing at servlo-nginx, so a container reaching a site on this server goes over the Podman bridge rather than back out to the public address. `~/.local/share/servlo/hosts` goes into every PHP-FPM container so server-side HTTP from PHP reaches local sites, and it also carries the `host.containers.internal` entry that host-database overrides rely on. `~/.local/share/servlo/browser-hosts` goes into services declaring `share_hosts: true`, today Selenium, so Dusk and Pest Browser can reach a site. Every other service quadlet mounts the first file too, for a second reason: without an explicit mount Podman derives the container's `/etc/hosts` from `base_hosts_file`, which defaults to the host's own, so anything you have added there for your own use leaks in and can shadow a container name that Podman's DNS would otherwise resolve. Mounting a servlo-managed file cuts that inheritance. Both files pin servlo-nginx's address on the Podman bridge, and Podman assigns a fresh one every time the container is recreated, so a reboot that brings the quadlet units up without `servlo start` would otherwise leave every container resolving those domains to a dead address. A background watcher inside `servlo-ui` inspects the nginx container once per 30-second tick and repoints both files when the address has moved. The same loop keeps `host.containers.internal` on a routable address across network changes, reprobing only when the host's primary address changes, since that probe is a container exec and costs far more than the inspect.

**Outbound DNS from containers**: rootless Podman puts containers in their own network namespace, where the host's nameservers are not reachable at their own addresses. Pasta bridges that gap and records the forwarder it picked in `/run/user/<uid>/containers/networks/rootless-netns/info.json`; servlo reads that record and hands the addresses to the `servlo` network as its upstream, falling back to pasta's usual `169.254.1.1` when the file is missing or every address in it is unusable. Loopback, unspecified and zoned link-local addresses (`fe80::1%18`) are filtered out, because netavark cannot consume a scoped address and an interface-bound one means nothing inside the namespace. The list is never allowed to end up empty: a network created with no upstream leaves containers unable to resolve anything at all.

Servlo neither reads nor writes the host's own resolver configuration. It does not run a resolver, it does not own a TLD, and nothing about a domain resolving on the public internet is servlo's to arrange: whatever this machine uses to resolve names is left exactly as you configured it, and no file of yours is edited to make a site reachable. Making a domain point at this server is DNS you hold at your registrar; servlo only checks the answer before it asks Let's Encrypt for a certificate.

**Per-version PHP-FPM**: each PHP version gets its own container built from a local `Containerfile`. The image includes all extensions needed for Laravel out of the box: `pdo_mysql`, `pdo_pgsql`, `bcmath`, `mbstring`, `xml`, `zip`, `gd`, `intl`, `opcache`, `pcntl`, `exif`, `sockets`, `redis`, `imagick`.

**Automatic volume mounts**: the PHP-FPM and nginx containers bind-mount `$HOME` by default. When a project lives outside the home directory (e.g. `/var/www`, `/opt/projects`), servlo automatically adds the extra volume mount to both containers and restarts them. This happens transparently during `servlo link`, `servlo park`, or the first `servlo php` / `composer` / `laravel new` invocation from the outside path.

Ephemeral system trees (`/tmp`, `/var/tmp`, `/run`, `/proc`, `/sys`, `/dev`) are deliberately excluded from auto-mounting: they vanish on reboot and would leave containers with dead mounts, and IDEs dropping randomly named temp files there would otherwise cascade container restarts. Running `servlo php` from such a path is refused with a clear message rather than an opaque runtime error. To opt a specific scratch root in anyway (common for AI coding agents whose session files live under `/tmp`), list it under `mounts:` in `~/.config/servlo/config.yaml`:

```yaml
mounts:
  - /tmp/claude
```

Each entry is bind-mounted at the same location in the PHP-FPM and nginx containers, on the next `servlo start` or on the first `servlo php` invocation from a path it covers. Paths are mounted verbatim (host path equals container path), so a script's working directory and file arguments resolve unchanged.

**Idle cost of the daemons**: `servlo-ui` and the watcher stay resident for the whole session, so what they cost while nothing is happening is a design constraint rather than an afterthought. The thing to watch is not CPU percentage, which is tiny, but how often a tick blocks: a periodic job that forks a subprocess or makes a round trip per unit costs orders of magnitude more than one that reads a cached snapshot, and on a laptop that is what keeps the machine from settling. So anything on a timer either answers from a batched cache, or backs off to a slow cadence and snaps back when something actually moves. Three rules keep it down. State files that a tick re-persists, the request-timing snapshot and the idle activity map, are written only when their content actually changed, so a quiet machine does no disk writes on those ticks instead of rewriting identical bytes every ten and thirty seconds; that matters more on a laptop than the CPU does, because it is what keeps the disk and the writeback path from settling. The daemons also cap `GOMAXPROCS` at 4 instead of sizing the Go scheduler to the host's core count, since they are event handlers waiting on sockets, timers and subprocesses. On a many-core machine the default gives each daemon dozens of threads and turns every wakeup into futex traffic across all of them. Goroutines blocked in syscalls still get threads of their own, so subprocess and file concurrency is unaffected, and an explicit `GOMAXPROCS` in the environment overrides the cap.

Third, install markers are stamped after the install that wrote them succeeds. Composer and npm only rewrite `vendor/composer/installed.json` and the `node_modules` lockfile snapshot when the package set actually changes, so an install against an already-correct tree leaves the marker older than the lockfile and the staleness check keeps asking for another one. A checkout seeded from another one lands in that state, carrying the source's older timestamps, and the result was the watcher re-running `composer install` on it once a minute for as long as the daemon lived.
