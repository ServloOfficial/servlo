# HTTPS / TLS

Servlo issues certificates through a certificate issuer, an interface with one job: mint a certificate and key for a set of domains. Everything around it is issuer-agnostic, so switching issuers changes nothing about renewal, the atomic swap or the nginx reload.

The issuer that ships is Let's Encrypt over HTTP-01. Servlo asks the authority for a certificate, the authority fetches a token from your site over port 80 to prove you control the domain, and the certificate comes back.

The domain has to resolve to this server before any of that can work, so Servlo checks first and refuses if it does not.

---

## The DNS gate

Both the panel and `servlo secure` resolve A and AAAA for the primary domain and every alias, and compare them against this server's public addresses. Until they match, the padlock in the site header is disabled and its tooltip is the actual mismatch:

> Waiting for DNS — example.com currently resolves to 1.2.3.4, this server is 5.6.7.8

This is not politeness. Let's Encrypt locks an account out of retrying a domain after five failed validations, so clicking against a record that has not propagated costs an hour of waiting to learn what a DNS lookup answers instantly.

Three details worth knowing:

- **Every record has to point here, not just one.** A stale address left alongside the new one makes validation a coin flip, because the authority connects to whichever it picks. A renewal that fails half the time is harder to diagnose than one that never runs.
- **AAAA counts.** Let's Encrypt prefers IPv6 when a AAAA record exists, so a site whose A record is correct and whose AAAA points at an old host fails validation while looking perfectly healthy over IPv4.
- **A secured site is never gated.** Turning HTTPS *off* is exactly what you need when a domain has moved away, so the check only stands between an unsecured site and its first certificate.

### When the public address is not on an interface

Servlo reads this server's address from its own interfaces, which is right for an ordinary droplet where the public address is bound directly. Behind a cloud load balancer, a floating IP, or NAT with a port forward, it is not: the address the authority connects to is nowhere on the machine, and reading interfaces alone would refuse every certificate for a reason that is not true.

Say what it is instead:

```yaml
certs:
  server_addresses:
    - 203.0.113.10
```

`servlo doctor` warns when no public address can be found and none is declared.

```bash
cd /srv/my-app
servlo secure
# Issues a cert for the site's domains, regenerates the SSL vhost, reloads nginx
# Updates APP_URL=https://example.com in .env if it exists
# Updates secured: true in .servlo.yaml if it exists

servlo unsecure
# Removes the cert, switches back to HTTP vhost
# Updates APP_URL=http://example.com in .env if it exists
# Updates secured: false in .servlo.yaml if it exists
```

HTTPS can also be enabled during `servlo init` or `servlo setup`, the wizard asks the question upfront and applies it as part of the configuration step.

Certificates are stored in `~/.local/share/servlo/certs/sites/`. Private keys are written `0600` and Servlo enforces that mode on every issuance rather than trusting the issuer to have got it right.

---

## The challenge path

Every vhost Servlo writes, HTTP and HTTPS alike, answers `/.well-known/acme-challenge/`. The location uses nginx's `^~` prefix match so it wins over any regex location a framework declares, and on a secured site it sits ahead of the HTTPS redirect: a renewal request that got bounced to port 443 would be a renewal that quietly stops working the day the certificate needs it most.

The tokens themselves live in `~/.local/share/servlo/acme-challenge/`, bind-mounted read-only into the nginx container. They are written just before validation and removed straight after, whether it succeeded or not.

---

## Staging first

Let's Encrypt's production rate limits are strict: five failed validations for the same domain locks you out of retrying it for an hour, and there are weekly caps on top. A DNS record that has not propagated yet burns those attempts for nothing.

Staging issues from an untrusted root, so browsers reject the certificate, but the limits are generous enough to work a problem out:

```bash
servlo secure --staging
# ... fix DNS, retry until it issues ...
servlo secure --staging=false
```

The flag records the authority for the whole install rather than overriding one run. That is deliberate: if issuance used staging and renewal used production, the certificate you just tested with would be silently replaced at the 30-day mark, spending the production quota the staging run existed to protect.

---

## Contact email

```yaml
certs:
  email: ops@example.com
```

Optional, and the only way the authority can tell you a renewal has been failing before the certificate actually lapses. `servlo doctor` warns when it is unset.

---

## A private ACME server

`certs.directory_url` points Servlo at any RFC 8555 authority. An explicit directory wins over the staging flag, so setting one does not need the flag turned off first.

