# HTTPS / TLS

Servlo uses [mkcert](https://github.com/FiloSottile/mkcert), a locally-trusted CA that your browser will accept without warnings.

> [!NOTE]
> HTTPS uses a locally issued certificate. S3.1 replaces this with ACME.

```bash
cd ~/Servlo/my-app
servlo secure
# Issues a cert for my-app.test, regenerates the SSL vhost, reloads nginx
# Updates APP_URL=https://my-app.test in .env if it exists
# Updates secured: true in .servlo.yaml if it exists
# Visit https://my-app.test with no certificate warning

servlo unsecure
# Removes the cert, switches back to HTTP vhost
# Updates APP_URL=http://my-app.test in .env if it exists
# Updates secured: false in .servlo.yaml if it exists
```

HTTPS can also be enabled during `servlo init` or `servlo setup`, the wizard asks the question upfront and applies it as part of the configuration step.

Certificates are stored in `~/.local/share/servlo/certs/sites/`.

---

## Browser trust (certutil / nss-tools)

mkcert installs the local CA into two places: the system trust store, used by curl, PHP, wget and openssl, and the browser NSS databases used by Firefox and Chrome/Chromium. Writing to the browser stores needs `certutil`, which ships in `nss-tools`. When `certutil` is missing mkcert still trusts the system store, so command-line tools accept `.test` HTTPS, but browsers show a certificate warning. `servlo doctor` reports this under "browser HTTPS trust", and `servlo install` / `servlo dns:enable` print a note when it applies.

That check looks in the stores themselves rather than settling for `certutil` being installed, since the two can disagree: a CA that never reached a browser, or one that was removed from a profile afterwards, leaves every site warning on a machine that otherwise looks healthy. The certificate found in each store is compared against the CA servlo signs with today, so a CA regenerated after a profile imported the old one is reported rather than accepted for carrying the same name. Any store that comes up short is named in the warning. A machine with no browser store at all has nothing to check and passes.

On ordinary distributions install nss-tools and reinstall the CA:

```bash
sudo dnf install nss-tools        # Fedora
sudo apt install libnss3-tools    # Debian / Ubuntu
sudo pacman -S nss                # Arch
```

On atomic images (Fedora Silverblue, Bazzite, Kinoite, CoreOS) the package has to be layered and the machine rebooted first:

```bash
rpm-ostree install nss-tools
systemctl reboot
```

If you would rather not install a package at all, serve your sites over plain http, which needs no certificate.

> [!NOTE]
> On atomic desktops a Flatpak Firefox or Chrome keeps its own trust store inside the sandbox, which mkcert cannot reach even once certutil is installed. A native (non-Flatpak) browser, or Chrome using the shared `~/.pki/nssdb`, picks up the CA normally.

---

## Automatic renewal

mkcert issues each leaf certificate with a lifetime of a little over two years. Servlo renews a secured site's certificate on its own before it lapses: whenever a certificate is within roughly 30 days of its `NotAfter` (or has already expired, gone missing, or been corrupted), the next ordinary `servlo start` or watcher pass reissues it in place. A still-valid certificate comfortably clear of that window is left untouched, so the renewal check is cheap and silent. `servlo status` continues to surface the same 30-day expiry warning under `[TLS Certificates]`, but you no longer need to act on it manually; a long-lived site that just keeps running self-heals its own certificate.

To reset the clock on demand, without toggling HTTPS off and on, run:

```bash
cd ~/Servlo/my-app
servlo secure --renew
# Reissues the certificate for my-app.test, reloads nginx
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

1. `servlo install` generates a local CA with mkcert and installs it into the system trust store (NSS databases for Chrome/Firefox, and the system root store).
2. `servlo secure <site>` issues a certificate signed by that CA for `<site>.test` **and** `*.<site>.test` (wildcard), so subdomains are covered too.
3. The nginx vhost is regenerated to listen on port 443 with the new cert, and port 80 redirects to HTTPS (302, not 301, so the redirect is not cached by browsers).
4. `APP_URL` in the project's `.env` is updated to `https://`.
5. If a `servlo stripe:listen` service is active for the site, it is restarted with the updated forwarding URL.
