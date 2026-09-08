# Alerts

What is currently wrong with this server, in one list.

```bash
servlo alerts
servlo alerts clear <kind> [site]
```

The dashboard shows the same list in a **Needs attention** card, and the card is not there at all when nothing is wrong.

## What raises one

| Kind | Raised when |
|---|---|
| `site_down` | A site stopped answering its health path |
| `cert_renew_failed` | Issuing or renewing a certificate failed |
| `backup_failed` | A backup did not complete, or no copy of it left this server |
| `backup_unverified` | The newest archive could not be restored into a scratch database |
| `deploy_failed` | A deploy did not finish |
| `worker_down` | A worker is not running when it should be |
| `disk_filling` | A filesystem servlo uses is 90% full or more |

Three of those are raised by whatever failed, at the moment it failed: a deploy, a backup, an issuance. The other three have nothing that fails at a moment anybody could be told about, so they are looked for on a timer every five minutes.

## The same failure is one alert

A backup that has been failing nightly since March is one thing wrong, not a hundred and forty. The first one is recorded and emailed, and every repeat after that changes nothing until it recovers.

That is also why the alert carries no history. It is a list of what is wrong now, not a log of what has ever been wrong; the audit log is the log.

## Alerts clear themselves

When the thing an alert is about recovers, the alert goes away: the next successful deploy, the next backup that works, the next check that finds the site answering. This is the half that makes the list worth reading. An alert that outlives its problem teaches an operator to ignore the list, which costs more than the alert was ever worth.

**Dismiss** is for the one that did not: a failure servlo cannot see the end of, or one you have decided to live with. It changes nothing about the server, and if the failure is still happening the next check raises it again.

## Email

An alert is emailed once, through the panel's own SMTP account, at the moment it is raised.

The panel is where an alert lives and email is a copy of it. That order matters: SMTP is optional, a mail server can be down, and an alert that only ever existed as an email nobody received is exactly the failure this is meant to prevent. So an alert is written down first and sent second, and a send that fails is reported without losing the alert.

A server with no panel SMTP configured sends nothing and says nothing about it. That is the ordinary state of a fresh install, not a fault. Set it up in the dashboard under **System, Mail**; there is no command for it, because the one thing it holds is a password and a shell is the wrong place to put one.

## The uptime check

Each site is asked for the path its framework declares, under `deploy.health` in the framework's definition:

```yaml
deploy:
  health: /up
```

A framework that names none is asked for its home page, which is a weaker check and still catches the failure that matters.

The request goes over the loopback with the site's own hostname in the URL, so it passes through nginx, the site's vhost and its PHP-FPM pool exactly as a visitor's would while never leaving the machine. SNI and certificate verification see the real domain, so a secured site's certificate is checked too.

Be clear about what that proves. It proves the stack servlo owns is serving. It says nothing about DNS, a cloud firewall, or the network between here and a visitor, because those are what a check running on this server cannot see. If you need to know the site is reachable from outside, that is an external monitor's job and always was.

Anything the server answers counts as up except a 5xx. A 404 on a health path is a wrong path, not an outage, and reporting it as one would have you chasing the wrong thing at three in the morning. A 502 or 503 is the shape of a dead PHP-FPM pool behind a live nginx, which is the commonest way a site goes down without the server going anywhere.

A paused site is not checked. Reporting it down would be reporting your own decision back at you, every five minutes.

## Disk

The threshold is 90%, measured against the space a non-root process can actually use rather than against the total. Ext4 reserves five percent for root by default, and counting that as free is how a disk reports room left while every write servlo makes is already failing.

Every filesystem servlo touches is watched, deduplicated by filesystem rather than by path, so a droplet with one root volume raises one alert and a server with sites on a separate volume gets both.
