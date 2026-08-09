# SFTP per site

An SFTP key can be authorised for one site, so a designer or a client can move files in and out of that site's directory without a panel login and without a shell.

SFTP on Linux is an OpenSSH subsystem, and OpenSSH belongs to root. Servlo does not run as root and does not ask to (CLAUDE.md §3.2), so this feature splits in two, and being clear about which half is which is most of the page.

**The half Servlo does, with no privilege at all:** authorising keys. A public key goes into the Servlo user's own `~/.ssh/authorized_keys`, inside a marked block, restricted so the session gets no shell and opens in the site's directory.

**The half Servlo will not do:** the chroot that stops the session leaving that directory. Servlo generates the configuration and prints the exact `sudo` block. You run it.

---

## The thing to be clear about first

Every site on a Servlo machine runs as the **same Linux user** (PRD §6). There are no per-site Linux accounts and Servlo will not add any.

So "locked to that site's directory" is a directory lock, not a boundary between people. Specifically:

- With the chroot installed, an SFTP session **cannot** leave its site's directory. That is the kernel enforcing it, and it holds even if Servlo has a bug.
- The same person, given a shell on the machine by any other route, is the same Unix user as every site and can read all of them. The chroot constrains the SFTP session, not the account.
- Anything a site's PHP can read, that site's PHP can read for every other site too. That was already true before SFTP existed; SFTP does not make it worse and does not fix it.

If you need one client to be unable to read another client's files at the operating-system level, Servlo is not that product yet. Per-site Linux users are on the deferred list (PRD §10) for exactly this reason.

## Authorising a key

In the panel: **System → SFTP**. Pick the site, give the key a label, paste the public key.

From the CLI:

```bash
servlo sftp add acme.com alice-laptop ~/keys/alice.pub
servlo sftp status
servlo sftp remove SHA256:JxZlDUD5LXn0p3ds8TvW2al0AZAmpvu1pWkeuW/xAMk
```

A label may contain letters, digits, dots, dashes and underscores, up to 64 characters, and nothing else. That is not fussiness: the label is written into `authorized_keys` verbatim, and a label with a newline in it would be a second key line that Servlo did not write and did not restrict.

Pass a `.pub` file, never a private key. Servlo refuses anything holding `PRIVATE KEY`, refuses a paste carrying its own `authorized_keys` options, and refuses a paste holding more than one key.

The line Servlo writes looks like this:

```
restrict,command="internal-sftp -d /home/deploy/sites/acme" ssh-ed25519 AAAA… servlo-sftp site=acme.com label=alice-laptop
```

`restrict` turns off the pty, port forwarding, agent forwarding, X11 and user-rc. The forced command is the SFTP subsystem and nothing else, so the key is file transfer and not a way onto the box.

### It is additive, always

Servlo owns only the lines between its `# BEGIN servlo sftp` and `# END servlo sftp` markers. Your own keys, and anything another tool put in the file, are copied through byte for byte on every write. The file is written `0600` and `~/.ssh` is set to `0700`, because sshd silently refuses a group-writable `authorized_keys` and tells only its own log.

**Servlo never disables password authentication.** Nothing here writes to `/etc/ssh/sshd_config`, and the drop-in Servlo generates does not set `PasswordAuthentication`, `PermitRootLogin` or `PubkeyAuthentication`. Hardening yourself out of your own droplet is not a service Servlo provides; if you want key-only SSH, that is your edit to make.

## Confining the session: the sudo block

Until you run this, an authorised key opens in the site's directory but is **not confined to it**. The panel says so, in a banner, and `servlo sftp status` says so on the terminal. Both keep saying it until the block is installed.

Confinement is sshd's `ChrootDirectory`, and it comes with a hard requirement: the chroot and every parent must be owned by `root` and writable by nobody else. A process running as the Servlo user cannot create such a directory, cannot chown one to root, and cannot bind-mount the site inside it. There is no way round that and Servlo does not pretend otherwise.

There is a second problem, and it is the reason the layout looks unusual. sshd tells sessions apart with `Match`, and `Match` can key on a user, a group, an address or a **local port**. It cannot key on "which site", and every site here is the same Linux user, so user and group are useless. That leaves the port. Each site with SFTP enabled gets its own port on the same sshd, allocated from 2200 upwards and remembered in `~/.local/share/servlo/sftp-ports.yaml` so it never moves under a saved connection.

Generate and print the block:

```bash
servlo sftp setup
```

Servlo writes the sshd configuration to `~/.config/servlo/sftp/60-servlo-sftp.conf` — read it first — and prints roughly this, for one site on port 2200:

```bash
sudo install -d -o root -g root -m 755 /srv/servlo-sftp
sudo install -d -o root -g root -m 755 /srv/servlo-sftp/acme.com
sudo install -d -o deploy -g deploy -m 755 /srv/servlo-sftp/acme.com/site
sudo mountpoint -q /srv/servlo-sftp/acme.com/site || mount --bind /home/deploy/sites/acme /srv/servlo-sftp/acme.com/site
sudo grep -qF "/home/deploy/sites/acme /srv/servlo-sftp/acme.com/site none bind 0 0" /etc/fstab || printf '%s\n' "/home/deploy/sites/acme /srv/servlo-sftp/acme.com/site none bind 0 0" >> /etc/fstab
sudo install -m 644 -o root -g root /home/deploy/.config/servlo/sftp/60-servlo-sftp.conf /etc/ssh/sshd_config.d/60-servlo-sftp.conf
sudo sshd -t
sudo systemctl reload ssh
```

Reading it in order: a root-owned chroot, a mount point inside it owned by the account that owns the sites, the site bind-mounted onto that mount point, the same bind recorded in `/etc/fstab` so it survives a reboot, then the sshd drop-in, then `sshd -t` before the reload so a configuration Servlo generated wrong is refused on your terminal instead of taking sshd down.

The generated drop-in restates the ports sshd already listens on. That is not decoration: declaring any `Port` in a drop-in replaces sshd's default of 22, and a file that named only the SFTP ports would take your own way in away from you at the next reload. Servlo reads `/etc/ssh/sshd_config` and the other drop-ins to find them, skipping its own.

### Connecting

```bash
sftp -P 2200 deploy@acme.com
```

The user is the Servlo account, the same one for every site. The port is what picks the site.

### When you add another site

The drop-in is generated whole, for every site with a key. Add a key to a second site and the installed file is out of date, so the new site is not confined. The panel marks it, `servlo sftp status` says so, and the fix is to run `servlo sftp setup` and install the block again.

## What this needs a real server to prove

Everything above is generated and checked by tests, but the tests cannot start an sshd. On a real Ubuntu host the things worth confirming once are: that `sshd -t` accepts the generated file, that a session on the site's port lands in `/site` and cannot `cd ..` out of it, that the bind mount comes back after a reboot, and that your existing SSH login on port 22 still works after the reload.
