# Security

**System → Security** in the panel, and `servlo harden` on the command line. Both answer one question: what does this server look like from outside, and what should be done about it.

## What servlo can and cannot do here

Almost everything on this page needs root. ufw, fail2ban and sshd all do, and servlo does not run as root and never runs `sudo` on your behalf. So the page is in two halves, in this order:

- **Everything above the keys is reported.** Servlo says what it found and prints the exact command, in an order that is safe to paste. You run it.
- **The SSH keys at the bottom are the one thing servlo changes itself,** because `~/.ssh/authorized_keys` belongs to the account servlo already runs as.

That division is deliberate and it is not going to move. A panel that could reconfigure a firewall would need standing root, and standing root on the box that serves other people's sites is the thing this design exists to avoid.

## The firewall

```bash
servlo harden
servlo harden --ssh-port 2222
```

Servlo builds the ufw configuration a servlo server wants and prints it:

```bash
sudo ufw allow 22/tcp comment 'SSH'
sudo ufw allow 80/tcp comment 'HTTP and ACME challenges'
sudo ufw allow 443/tcp comment 'HTTPS'
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw --force enable
```

The order is the whole reason this is generated rather than left to be typed. **SSH is allowed before the firewall is enabled, every time.** Any other order can end the session that is running it, on a machine you may not have console access to.

Port 80 is opened even on a server with no certificate yet: that is where the HTTP-01 challenge is answered, and closing it makes the first **Get SSL** fail with nothing to point at.

The plan assumes sshd is on port 22. If yours is not, pass `--ssh-port` or change that line before running it.

### Open ports

The check worth reading is the gap between the two. A firewall rule is a claim about what should be reachable; a listening socket is what actually is. Servlo reads `/proc/net/tcp` directly, so this is the kernel's answer rather than ufw's, and anything open that the plan does not account for is called out.

Every servlo service binds to the container network. So an unexpected open port is one of two things: a published port that is no longer needed, or a service bound to the wrong address. Loopback listeners are not counted, because servlo runs a great many and listing them would bury the handful that are actually exposed.

## The provider's firewall

DigitalOcean, Hetzner, AWS and Google all filter in front of the machine, where servlo cannot see it. A packet that never arrives is indistinguishable from a visitor who never came, so **servlo cannot tell you whether a cloud firewall is blocking a port** and anything claiming to would be guessing.

What it does instead is work out which provider this is, from the metadata service on the link-local address, and link straight to that provider's firewall screen. That is the difference between an afternoon spent debugging ufw and thirty seconds spent looking at the right page.

The commonest version of this: ufw is open, the site is unreachable, and the droplet's cloud firewall has an inbound rule for SSH and nothing for 80 and 443.

## fail2ban

Servlo keeps **SSH password authentication enabled**, always. It never edits `sshd_config`. That makes fail2ban the thing standing between this server and an automated password guesser, so the panel says whether it is running.

Whether it is running is the only part servlo can see. Listing the bans and lifting one both go through `fail2ban-client`, which talks to a root-owned socket, so those are commands to run rather than buttons:

```bash
sudo fail2ban-client status sshd
sudo fail2ban-client set sshd unbanip 203.0.113.7
```

Do not read the absence of a ban list as servlo saying there are no bans. It is servlo saying it cannot see them.

## SSH keys

Added to `~/.ssh/authorized_keys` for the account servlo runs as, **additively**. Every write keeps every key already in the file, including ones servlo did not put there and any comments somebody wrote in it by hand. Removing a key removes exactly that one line.

That matters more than it sounds. This file is how you get into the server, and a panel that rewrote it wholesale would eventually lock somebody out of their own machine from a browser tab. The file is written through a temporary file and a rename, so an interrupted write leaves the previous keys rather than half a file.

A key needs a name. Without one, a list of five keys is five identical rows and nobody dares remove any of them.

Servlo sets `~/.ssh` to `0700` and `authorized_keys` to `0600` on every write, because sshd refuses to read them otherwise and the failure is a silent refusal of the key rather than an error anybody sees.

Pasting the same key twice authorises it once. Pasting a private key by mistake is refused and named, because the person doing it does not know they have done it.

**Adding a key does not turn password logins off.** If you want passwords disabled, that is a decision to make deliberately, in `sshd_config`, once you have confirmed a key works. Servlo will not make it for you.

## Config file modes

Half of what servlo stores is a credential: the backup key, the database connections, the per-site database accounts, the backup destinations, the panel's SMTP settings. Every one is checked for a mode any other account on the machine could read, and a `chmod` is printed for any that is wrong.

## Unattended upgrades

Checked, and the two commands to configure them are printed when they are not. A server that does not install its own security updates is one that is fine until it is not, on a schedule set by somebody else.
