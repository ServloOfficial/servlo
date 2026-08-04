# Installation

## Linux

::: warning Requires systemd
Servlo runs every container as a Podman Quadlet and every worker as a systemd user service, so a systemd-based distro is required. OpenRC (Gentoo, Artix-openrc, Alpine), runit (Void, Artix-runit), s6, and sysvinit-based distros (Devuan) are not supported.

Tested and known-good: Ubuntu, Fedora, Arch, Debian, Mint, Pop!_OS, openSUSE, CachyOS, Omarchy. Any systemd distro should work.
:::

### One-line installer (recommended)

::: code-group

```bash [curl]
curl -fsSL https://raw.githubusercontent.com/realrashid/servlo/main/install.sh | bash
```

```bash [wget]
wget -qO- https://raw.githubusercontent.com/realrashid/servlo/main/install.sh | bash
```

```bash [From source]
git clone https://github.com/realrashid/servlo
cd servlo
make build
make install            # installs to ~/.local/bin/servlo
make install-installer  # installs servlo-installer to ~/.local/bin/
```

:::

The installer will:

- Check and offer to install missing prerequisites (Podman, NetworkManager, unzip)
- Download the latest `servlo` binary for your architecture (amd64 / arm64)
- Install it to `~/.local/bin/servlo`
- Add `~/.local/bin` to your shell's `PATH` (bash, zsh, or fish)
- Automatically run `servlo install` to complete environment setup

::: info Setup asks for sudo once, up front
Everything `servlo install` needs root for happens in one step at the very start, before any downloading or container work: the unprivileged-port sysctl so nginx can bind 80 and 443, systemd linger so your containers survive logout, and a passwordless sudoers rule for the DNS resolver operations. It runs as `sudo servlo bootstrap --system`, the same command the apt package runs as root, so both routes apply identical settings. The mkcert CA is trusted in the system store the same way once it has been generated.

Reinstalling for an update or a test reuses what is already in place and does not ask again, and if a step cannot run through `sudo` it falls back to prompting for each one separately. Uninstalling takes the sudoers rule and the CA back out, so both last exactly as long as servlo does.
:::

After install, reload your shell or open a new terminal so `PATH` takes effect.

`servlo install` will:

1. Check that the host ports servlo binds first (HTTP 80, HTTPS 443, DNS 5300) are free
2. Create XDG config and data directories
3. Create the `servlo` Podman network
4. Download static binaries: Composer, fnm, mkcert
5. Install the mkcert CA into your system trust store
6. Write and start the `servlo-dns` and `servlo-nginx` Podman Quadlet containers
7. Enable the `servlo-watcher` background service (auto-discovers new projects)
8. Add `~/.local/share/servlo/bin` to your shell's `PATH`

The downloaded tools are pinned to explicit versions, so a fresh install always gets the same Composer, fnm and mkcert regardless of what upstream shipped that day. The pins live in a small manifest published in the servlo repository: the binary fetches it before downloading and falls back to its embedded copy when offline, so a broken pin can be fixed for every install without waiting for a release. Downloads retry transient network and server errors with a short backoff, and a stalled transfer is cancelled and retried instead of hanging, so a momentary CDN hiccup doesn't abort the install. Already-installed tools are never touched by an upgrade; `servlo status` shows their versions and `servlo tools:update` brings them to the current pins when you want that.

::: info Running alongside Laravel Herd or another local stack
If another tool is already serving sites on ports 80/443 (Laravel Herd, a system nginx/Apache) or holding the DNS port, install prints a warning naming each busy port and how to find the process. Install still continues, so stop the other stack to free the ports first, otherwise `servlo-nginx` and `servlo-dns` will fail to start.
:::

---

### Install from a local build

If you built from source and want to skip the GitHub download:

```bash
make build
bash install.sh --local ./build/servlo
```

---

### Update

```bash
servlo update
```

Fetches the latest release from GitHub, downloads the binary for your architecture, and atomically replaces the running binary. No restart needed.

You can also re-run the installer:

::: code-group

```bash [curl]
curl -fsSL https://raw.githubusercontent.com/realrashid/servlo/main/install.sh | bash -s -- --update
```

```bash [wget]
wget -qO- https://raw.githubusercontent.com/realrashid/servlo/main/install.sh | bash -s -- --update
```

:::

---

### Uninstall

```bash
servlo uninstall
```

Stops all containers, disables and removes Quadlet units, removes the watcher service, removes the binary, tears down the `servlo` podman network (including aardvark-dns runtime state), and cleans up the `PATH` entry from your shell config.

Four opt-in prompts before finishing:

1. **Remove all config and data**: deletes `~/.config/servlo` and `~/.local/share/servlo` (takes your `sites.yaml`, bundled binaries, TLS certs, and all service data with it). Global npm packages that the `npm` shim installed into servlo's managed prefix are not silently lost: when a system npm exists you're offered a reinstall into your own prefix first, and otherwise the exact `npm install -g …` line to run afterwards is printed.
3. **Uninstall mkcert CA**: runs `mkcert -uninstall` so browsers and OS trust stores stop trusting the servlo CA that `install` originally added.
4. **Purge servlo-built container images**: removes `servlo-php*-fpm:local`, `servlo-custom-*:local`, and `servlo-dnsmasq:local`. Upstream pulled images (mysql/redis/postgres/etc.) are deliberately left alone; they're expensive to re-pull and your database/app data lives in host bind mounts, not inside the images, so nothing is lost by keeping them.

To answer yes to every prompt without interaction:

```bash
servlo uninstall --force
```

The installer's own `--uninstall` stops the user units and removes the binary, but the DNS setup lives outside your home directory and only servlo can take it back out: the `servlo0` link unit, the NetworkManager rules and dispatcher, the drop-in that empties `FallbackDNS`, and the passwordless sudoers rule the DNS operations run under. So when it finds that configuration it offers to run `servlo dns:disable` first, and prints the root commands to clear it by hand if you decline or the binary has already gone.

---

### Check prerequisites only

```bash
bash install.sh --check
```

---