---

## What a secured site is served with

Every secured vhost carries the same TLS defaults. Without them a vhost inherits whatever the nginx image happens to default to, which for images still in circulation has included TLS 1.0 and 1.1.

```nginx
ssl_protocols TLSv1.2 TLSv1.3;
ssl_ciphers <Mozilla intermediate>;
ssl_prefer_server_ciphers off;
ssl_session_cache shared:SSL:10m;
ssl_session_timeout 1d;
ssl_session_tickets off;
```

Three of those are choices rather than boilerplate:

**`ssl_prefer_server_ciphers off`** lets the client choose. The old advice was to impose the server's order; the current advice is the opposite, because a phone without AES hardware is faster and no less safe on ChaCha20 and is the only party that knows which it is.

**`ssl_session_tickets off`** because nginx reuses one ticket key for the life of the process. Anyone who later obtains that key can decrypt every session recorded since it was created, which throws away exactly the forward secrecy the cipher list was chosen for.

**The cipher list** is Mozilla's intermediate set: forward secrecy on every suite, AES-GCM and ChaCha20 only, nothing with CBC, RC4, 3DES or MD5. TLS 1.3 ignores it entirely, since its suites are fixed by the protocol.

### OCSP stapling

Emitted only when the certificate actually names a responder, with `ssl_stapling_verify on` so nginx checks what it staples.

Let's Encrypt has retired OCSP in favour of CRLs, and its certificates no longer carry a responder URL. Turning stapling on against one makes nginx warn on every reload and staple nothing, so Servlo reads the certificate rather than assuming: a site on an authority that still publishes OCSP gets stapling, and one that does not gets a clean config instead of a warning nobody can act on.

### HSTS and the redirect

A secured site redirects port 80 to 443 with a **301** and sends `Strict-Transport-Security` with `always` set, so the header goes out on error responses too — the ones an attacker can most easily provoke.

Two things it deliberately does **not** carry. `includeSubDomains` would extend the policy to every subdomain including a group secondary you left on plain http on purpose, breaking it in every browser that had seen the parent. `preload` is effectively irreversible and is not a default anyone can consent to on your behalf.

The lifetime is a year by default and configurable:

```yaml
certs:
  hsts_max_age: 31536000   # 0 omits the header entirely
```

> [!WARNING]
> HSTS is sticky. Once a browser has seen the header it will refuse plain http for that host until the max-age runs out, and `servlo unsecure` cannot reach into browsers that already cached it. This is what HSTS is for, but it does mean turning HTTPS on is a decision with a tail. Set `hsts_max_age: 0` if you do not want that commitment.

---

## Wildcards, and the DNS-01 challenge

HTTP-01 proves control by serving a file, so the authority has to reach this server at that exact name. There is no single name to fetch for `*.example.com`, which is why a wildcard cannot be proved that way at all. Asking for one over HTTP-01 is refused up front rather than forty seconds later by the authority, because a rejected validation counts against the rate limit.

DNS-01 proves control by publishing a TXT record instead. That covers wildcards, and it works on a server the authority could never connect to.

The cost is the credential. A DNS API token can create and delete any record in the zone, which for most operators means the whole domain: mail, subdomains, and the ability to obtain a certificate for any of them. It is a bigger secret than a database password. Scope it as narrowly as the provider allows — Servlo only ever creates and deletes TXT records under `_acme-challenge`.

```bash
servlo dns-provider set cloudflare --token <token>
servlo dns-provider set digitalocean --token <token>
servlo dns-provider set route53 --access-key-id <id> --secret-access-key <secret>

servlo dns-provider use cloudflare      # prove control over DNS-01 from now on
servlo dns-provider use http-01         # back to serving a file
servlo dns-provider list                # shows which are configured, redacted
```

Credentials live in `dns-providers.yaml` under Servlo's config directory, created `0600` inside a `0700` directory, and never inside a site tree — a site tree is served by nginx, cloned from git and readable by the app that runs there, so a token in one is a single misconfigured location block from being downloadable. `servlo dns-provider list` prints only the last four characters.

Storing a credential and using it are separate commands on purpose. An operator may hold a token for one wildcard site and leave everything else on HTTP-01, which needs no credential at all.

### The wildcard record

`*.example.com` and `example.com` are proved at the same record name, `_acme-challenge.example.com`, and each carries its own value. Both have to be present at once: publishing the second as a replacement withdraws the proof of the first and fails half the order. Servlo adds rather than replaces, and withdraws only the value it published, so a second order in flight for the same name keeps its own.

