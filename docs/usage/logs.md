# Logs

Two different things are called logs on a server, and servlo treats them differently because they are owned by different processes.

```bash
servlo logs                     # tail a container's output
servlo logs nginx
servlo logs keep                # how much application log is kept
servlo logs keep --max-size 100 --keep 7
servlo logs rotate              # rotate now
```

## What servlo rotates

The application's own log files inside each site: whatever its framework declares.

```yaml
logs:
  - path: storage/logs/*.log
    format: monolog
```

That is store data, not knowledge in Go. A framework that names log paths gets them rotated, one that names none gets nothing rotated, and adding a framework needs no servlo release.

Nothing else rotates these. A single Laravel log growing quietly for eleven months is one of the two commonest ways a droplet's disk disappears, and the other one is container images.

## When and how much

A log is rotated once it passes **50 MB**, and **5** rotated copies are kept, gzipped. That bounds a site's logs at roughly 80 MB compressed and is generous enough that nobody loses a log they were reading.

```bash
servlo logs keep --max-size 100 --keep 7
servlo logs keep --no-compress
servlo logs keep --off
```

Changing it applies from the next rotation; nothing already on disk is touched. Switching rotation off removes the timer as well, rather than leaving a disabled one behind for somebody to find and wonder about, and it is a legitimate choice for a server running its own logrotate.

The timer runs daily at 02:30, before the usual backup at 03:30, so a site's archive carries rotated logs rather than one enormous live one. It is `Persistent`, so a droplet that was off overnight rotates when it comes back.

A log under the size limit is left alone, which is why running `servlo logs rotate` by hand does not split a log you are halfway through reading.

## What rotation actually does

The live log is renamed, not copied and truncated. Every application log servlo sees is written by PHP, whose log handlers open and close around a request, so the next write lands in a fresh file. A copy would also need twice the log's size in free disk at exactly the moment a disk is filling up, which is when this runs.

A compressed rotation writes the gzip first and removes the original only once it is complete, so an interrupted rotation loses nothing.

Two things it will not touch. A file servlo itself produced (`laravel.log.2.gz`) is never rotated again, so the names cannot grow into a chain. And a symlink named like a log is measured as a link rather than as what it points at, which makes it too small to rotate and so never followed: otherwise a link in a site directory would be a way to copy something from outside the site into it.

## What servlo does not rotate

nginx, PHP-FPM and every worker run under systemd, so their output is in the journal. The journal's size is a journald setting in a system file, and changing it needs root, which servlo does not have and does not ask for. `servlo logs keep` prints the exact commands:

```bash
sudo mkdir -p /etc/systemd/journald.conf.d && printf '[Journal]\nSystemMaxUse=1G\nMaxRetentionSec=1month\n' | sudo tee /etc/systemd/journald.conf.d/servlo.conf
sudo systemctl restart systemd-journald
```

Without that, journald's own default caps the journal at 10% of the filesystem, which on a 25 GB droplet is 2.5 GB of logs before anything gives.

## Reading them

`servlo logs [target]` tails a container's output, and the panel shows the same thing per site along with the application logs. A site's application logs are also readable and clearable from its Logs tab, which is the right place to look when a page is 500ing and nginx only says upstream closed.
