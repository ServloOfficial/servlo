# Requirements

## Linux

- **Distribution**: Arch, Debian/Ubuntu, Fedora-based, or omarchy
- **[Podman](https://podman.io/)** 4.5 or newer, rootless, with systemd user session active
- **[crun](https://github.com/containers/crun)**: recommended OCI runtime for rootless Podman
- **DNS resolver**: [NetworkManager](https://networkmanager.dev/) or [systemd-resolved](https://www.freedesktop.org/software/systemd/man/systemd-resolved.service.html) (at least one is required for `.test` DNS)
- **`systemctl --user` functional**: run `loginctl enable-linger $USER` if needed

### Podman 4.5 minimum

::: warning
Servlo creates the `servlo` podman network with `podman network create --dns`, a flag added in podman 4.5 (April 2023). Older releases fail install with `Error: unknown flag: --dns`. Distribution defaults that ship podman older than 4.5:

| Distro                 | Default podman | Workaround                                                                       |
|------------------------|---------------:|----------------------------------------------------------------------------------|
| Ubuntu 22.04           | 3.4.4          | Install a newer podman from the [Kubic libcontainers OBS repo](https://podman.io/docs/installation#ubuntu-2204-2104-2010-2004) |
| Zorin 17               | 3.4.4          | Same Kubic instructions as Ubuntu 22.04 (Zorin 17 is jammy-based)                |
| Debian 12 (bookworm)   | 4.3.1          | `sudo apt install -t bookworm-backports podman` (ships 4.9+)                     |
| Debian 11 (bullseye)   | 3.0.1          | Upgrade to Debian 12 + enable bookworm-backports                                 |

Fedora 38+, Ubuntu 24.04+, openSUSE Tumbleweed, Arch and CachyOS all ship podman 4.5 or newer out of the box.
:::

::: warning Linger must be enabled
If `systemctl --user` units do not survive logout, run:
```bash
loginctl enable-linger $USER
```
This is required for Podman Quadlet containers to start automatically and persist across sessions.
:::

::: tip crun is the recommended OCI runtime
Most distributions ship `crun` as the default rootless Podman runtime. On Arch-based systems, `runc` is the default and `crun` must be installed separately. While both runtimes work, `crun` is lighter and purpose-built for rootless containers. `servlo doctor` will warn if `crun` is not installed.

```bash
# Arch / omarchy
sudo pacman -S crun

# Debian / Ubuntu
sudo apt install crun

# Fedora
sudo dnf install crun
```
:::

- **`unzip`**: used during install to extract fnm
- **`certutil` / `nss-tools`**: for mkcert to install the CA into Chrome/Firefox. Only needed when servlo manages DNS for `.test` sites with HTTPS. If you pick the `.localhost` mode at install time the installer skips this package, so immutable hosts like Fedora Silverblue don't need to layer it.
    - Arch: `nss`
    - Debian/Ubuntu: `libnss3-tools`
    - Fedora: `nss-tools`

::: tip Go is only needed to build from source
The released binary is fully static with no runtime dependencies. You do not need Go installed to use Servlo.
:::
