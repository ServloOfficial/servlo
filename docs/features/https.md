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

## HSTS and the redirect

A secured site redirects port 80 to 443 with a **301** and sends:

```
Strict-Transport-Security: max-age=31536000
```

`always` is set, so the header goes out on error responses too, which are the ones an attacker can most easily provoke.

Two things it deliberately does **not** carry. `includeSubDomains` would extend the policy to every subdomain including a group secondary you left on plain http on purpose, breaking it in every browser that had seen the parent. `preload` is effectively irreversible and is not a default anyone can consent to on your behalf.

> [!WARNING]
> HSTS is sticky. Once a browser has seen the header it will refuse plain http for that host for a year, and `servlo unsecure` cannot reach into browsers that already cached it. This is what HSTS is for, but it does mean turning HTTPS on is a decision with a tail.

---

## Automatic renewal

Servlo renews a secured site's certificate on its own before it lapses: whenever a certificate is within roughly 30 days of its `NotAfter` (or has already expired, gone missing, or been corrupted), the next ordinary `servlo start` or watcher pass reissues it in place. A still-valid certificate comfortably clear of that window is left untouched, so the renewal check is cheap and silent. `servlo status` continues to surface the same 30-day expiry warning under `[TLS Certificates]`, but you no longer need to act on it manually; a long-lived site that just keeps running self-heals its own certificate.

A failed renewal never costs you the certificate you already have. The new certificate and key are written to temporary paths and renamed into place, with the previous certificate copied aside first so the live path always holds a complete certificate, even for the instant between the two renames. If the key rename fails the previous certificate is rolled back, because a new certificate paired with an old key is worse than a stale certificate: nginx refuses to start the site at all.

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
