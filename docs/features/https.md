# HTTPS / TLS

Servlo issues certificates through a certificate issuer, an interface with one job: mint a certificate and key for a set of domains. Everything around it is issuer-agnostic, so switching issuers changes nothing about renewal, the atomic swap or the nginx reload.

> [!IMPORTANT]
> There is no issuer wired up yet. `servlo secure` refuses and tells you so. ACME (Let's Encrypt) arrives in S3.2, and until then a site is served over plain http.
>
> The local CA Servlo inherited from upstream is gone. A locally trusted CA is meaningless on a real domain: only the machine that generated it trusts it, so every visitor gets a warning. Servlo does not fall back to a self-signed certificate either, because to a browser a self-signed certificate on a real domain is indistinguishable from someone intercepting the connection, and shipping one would teach you to click through the warning that exists to protect you.

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
2. The certificate and key are swapped into place atomically, keeping a complete certificate at the live path at every instant.
3. The nginx vhost is regenerated to listen on port 443 with the new cert, and port 80 redirects to HTTPS (302, not 301, so the redirect is not cached by browsers).
4. `APP_URL` in the project's `.env` is updated to `https://`.
5. If a `servlo stripe:listen` service is active for the site, it is restarted with the updated forwarding URL.
