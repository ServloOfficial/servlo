# What to know before you put clients on this

Servlo makes some deliberate trades to be a single-operator panel rather than
multi-tenant hosting. They are the reason it is a months-long build instead of a
years-long one, and they are reasonable for your own and your clients' sites on
your own server. They are also worth knowing now rather than discovering later,
which is what this page is for.

## Every site runs as the same Linux user

There are no per-client Linux accounts here. Every site's PHP runs as the user
that installed Servlo.

The consequence is direct: a site that gets compromised — an outdated WordPress
plugin, a vulnerable dependency — can read every other site's `.env` on this
server, and therefore every other site's database credentials.

Two things narrow what that is worth:

**Each site has its own database account**, granted only on its own schemas. A
leaked `.env` buys the attacker that site's data, not the server's. Servlo
issues these accounts itself and can rotate them; see
[Database](/usage/database).

**Don't put a site you do not control next to a site that matters.** The
cheapest mitigation is not co-locating. A client's neglected WordPress and your
own billing application do not belong on one machine, and no amount of
configuration changes that.

A per-site FPM pool running as its own Linux user is a possible later hardening.
It is not here today, and this page will say so until it is.

## A deploy is not atomic

There are no `releases/` directories and no symlink swap. A deploy pulls into
the live directory and runs the script there, so during `composer install` the
site is briefly serving a tree that is part old and part new.

The order is chosen to make that window as safe as it can be: a migration never
runs without a database backup taken first, the script never runs against a tree
that only half-pulled, and new code never goes live if the script meant to
prepare it failed. Each step refuses to start because the one before it did not
finish.

What that leaves you:

- **A pre-deploy database backup** on any deploy whose script migrates, and the deploy is refused outright if that backup fails
- **Redeploy the previous commit** in one click, which puts the code back but not the database — the backup above is what covers that
- **[Staging](/usage/staging)**, if the site is busy enough that the window matters

Deploy at a quiet hour for a site where a few seconds of mixed state is a real
problem. See [Deploy](/usage/deploy).

## Logs will fill the disk if nothing rotates them

A production server that never rotates logs fills its disk and takes every site
on it down — nginx included, so the failure is total rather than partial.

Rotation with configurable retention is on by default rather than being
something to remember, and disk is an alert category so you hear about it before
it is a problem. See [Logs](/usage/logs) and [Alerts](/usage/alerts).

## SSH password authentication stays on

Servlo never disables password login and never restarts sshd. Turning off
password authentication on a machine whose owner has not added a key yet locks
them out of their own server, which is what hardening scripts do and why this one
does not.

fail2ban is configured at install and bans an address after five failures, which
is the actual defence against the brute-force attempts that start within minutes
of a server going live. Servlo manages authorised keys additively, so you can
migrate to keys whenever you choose — and then turn password login off yourself,
deliberately. See [Security](/usage/security).

## There is no mail server

No postfix, no dovecot, no port 25 listening. Deliverability, DKIM, SPF and
reputation are a product of their own and a bad one to get half right.

Each site takes SMTP credentials you supply, and the panel has its own set for
alerts. See [Email](/usage/email).
