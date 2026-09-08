# Panel access

The Servlo panel is reachable two ways on a server, and both are meant to be used.

**By address, from the first minute.** `https://<server-ip>:7073` works before any DNS exists, over a certificate Servlo signs itself. Your browser will warn about it once.

**By domain, once one points here.** `https://panel.example.com`, served through nginx with a real certificate from Let's Encrypt, issued by the same flow every site uses.

Whichever you use, the panel asks who you are before it shows you anything. See [Panel authentication](/features/panel-authentication).

A third URL, `http://servlo.localhost`, works from a shell on the server itself. RFC 6761 makes `.localhost` resolve to the visiting device's own loopback, so it is unreachable from anywhere else, which is exactly why the local dashboard uses it.

---

## Before DNS: the self-signed certificate

Upstream served the dashboard over plain HTTP, which is right for a laptop reaching `127.0.0.1` and wrong for a droplet. On a server the wire between you and the panel is the internet, and the first thing you do over it is type a password.

So the panel speaks TLS from the start. With no domain yet there is no certificate authority that will issue for a bare IP address, so Servlo signs one itself covering the server's addresses plus loopback.

That certificate is **not trusted, and is not meant to be**. Nothing is installed into any trust store, on the server or on your machine. What it buys you is that the login does not cross the internet in clear text while DNS propagates. Your browser will show an interstitial the first time; accept it once, or skip ahead and attach a domain.

Two details worth knowing:

- **Typing `http://` works.** The listener reads the first byte of each connection. A TLS handshake starts with `0x16`; anything else is answered with a redirect to the same URL over `https`, path and query intact. One port, no second listener to open in the firewall.
- **The certificate is reused, not regenerated.** Regenerating on every restart would change the fingerprint you just accepted, which trains you to click through warnings. It is replaced only when the server's addresses change, when a domain is attached, or when it nears expiry.

## Attaching a domain

Point an A record (and AAAA if you have one) at the server, then:

```bash
servlo panel domain set panel.example.com
```

That writes an nginx vhost immediately, on port 80 only. It cannot name a certificate that has not been issued yet: nginx refuses to start when a `ssl_certificate` file is missing, and a panel misconfiguration that stops nginx takes every site on the box down with it.

The command tells you whether DNS already points here. Once it does:

```bash
servlo panel domain secure
servlo restart
```

That issues through the ordinary ACME path, into the same certificate directory sites use, so the renewal scanner picks the panel up without being told about it. The vhost is rewritten to listen on 443, redirect from 80, and carry the same TLS defaults, HSTS and OCSP handling every secured site gets.

Until the certificate exists, the panel keeps answering on port 7073 with its self-signed certificate. Nothing goes offline in between.

```bash
servlo panel                    # which of the two paths is live
servlo panel domain remove      # stop serving on the domain
```

Removing a domain leaves its certificate on disk. Removing one is usually a step in moving it, and re-issuing costs a Let's Encrypt rate-limit slot that deleting a working certificate throws away.

## What the domain does not grant

Servlo treats requests arriving over its own unix socket as local. That is correct for `servlo.localhost`, which no remote browser can reach.

A panel domain proxies into that same socket, and it *is* reachable from anywhere. So the vhost marks every request it forwards with a header saying so, and the panel refuses local trust to anything carrying it. The header is set with nginx's `proxy_set_header`, which overwrites whatever the client sent, so it cannot be stripped from outside; and it only ever *removes* trust, so a client that sets it directly is denying itself and nobody else.

Be clear about what being local does and does not decide, because this page used to say otherwise. It is **not** what separates what you may do: an admin signed in over the panel domain has an admin's authority, terminal and file manager included. Reserving those to somebody sitting at the droplet is the rule S5.5 threw out, on the grounds that a panel reached over the internet by design cannot hide its own surface from the only person who is ever going to use it. What decides authority is your role and the permission each route declares, and those answer the same wherever the request came from.

What local trust still decides is the short list of things that cannot be gated on a session at all: the profiler endpoint, the cross-process notifier the CLI pokes, and creating the very first account on a panel that has none. That last one matters most, because until it exists there is nobody to authorise it — see [Panel authentication](./panel-authentication).

The panel vhost also refuses to be framed (`X-Frame-Options: DENY` and `frame-ancestors 'none'`). A clickjacked panel is a clickjacked delete button.

::: tip Reaching a database instead
Databases and caches never leave loopback, whatever the panel is doing. To reach one from your laptop, tunnel it:

```bash
ssh -L 3306:127.0.0.1:3306 you@your-droplet
```

See [Production mode](/features/production-mode).
:::
