# Domains

A site is served on a real domain. Servlo runs no resolver and never touches
this machine's resolver configuration, so making a domain arrive here is DNS you
hold at your registrar — Servlo's part starts once it does.

Every domain on a site is served by the same vhost, is included in the
certificate's SANs, and is checked by the DNS pre-flight before an issuance is
attempted.

## From the command line

```bash
servlo domain list                    # what this site answers to
servlo domain add shop.example.com    # add an alias
servlo domain remove old.example.com  # drop one
```

Each does rather more than edit a list, which is why they are commands rather
than a file you edit by hand. Adding a domain rewrites the vhost, updates the
hosts file every container reads, reloads nginx, and — if the site is already
secured — **reissues the certificate straight away** so the new name is in the
SAN list rather than waiting for the next renewal. Removing one does the same in
reverse. A site grouped as a main site cascades the change to its secondaries.

Four refusals worth meeting here rather than at the prompt:

- **A bare name is refused.** Give the whole domain; there is no suffix to
  complete `shop` with.
- **A domain another site already holds is refused**, whatever its TLS state.
- **The last domain cannot be removed.** A site with no domain is not reachable.
- **Reserved names are refused** — the ones Servlo uses for itself.

Removing the primary promotes the next domain in the list: the vhost file is
renamed and `.env` is resynced so `APP_URL` follows.

## Domain naming

A directory name that looks like a domain is normalised into the site handle: dots become dashes and a trailing TLD is dropped. The handle names units and containers and is not a domain. The domain is never derived, because servlo has no TLD of its own to append, so a link that names no domain and finds none in the project config is refused rather than given an invented one.

For example: `admin.example.com` becomes `admin-example.example.com`

---

## Multiple domains

A site can respond to several domains. The first is its primary: the one that names the vhost file, the certificate files and `APP_URL`. The rest are aliases, and they are equal in every way that matters to a visitor.

```bash
servlo link myapp.example.com
```

After linking, you can add more domains:

```bash
servlo domain add api.example.com
servlo domain add admin.example.com
servlo domain list
#   myapp.example.com (primary)
#   api.example.com
#   admin.example.com
servlo domain remove api.example.com
```

Every domain is a fully qualified name. Servlo appends nothing, so a bare label like `api` is refused rather than completed: the only name that reaches this server is the one DNS points here, and inventing a suffix would produce a site nobody can visit.

Every alias goes into the site's certificate as a SAN and into the DNS pre-flight, so one alias pointing somewhere else blocks the whole issuance rather than producing a certificate that covers some of the site.

That is also why changing the domains on a site that already has a certificate usually cannot reissue on the spot. The ordinary sequence is to add the alias and then point its DNS here, and the pre-flight refuses to issue for a name that does not resolve to this server yet. When that happens the domain change still goes through, and the panel says plainly that the certificate does not cover the new name and what to do about it. It does not fail quietly: a site serving a certificate that does not name a domain it answers to gives every visitor to that domain a browser warning, and the operator has to be able to see why.

You do not have to come back and press anything afterwards. A certificate short of one of its site's domains counts as due for renewal in the same way an expiring one does, so once the DNS moves, the next renewal pass issues a certificate naming it. Until then the site keeps the certificate it has, and the shortfall is on the alert list rather than only in the message you saw when you added the domain.

Point the DNS, then use **Get SSL**.

### Subdomains

A subdomain is a site like any other. Register `admin.example.com` the way you would register anything else: its own directory, its own PHP version, its own settings, its own certificate. It is not a mode of the site at `example.com` and shares nothing with it.

A site answers for the domains it was given and nothing else. A subdomain nobody has registered is not served by its parent: the request falls through to servlo's default vhost and gets "not found", which is the honest answer.

Servlo used to add a `*.example.com` wildcard to every site, so any subdomain reached the parent automatically. That is a convenience worth having on a laptop and wrong on a server. The parent's certificate does not name the subdomain, so a visitor arriving over HTTPS met a browser security warning attached to a site nobody meant to put there, and unlinking a subdomain that did have a site of its own silently handed its traffic back to the parent instead of answering "not found".

If you do want one site to answer for every subdomain, add `*.example.com` as one of its domains. Servlo passes it through untouched. That needs a wildcard certificate, which means [DNS-01](../features/https.md), so it is a decision you make once for the site that wants it rather than one applied to every site by default.

### Picking a canonical domain

A site serving both `example.com` and `www.example.com` is serving the same content at two addresses. That splits its analytics, and a search engine treats the two as separate pages competing with each other.

