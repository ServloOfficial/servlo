# Staging sites

A copy of a live site that is safe to have on the internet.

```bash
servlo staging create acme.com staging.acme.com
servlo staging refresh staging.acme.com
servlo staging password staging.acme.com
servlo staging
```

It is a site like any other: its own directory, its own domain, its own database, its own certificate, its own PHP version, its own workers, its own backups. Everything servlo does to a site works here unchanged, and that is why it is built as a site rather than as a mode.

## What makes it staging

Three things, and two of them are not optional.

**It remembers which live site it copies from.** That is what a refresh reads, and it is what makes the direction a property of the data rather than of the argument order.

**It is not indexed.** Every response carries `X-Robots-Tag: noindex, nofollow, noarchive`, including error pages and including static assets. A staging site search engines index is duplicate content against the real site, and it is found late and by somebody else.

**It is behind a password.** HTTP basic auth in nginx, with a generated password. A staging site anybody can open is a half-finished feature and a copy of the client's data on a public address.

There is no switch for either of the last two. If you want a second production site, make a site.

## The password

Generated, never chosen: 32 characters of randomness, printed once, and stored only as a bcrypt hash. A staging password somebody picked is the same password as the last five staging sites, and the whole value of this is that the copy of production data behind it is not reachable by somebody who guessed.

```bash
servlo staging password staging.acme.com
```

That is what to run when somebody who had it should not have it any more. The old one stops working immediately.

The certificate still works. The ACME challenge path is exempted from the password, because the authority cannot be asked for one, and without that exemption a staging site could never be issued a certificate and the failure would read like a DNS problem.

The hash lives in its own file named for the domain, and it is the one credential servlo leaves at 0644: nginx's workers drop to the image's own user before they read it, and a file they cannot open makes the site answer 500 to everybody. A hash rather than a password is what makes that payable. Removing the site removes the file; unlinking a parked one leaves it, because that site is coming back and its vhost still names it.

## Refreshing from live

```bash
servlo staging refresh staging.acme.com
servlo staging refresh staging.acme.com --files-only
servlo staging refresh staging.acme.com --database-only
```

Live's files are written over staging's, and live's database is loaded into staging's own.

**There is no command for the other direction and there is not going to be one.** The refresh takes the staging site and reads its origin out of its own record, so there is no call anywhere with two site names that could be swapped. Every other shape of this has a place where swapping them costs a client their production data, and no confirmation prompt makes that acceptable.

Three things never cross:

- **The `.env`.** Staging has its own database credentials and its own address. Overwriting them with live's would point the staging site at the production database, which is the same accident arriving by a different route.
- **A symlink's target.** Links are recreated, not followed, or a link pointing out of the site would copy the rest of the server into staging.
- **Anything that is only on staging.** A refresh writes over what live has and leaves the rest, the same way a deploy does. Occasionally surprising, and much better than deleting work somebody had not committed.

The refresh is stamped, because the question anybody asks about a staging site is how old it is.

## In the panel

The Staging card is on a site's Settings tab, and it shows both sides of the relationship. On the staging site it is the controls; on the live site it is a list of the copies that exist, which is what somebody about to deploy wants to know.

Refreshing needs an admin account even though the card is on a site-scoped page. It copies the live site's database across, so a Developer given only the staging site could otherwise pull production data onto a site they control, and whether they should see that data is not the staging site's assignment to answer.

## Creating one

```bash
servlo staging create acme.com staging.acme.com --refresh
```

The staging domain is given, never derived. Servlo appends no suffix to anything, and guessing `staging.<the live domain>` would be wrong for everybody whose staging lives somewhere else.

The files land beside the live site by default; `--path` puts them elsewhere. The site is created empty: creating it and putting today's live data on it are two decisions, and the second is the one you will want to repeat. `--refresh` does both at once.

A staging site cannot be the origin of another. The refresh chain that would imply is a copy of a copy, and the question "is this data live" stops having an answer.

## Getting it a certificate

The same as any other site: point DNS at this server, then `servlo secure staging.acme.com`. The password does not get in the way.

Worth thinking about before you do: a certificate for a staging domain is in the public certificate transparency logs, so the address is discoverable whether or not anybody links to it. That is exactly why the password is not optional.
