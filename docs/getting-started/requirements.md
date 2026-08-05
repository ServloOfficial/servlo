# Requirements

Servlo runs on **Ubuntu 24.04 LTS** and refuses to install anywhere else. The installer verifies everything below before it writes anything, and stops with a specific fix rather than leaving a half-installed system behind.

## What the installer verifies

| Requirement | Why | If it is missing |
|---|---|---|
| **Ubuntu 24.04 LTS** | The only release Servlo supports | Refuses, naming the distribution it found |
| **[Podman](https://podman.io/) 4.5+**, rootless | Older podman cannot create the network Servlo needs | Refuses, with the upgrade path for the release you are on |
| **[crun](https://github.com/containers/crun)** | Servlo's container units name crun as the runtime | `sudo apt install crun` |
| **cgroup v2** | Per-container resource limits, including the memory cap asset builds run under | Add `systemd.unified_cgroup_hierarchy=1` to the kernel command line and reboot |
| **systemd linger** | Servlo's units are systemd *user* units | `loginctl enable-linger $USER` — the installer offers to run it for you |
| **`unzip`** | Extracting fnm during install | Offered as a package install |

Ubuntu 24.04 ships podman 4.9 and boots cgroup v2 by default, so on a stock droplet linger is the only one you are likely to hit.

### Why podman 4.5

Servlo creates the `servlo` podman network with `podman network create --dns`, a flag added in podman 4.5 (April 2023). Older releases fail with `Error: unknown flag: --dns`. Ubuntu 22.04 ships podman 3.4.4 and cannot provide a newer one from its own archive, which is why the installer points at `do-release-upgrade` rather than at a package.

### Why linger matters

Every container, every worker and the panel itself runs as a systemd **user** unit. Without linger, systemd tears the whole user manager down when your session ends, so closing an SSH connection stops every site on the server. This is the requirement most often missed on a fresh droplet, and enabling it needs no privilege, so the installer offers to run it before it refuses:

```bash
loginctl enable-linger $USER
```

`servlo doctor` re-checks it on every run, because a rebuild or a user change can quietly take it away again.

### Why cgroup v2

Rootless podman needs the unified hierarchy to apply resource limits at all. On cgroup v1 a limit is accepted and then silently ignored, which is worse than refusing it: an oversized `npm run build` would run uncapped and let the OOM killer take MySQL down with it.

## Also needed

- **DNS resolver**: [NetworkManager](https://networkmanager.dev/) or [systemd-resolved](https://www.freedesktop.org/software/systemd/man/systemd-resolved.service.html)
- **`libnss3-tools`** (for `certutil`): only when servlo manages DNS for `.test` sites with HTTPS. The `.localhost` mode skips it.

::: tip Go is only needed to build from source
The released binary is fully static with no runtime dependencies. You do not need Go installed to use Servlo.
:::
