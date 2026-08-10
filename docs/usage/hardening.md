# Hardening

```bash
servlo harden
```

Checks what this server looks like from outside and prints what to do about it. It changes nothing.

That division is deliberate and it is the same one the rest of servlo keeps: **servlo never runs `sudo` on your behalf.** Everything here that needs root, and most of it does, is written out as the exact command for you to run.

## What it checks

**What is actually listening on a public address.** This is the one thing servlo can always determine for itself, from `/proc`, and it is the fact that matters most: a firewall rule is a claim about what should be reachable, and an open socket is what is. Loopback listeners are left out, because servlo runs a great many of them and listing them would bury the handful that are exposed.

Anything open that is not SSH, HTTP or HTTPS is a finding. Every servlo service binds to the container network, so a port here is either a published port that is no longer needed or a service bound to the wrong address.

A server with IPv6 disabled has no `/proc/net/tcp6`, which is ordinary and does not stop the check: the IPv4 answer is still reported. Only being unable to read any table at all is an error, because reporting no open ports because nothing could be read is the most reassuring possible lie.

**fail2ban.** Servlo keeps SSH password authentication enabled by design, which makes fail2ban the thing standing between this server and an automated password guesser. Its socket is the honest signal available without root; reading its ban list needs `sudo fail2ban-client status sshd`, which is printed rather than run.

**Unattended upgrades**, so security updates install themselves.

**The modes on everything servlo stores.** Half of it is a credential: the backup key, the database connections, the per-site database accounts, the backup destinations, the panel's SMTP settings. Servlo runs every site as one Linux user, so "readable by another account" is one of the few boundaries that genuinely holds here, and a credential at `0644` is a real finding rather than a note.

## The firewall

`servlo harden` prints the ufw configuration a servlo server wants, in an order that is safe to paste:

```bash
sudo ufw allow 22/tcp comment 'SSH'
sudo ufw allow 80/tcp comment 'HTTP and ACME challenges'
sudo ufw allow 443/tcp comment 'HTTPS'
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw --force enable
```

The order is the whole reason it is generated rather than left to be typed. **SSH is allowed before the firewall is enabled**, every time, because the other order ends the session running it and locks you out of the machine you are configuring. Pass `--ssh-port` if sshd is not on 22; opening 22 on a server whose sshd is on 2222 is the same lockout with extra steps.

Port 80 stays open even on a server with no certificate yet, because that is where the HTTP-01 challenge is answered and closing it makes the first **Get SSL** fail with nothing to point at.

## The provider's firewall

DigitalOcean, AWS and the rest filter in front of the machine, where servlo cannot see it at all. The audit says so on every run, which is the point: a port that is open in `ufw` and still unreachable is a cloud firewall, and knowing that saves debugging the wrong layer for an afternoon.

## SSH keys

Authorised keys are managed additively through the SFTP page and `servlo sftp`. A key servlo did not put in `authorized_keys` is never disturbed.

**Servlo never disables password authentication.** That is a deliberate decision, not an oversight: locking a key-only server out of itself is a worse failure than the one it prevents, and fail2ban is what covers the gap.
