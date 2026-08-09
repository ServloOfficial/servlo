# Cron

Every site has a **Cron** tab: a list of commands, when each one runs, and what happened the last time it did. Each entry is one systemd user timer and one oneshot service, written by servlo and inspectable with `systemctl --user` like anything else on the machine.

## What an entry is

Four things: a name, a command, a schedule, and whether the output is kept.

The command runs **inside the site's container**, from the site's directory, exactly the way its workers and its deploy script do. So a WordPress site's entry runs against the PHP version that site is pinned to, with that site's extensions, and `php` means the same thing it means everywhere else in the panel.

Servlo writes a pair of units per entry, named the way every other servlo unit is named:

```
~/.config/systemd/user/servlo-cron-<site>-<id>.service   the oneshot that runs the command
~/.config/systemd/user/servlo-cron-<site>-<id>.timer     when it runs
```

The identifier comes from the name you typed (`Prune batches` becomes `prune-batches`) and never changes afterwards, so renaming an entry does not move its unit or lose its history.

## Schedules

Two forms are accepted, because those are the two an operator arrives with:

| You type | Servlo writes |
| --- | --- |
| `*/5 * * * *` | `OnCalendar=*-*-* *:0/5:00` |
| `0 3 * * *` | `OnCalendar=*-*-* 03:00:00` |
| `15 2 * * 1-5` | `OnCalendar=Mon..Fri *-*-* 02:15:00` |
| `@daily`, `@hourly`, `@weekly`, `@monthly`, `@yearly` | `daily`, `hourly`, … |
| `daily`, `minutely`, `Mon *-*-* 02:00:00` | kept as typed |

A crontab line is translated to a [systemd calendar expression](https://www.freedesktop.org/software/systemd/man/systemd.time.html) and both are stored: what you typed is what the form shows you back, and the translation is what the timer runs on. A schedule servlo cannot read is refused with an example to copy rather than a parser error.

One cron form has no systemd equivalent and is refused rather than guessed at: a line with **both** a day-of-month and a day-of-week. `crontab` runs such a line on either of them; a systemd calendar needs both to match at once. Pick one.

Timers are written `Persistent=true`, so a nightly job on a droplet that was powered off at 03:00 runs when it comes back rather than skipping the day in silence.

## The last run

The status beside each entry comes from systemd, not from the panel's memory, which is why it is still right after `servlo restart` or a reboot:

- **Never run** — systemd has no record of this entry running. A brand-new entry, and worth telling apart from a failure.
- **Succeeded** — the last invocation exited 0.
- **Failed** — with systemd's own word for it (`exit-code`, `timeout`, `signal`, `oom-kill`) and the exit status, so the same terms find the run again in `journalctl`.
- **Running** — the invocation is still going.
- **Not scheduled** — the entry is kept but its timer is stopped.

**Keep the output** sends the command's stdout and stderr to the journal, which is where the panel reads the last run's output from. Turn it off for something that prints every minute and the unit is written `StandardOutput=null`, so nothing accumulates; the panel then says plainly that this entry keeps no output rather than showing you an older run's.

Everything the panel shows is in the journal too:

```
journalctl --user -u servlo-cron-<site>-<id>
systemctl --user list-timers 'servlo-cron-*'
```

## Deleting an entry

Deleting takes the timer and the service off the machine: stopped, disabled, both unit files removed, `daemon-reload`. It asks first, because a timer that stops firing is a change to what the server does and there is nothing in the site to notice it from afterwards. The action is recorded in the audit log like every other state-changing route.

Unlinking a site takes its schedules with it, for the same reason: a timer left behind fires a command at a directory nothing serves any more.

## Things a command cannot contain

The command is one line of a systemd unit, and systemd is not a shell:

- **No newline or NUL.** Either would add directives nobody wrote.
- **No `$`.** systemd expands `$NAME` on a command line before the shell ever sees it, and an unset variable expands to nothing, so a command carrying one would quietly run as something other than what it says. Put the work in a script inside the site and schedule that.

`%` and `"` are fine: servlo escapes them. Everything else is handed to `/bin/sh -c` inside the container, so pipes, `&&` and redirects work.

A host-proxy site has no container to run a command in, so it has no Cron tab. The panel says why rather than offering a form whose every entry would fail once a minute.

## Replacing a framework's built-in scheduler

Some frameworks run their scheduled work off page loads. WordPress is the case: `wp-cron.php` fires on a visitor's request, which means the schedule is only as reliable as the traffic and every visitor pays for whatever was due. A site with no visitors at 3am does not publish the post scheduled for 3am.

On a WordPress site the Cron tab shows a **Built-in scheduler** card. Switching it to a system timer does two things:

1. Sets `DISABLE_WP_CRON` to the PHP literal `true` in `wp-config.php`.
2. Installs a one-minute entry running `php wp-cron.php`, marked as one servlo manages.

Switching it back sets the constant to `false` and removes the entry, in that order, so the site is never left with the built-in scheduler off and nothing in its place. The value is written unquoted on purpose: PHP reads every non-empty string as true, so `define('DISABLE_WP_CRON', 'false')` would leave the scheduler off while appearing to say otherwise.

The managed entry appears in the list like any other, but is edited from the card rather than in the row: it is the switch's, and a hand-edit would be undone the next time the switch was used.

**None of this is WordPress-specific code.** The framework store declares it, and any framework definition may:

```yaml
pseudo_cron:
  label: WordPress cron
  description: wp-cron.php runs on page loads, so scheduled posts and updates wait for a visitor.
  file: wp-config.php
  constant: DISABLE_WP_CRON
  replaced_value: "true"    # PHP literals, written unquoted
  restored_value: "false"
  entry:
    id: wp-cron
    name: WordPress cron
    command: php wp-cron.php
    schedule: "* * * * *"
```

A framework definition with no `pseudo_cron` block shows no card. See [Framework definitions](framework-definitions.md).

## Pausing a site

Pausing takes the schedule down with everything else. `servlo pause <site>` stops and disables every one of that site's timers, so a command cannot keep running against a site whose vhost is serving the paused page and whose workers are stopped. `servlo unpause` puts back exactly what was scheduled, and an entry you had switched off stays off.

The unit files stay on disk while the site is paused, so nothing has to be retyped, and `systemctl --user list-timers` shows them inactive rather than gone.

## The API

| Route | What it does |
| --- | --- |
| `GET /api/sites/{domain}/cron` | the schedule, each entry's last run, and the framework's pseudo-cron state |
| `POST /api/sites/{domain}/cron` | add an entry, or replace one by `id` |
| `DELETE /api/sites/{domain}/cron/{id}` | remove an entry and its units |
| `POST /api/sites/{domain}/pseudo-cron` | `{"replace": true}` to switch to a timer, `false` to put the built-in scheduler back |

All four are site-scoped: a Developer reaches them for the sites assigned to them.