Open the site, go to **Settings**, and under **Canonical domain** pick which of the two is the real one. The other is permanently redirected to it, keeping the path and the scheme. Leave it on **Serve both** and nothing is written, which is the default.

Both names stay in `server_name` and in the certificate. The redirecting one still has to be answered, over TLS as well, or somebody typing `https://www.example.com` gets a certificate warning instead of a redirect.

The choice is only offered when the site serves a domain and its own www form, because otherwise there is no other host to redirect. It is refused rather than saved if the site could not honour it: the redirect is permanent and browsers cache it, so pointing it at a name nothing answers for is not something an operator can undo by changing their mind.

The redirect deliberately does not apply to `/.well-known/acme-challenge/`. A validation for the redirecting host has to be answerable at that host.

## Redirects

Under **Settings** the site can send visitors somewhere else, at two scales.

**The whole domain.** Give a target and everything on the site goes there, keeping the path and query, so every deep link anybody ever shared still lands somewhere useful. Use it for a domain that has moved. It applies before anything the site or its framework would otherwise serve, and on a secured site it applies from the plain-HTTP block too: the target is an absolute URL, so sending the visitor straight there beats bouncing them through the HTTPS version of a domain that has moved.

**A single address.** Give a path and where it now lives, a path on this site or an absolute URL. Matched exactly, so `/old` does not also capture `/older` or anything under it. On a secured site these live on the HTTPS block only, because a relative redirect issued from port 80 resolves against `http://` and would send the visitor to the new path over plain HTTP before redirecting again.

Both default to a temporary 302. Tick **Permanent** for a 301 when the move is final: browsers cache a 301 and will not ask again, which is the point of it and also why it is not the default.

Two things are refused rather than saved, because a permanent redirect is not something an operator can take back once visitors have it cached: a whole-domain target this site already answers for, and a rule pointing at its own path. Both are loops the browser gives up on, and in the first case the site being looped is the one you would need to reach to undo it.

Whole-domain redirects deliberately do not apply to `/.well-known/acme-challenge/`. A moved domain still has to prove itself to the certificate authority, or it can never renew the certificate it is serving the redirect over.

Domains are stored whole in `.servlo.yaml`:

```yaml
domains:
  - myapp.example.com
  - admin.example.com
```

You can also manage domains from the web UI: click the pencil icon next to the domain in the site header to open the domain management modal. Changing the primary domain there also rewrites `APP_URL` in the project's `.env` to match the new primary, unless you have pinned a custom `app_url` (see [Custom `APP_URL`](#custom-app-url) below).

When a site is secured with HTTPS, the certificate is automatically reissued to cover all domains.

Subdomains (e.g. `anything.myapp.example.com`) are automatically routed to the same site.

To route a subdomain to a **different** site instead (for example a separate admin app at `admin.myapp.example.com`), group the two sites rather than adding an alias. See [Site Groups](site-groups.md).

---

## Domain conflicts

A domain may only be claimed by one site at a time. When `servlo link`, the watcher's auto-registration, or a `.servlo.yaml`-driven re-link tries to register a domain that another site already owns, the conflicting domain is **filtered out** (not the whole site) and a warning is printed:

```
$ servlo link
  [WARN] domain "shared.example.com" already used by site "owner-app", skipped
Linked: clone-app -> clone-app.example.com (PHP 8.5, Node 22, Framework: laravel)
```

The site still gets registered with whatever domains survived the filter. If every requested domain is conflicted, servlo falls back to a freshly generated `<dirname>.<tld>` (with a numeric suffix to avoid name collisions).

`.servlo.yaml` is **never modified** when this happens; the original `domains:` list stays on disk so the conflict is visible to the UI and the entry self-heals on the next link if you remove the owning site. The web UI surfaces filtered domains in two places:

- The site detail header's domain pill shows an amber ⚠️ when one or more declared domains are filtered (`+N more` count includes them). Hovering reveals each conflicted entry with the owning site name.
- The Manage Domains modal lists conflicted entries at the top with a warning icon, the domain struck-through, a `used by <site>` pill, and a small trash button. Clicking the trash removes the entry from `.servlo.yaml` only; the registry, vhost, and certs are untouched.

The conflict check is **strict**: a domain is reserved regardless of TLS scheme. Two sites cannot share the same domain even if one runs HTTPS and the other HTTP; DNS and browser caches don't reliably disambiguate by scheme, and the resulting setup is fragile.
