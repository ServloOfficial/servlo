# Installation

## Linux

::: warning Requires systemd
Servlo runs every container as a Podman Quadlet and every worker as a systemd user service, so a systemd-based distro is required. OpenRC (Gentoo, Artix-openrc, Alpine), runit (Void, Artix-runit), s6, and sysvinit-based distros (Devuan) are not supported.

Tested and known-good: Ubuntu, Fedora, Arch, Debian, Mint, Pop!_OS, openSUSE, CachyOS, Omarchy. Any systemd distro should work.
:::

### One-line installer (recommended)

::: code-group

```bash [curl]
curl -fsSL https://raw.githubusercontent.com/ServloOfficial/servlo/main/install.sh | bash
```

```bash [wget]
wget -qO- https://raw.githubusercontent.com/ServloOfficial/servlo/main/install.sh | bash
```

```bash [From source]
git clone https://github.com/ServloOfficial/servlo
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
Everything `servlo install` needs root for happens in one step at the very start, before any downloading or container work: the unprivileged-port sysctl so nginx can bind 80 and 443, systemd linger so your containers survive logout, and the server basics below. It runs as `sudo servlo bootstrap --system`, the same command the apt package runs as root, so both routes apply identical settings.

## Server basics

A fresh droplet is missing four things before it is a server worth putting a site on. `sudo servlo bootstrap --system` applies them; an install run without it prints the exact commands instead, and `servlo doctor` re-checks them on every run because a rebuilt machine forgets.

**Swap**, sized from installed memory: double the RAM below 2GB, matching RAM up to 8GB, capped there. The small end is the one that matters — a 1GB droplet running `composer install` or an asset build will exhaust itself, and with no swap the OOM killer takes MySQL rather than the build. Above 8GB more swap stops helping; a machine swapping that hard needs fewer sites, not more disk. A machine that already has swap is left alone. The file is created `0600` before anything is swapped to it, because a readable swap file hands every local process the memory of every other one.

**Timezone**, set to UTC. Right for a server even when you are not in it: logs, cron schedules, backup timestamps and certificate expiry all get compared across machines, and a local timezone makes every one of those a conversion somebody has to remember. It also removes daylight saving, which otherwise makes one hour a year happen twice and another never.

**Unattended security updates**, security only and never rebooting on their own. Pulling every update automatically means a package changing behaviour under a running site with nobody watching, which is worse than being a week behind on a non-security release. A site going down at 3am because a kernel landed is not an improvement over a reboot you apply deliberately.

**fail2ban in front of SSH**, banning an address after five failures for an hour.

> [!IMPORTANT]
> Servlo never touches SSH password authentication, and never restarts sshd. Turning off password login on a machine whose owner has not added a key yet locks them out of their own droplet, which is what "hardening" scripts do and why this one does not. fail2ban throttles the attempts and the login method stays yours. A test in the repository asserts that no server basic mentions `sshd_config`, `PasswordAuthentication` or `PermitRootLogin`, so this cannot drift.


Reinstalling for an update or a test reuses what is already in place and does not ask again, and if a step cannot run through `sudo` it falls back to prompting for each one separately.
:::

After install, reload your shell or open a new terminal so `PATH` takes effect.

`servlo install` will:

1. Check that the host ports servlo binds first (HTTP 80, HTTPS 443) are free
2. Create XDG config and data directories
3. Create the `servlo` Podman network
4. Download static binaries: Composer, fnm
5. Write and start the `servlo-nginx` Podman Quadlet container
6. Enable the `servlo-watcher` background service (auto-discovers new projects)
7. Add `~/.local/share/servlo/bin` to your shell's `PATH`

The downloaded tools are pinned to explicit versions, so a fresh install always gets the same Composer and fnm regardless of what upstream shipped that day. The pins live in a small manifest published in the servlo repository: the binary fetches it before downloading and falls back to its embedded copy when offline, so a broken pin can be fixed for every install without waiting for a release. Downloads retry transient network and server errors with a short backoff, and a stalled transfer is cancelled and retried instead of hanging, so a momentary CDN hiccup doesn't abort the install. Already-installed tools are never touched by an upgrade; `servlo status` shows their versions and `servlo tools:update` brings them to the current pins when you want that.

::: info Running alongside another web server
If something is already serving on ports 80/443, typically a system nginx or Apache, install prints a warning naming each busy port and how to find the process. Install still continues, so stop the other stack to free the ports first, otherwise `servlo-nginx` will fail to start.
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
curl -fsSL https://raw.githubusercontent.com/ServloOfficial/servlo/main/install.sh | bash -s -- --update
```

```bash [wget]
wget -qO- https://raw.githubusercontent.com/ServloOfficial/servlo/main/install.sh | bash -s -- --update
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
4. **Purge servlo-built container images**: removes `servlo-php*-fpm:local` and `servlo-custom-*:local`. Upstream pulled images (mysql/redis/postgres/etc.) are deliberately left alone; they're expensive to re-pull and your database/app data lives in host bind mounts, not inside the images, so nothing is lost by keeping them.

To answer yes to every prompt without interaction:

```bash
servlo uninstall --force
```

The installer's own `--uninstall` stops the user units and removes the binary. An install that predates the removal of servlo's DNS stack still has root-owned resolver files outside your home directory, so the uninstaller detects that set and offers to clear it, printing the root commands if you decline.

---

### Check prerequisites only

```bash
bash install.sh --check
```

---

