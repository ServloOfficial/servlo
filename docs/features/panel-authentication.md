# Panel authentication

Every request to the panel carries a session or it goes no further. Not "unless it came from loopback", and not "unless the LAN is exposed" — on a server there is no trusted side of the connection to exempt.

Open the panel and it asks who you are. On a fresh install it asks you to create the first account instead, and that account is an administrator, because a panel whose only account cannot administer it is a panel nobody can administer.

---

## What replaced what

Servlo inherited a model built for a laptop: loopback was trusted absolutely, and a client from elsewhere presented HTTP Basic credentials. Both halves are wrong on a droplet.

A request from `127.0.0.1` is a reverse proxy, a container, or anything else that reached the port — not the operator sitting at the machine. And Basic auth sends the password on every request, with no way to sign out and no way to see who is signed in.

| | Before | Now |
|---|---|---|
| Password hashing | bcrypt | Argon2id |
| Credentials on the wire | every request | once, at sign-in |
| Sign out | change the password | ends that one session |
| See who is signed in | not possible | `servlo sessions list` |
| Loopback | trusted absolutely | authenticates like anything else |

Passwords already stored keep working. The first time one is used it is quietly rehashed with Argon2id, so an upgrade never locks you out and the inherited hash is used exactly once more.

## Passwords

Argon2id, because bcrypt's work factor is bounded by a 72-byte input and a cost that buys little against a GPU, while Argon2id's memory cost is the part an attacker cannot cheaply buy around.

The parameters travel with each hash, so raising them later needs no migration: every existing password stays verifiable against the values it was made with, and gets a stronger hash the next time you sign in.

The floor is twelve characters, and it is a length rather than a character-class rule. Requiring a symbol produces `Password1!`; requiring length produces a passphrase, and length is what actually costs an attacker.

## Sessions

A session is a record on the server, not a signed cookie. That is what makes the next two commands possible at all: a cookie that authenticates by its own signature cannot be listed and cannot be ended without changing the password, which ends every session on every device at once.

```bash
servlo sessions list
servlo sessions revoke <id>
servlo sessions revoke --all
```

The store holds the SHA-256 of each token and never the token itself, so reading the file is not the same as holding every live session. The panel and the CLI both read through to it, so a session ended from a shell stops working on the next request rather than at the next restart.

Sessions last a week from last use. Working all day pushes the expiry out; a laptop left in a hotel eventually stops being a way in.

## The cookie

`HttpOnly`, so a script cannot read it. `Secure`, so it never crosses a plain connection. `SameSite=Strict`, so a browser will not attach it to a request another origin started.

It also carries the `__Host-` prefix, which is a promise the browser enforces: a cookie with it must be `Secure`, must be path `/`, and must carry no `Domain`. On a panel sharing a parent domain with the sites it hosts, that is the difference between a site's own JavaScript being able to plant a session cookie and not.

## CSRF

`SameSite=Strict` is most of the defence. The rest is a token bound to the session, required on every state-changing request, so one that somehow arrives with the cookie attached still has to prove it came from a page Servlo rendered.

Per session, not per install: a site-wide token is one an attacker fetches for themselves and then replays against everyone. The key lives in the running panel and nowhere else, so restarting invalidates outstanding tokens — a page reload for you, a dead end for anything replaying one.

Reads do not need a token. A forged `GET` returns data to Servlo, not to whoever forged it.

## Rate limiting

The first few failures are free, because mistyping a passphrase is the common case and locking you out for it is its own kind of outage. After that the wait doubles, up to fifteen minutes.

Doubling rather than a fixed delay, because a fixed delay is a rate an attacker plans around: at one attempt a second a dictionary still finishes overnight. And per source address, or anyone could deny you your own panel by guessing badly from somewhere else.

The lockout holds against the right password too. One that let a correct guess through would only be slowing down an attacker who was going to fail anyway.

The counter lives in the running panel rather than on disk. A restart clears it, which sounds like a weakness and is not: restarting Servlo needs access to the machine, and anyone with that has no reason to be guessing at the login. Persisting it would hand an attacker a disk write per attempt.

## Accounts

```bash
servlo users                       # who can sign in
servlo users add alice --role admin
servlo users password alice        # also signs that account out everywhere
servlo users role alice developer
servlo users remove bob
```

Two roles. **Admin** can do everything. **Developer** reaches only the sites assigned to them — the role is recorded now and enforced in a later story, so an account created today needs no migrating when it lands.

Changing a password signs that account out everywhere by default. If you are changing it because it may be known, the sessions opened with it may be too. `--keep-sessions` is for the ordinary rotation where nothing is suspected.

The last admin cannot be removed or demoted. That leaves a panel nobody can administer, and undoing it needs a shell on the box.

::: tip Locked out
A shell on the server is the recovery path for all of it:

```bash
servlo users password alice
```
:::

## What authentication does not grant

Signing in is not the same as sitting at the machine. A terminal on the host, filesystem browsing, raw `.env` reads and database drops stay with the local dashboard, or with a remote session explicitly opted in with `servlo remote-control full-access on`.

A password alone should not open a shell on the server, whoever is holding it. A later story replaces this with a permission declared per route and checked against the signed-in account's role.