After publishing, Servlo waits before telling the authority to look. A registrar answers its own API immediately and its nameservers a moment later, and an authority that checks too early records a failed validation that counts against the limit. The wait is paid once for the whole order rather than once per domain.

### Why no AWS SDK

Route53 is signed with SigV4 rather than bearer-authenticated, which is why it needs its own code path. It is signed by hand here: the AWS SDK would bring dozens of modules into a binary that needs exactly two API calls, and the algorithm is about a hundred lines that keep the whole provider auditable in one file.

---

## Automatic renewal

Servlo renews a secured site's certificate on its own before it lapses: whenever a certificate is within roughly 30 days of its `NotAfter` (or has already expired, gone missing, or been corrupted), the next ordinary `servlo start` or watcher pass reissues it in place. A still-valid certificate comfortably clear of that window is left untouched, so the renewal check is cheap and silent. `servlo status` continues to surface the same 30-day expiry warning under `[TLS Certificates]`, but you no longer need to act on it manually; a long-lived site that just keeps running self-heals its own certificate.

A failed renewal never costs you the certificate you already have. The new certificate and key are written to temporary paths and renamed into place, with the previous certificate copied aside first so the live path always holds a complete certificate, even for the instant between the two renames. If the key rename fails the previous certificate is rolled back, because a new certificate paired with an old key is worse than a stale certificate: nginx refuses to start the site at all.

### When renewal fails

The dangerous shape is not a certificate that expires. It is a renewal that starts failing while the certificate still has a month on it: everything keeps working, nobody is told, and thirty days later the site goes down for a reason that stopped being visible a month earlier.

So a failure is loud from the first attempt. It is written to the append-only audit log at `~/.local/share/servlo/audit.log`, kept until an issuance actually succeeds, and shown in the dashboard and by `servlo doctor`:

```
✗ certificate renewal for example.com
  failing since 2026-03-14: the authority could not validate this domain
  fix the cause, then: servlo secure --renew example.com
```

The record keeps the time of the **first** failure rather than the most recent one. A renewal that has been failing for three weeks is a different problem from one that failed once this morning, and resetting the clock on every attempt hides which you are looking at. A successful issuance clears it, because an alarm that outlives its problem is one operators learn to ignore.

Separately, `servlo doctor` checks what is actually on disk. A machine restored from a backup, or one whose panel has never run, has no failure records and can still be serving something expired. An expired or missing certificate is reported as a failure; one inside the renewal window is a warning, since the self-heal still has weeks of attempts left.

To reset the clock on demand, without toggling HTTPS off and on, run:

```bash
cd /srv/my-app
servlo secure --renew
# Reissues the certificate for the site's domains, reloads nginx
```

`servlo secure --renew` only applies to already-secured sites; on an HTTP site it tells you to run `servlo secure` first.

---

## From the Web UI

The Sites tab has an HTTPS toggle per site; clicking it runs `servlo secure` or `servlo unsecure` inline and updates the vhost without touching the terminal. If `.servlo.yaml` exists in the project, the `secured` field is updated there too so the state is preserved for future `servlo init` runs.

---

## Stripe listener

If a Stripe webhook listener is running for the site, toggling HTTPS automatically restarts it so `--forward-to` points at the correct `http://` or `https://` URL. No manual intervention required.

---

## How it works

1. `servlo secure <site>` asks the active issuer for a certificate covering the site's primary domain and every alias. What SANs that implies is the issuer's decision: a CA can mint wildcards for free, an ACME authority cannot over HTTP-01.
2. The issuer registers an ACME account on first use, keyed to the authority so staging and production never share one, and stores the key `0600` under `~/.local/share/servlo/acme/`. It publishes a token per domain, waits for validation, then sends a CSR and receives the chain.
3. The certificate and key are swapped into place atomically, keeping a complete certificate at the live path at every instant. A fresh key is generated for every issuance rather than reused: a renewal that keeps the old key gains nothing and means one compromise covers every certificate the site has ever had.
4. The nginx vhost is regenerated to listen on port 443 with the new cert, port 80 redirects permanently to HTTPS, and the HSTS header is added.
5. `APP_URL` in the project's `.env` is updated to `https://`.
6. If a `servlo stripe:listen` service is active for the site, it is restarted with the updated forwarding URL.
