# Panel authentication

Every request to the panel carries a session or it goes no further. Not "unless it came from loopback", and not "unless the request looks local", because on a server there is no trusted side of the connection to exempt.

Open the panel and it asks who you are. On a fresh install it asks you to create the first account instead, and that account is an administrator, because a panel whose only account cannot administer it is a panel nobody can administer.

**That first account is created from the machine, not over the network.** Everything else the panel does is gated on a session, but claiming it the first time cannot be: there is no account yet to hold one. The panel listens on every interface by design, so a route that took the first account from anywhere would mean that between an install finishing and you opening the dashboard, whoever reached port 7073 first became the administrator of every site, database and file on the server. Open the dashboard on the server itself — `http://servlo.localhost`, or over an SSH tunnel — or create the account from a shell with `servlo users add`, which is the way in when the public address is your only access.

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

The dashboard's live connection is the one thing with no next request: it is authorised when it opens and then streams the panel's state for as long as the browser holds it. So it asks, every thirty seconds, whether its session is still there, and closes itself when the answer is no. Revoking a session takes the open dashboard with it, within one interval, which is what the laptop that is no longer in the building needed it to do.

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

Which address that is takes some care once a panel domain is attached, because then every remote request reaches the panel through Servlo's own nginx and they all share one peer address: nginx's. Counting against that is one bucket for the whole internet, which turns the lockout upside down, since an attacker guessing passwords would lock out every operator rather than themselves. So the address nginx recorded is used instead, but only for requests that arrived over the socket only nginx is on. A request that came straight to the port keeps its peer address and cannot talk Servlo out of it: a header is set by whoever is calling, and believing one from a direct client would let an attacker reset their own budget on every request. The audit log records the same address, so "who signed in from where" answers with a person rather than a proxy.

The lockout holds against the right password too. One that let a correct guess through would only be slowing down an attacker who was going to fail anyway.

The counter lives in the running panel rather than on disk. A restart clears it, which sounds like a weakness and is not: restarting Servlo needs access to the machine, and anyone with that has no reason to be guessing at the login. Persisting it would hand an attacker a disk write per attempt.

## Two-factor authentication

Optional, and off unless you turn it on. A panel behind a long passphrase and a doubling lockout is already hard to guess; this is for when you want a stolen password not to be enough on its own.

From the panel, **System → Two-factor authentication → Turn on**: scan the QR with any authenticator app, type back the code it shows, and write down the ten recovery codes. From a shell:

```bash
servlo users totp enable alice
servlo users totp codes alice     # a fresh set, replacing the old
servlo users totp disable alice
```

Nothing is stored until you type back a matching code, so an app that failed to scan leaves the account exactly as it was.

It is TOTP as RFC 6238 specifies it — SHA-1, six digits, thirty-second step — which is not a choice so much as the only interoperable option. It is what every authenticator app assumes when it scans a QR code, and a stronger digest produces codes no app will generate. The security comes from the second factor existing, not from the hash.

Codes are accepted one step either side of now, and no further. Phone clocks are rarely exact and a code typed as the window turns over should still work; a wider window is a longer life for a code read over your shoulder.

And each code signs in once. RFC 6238 asks for that in as many words, and the reason is the window above: without it a code is good for a minute and a half, against an account whose password whoever read the code already has. Servlo records the step of the last code an account signed in with and refuses anything at or below it, so a code watched being typed, echoed by a phishing page or left in a form somebody logged is spent by the time it is used. The code that proves an enrolment counts as spent too. The next code always works, so this costs nothing but a second's wait in the case where somebody signs in twice in the same half-minute.

### Recovery codes

Ten, each good once, shown exactly when they are made. There is no command to show them again — storing them in a form Servlo could reprint would make them a second password sitting on the same disk as the first.

A recovery code goes in the same box as a code from the app. The panel tries one and then the other, because the person typing it does not have to know which kind it is.

They are stored hashed, with SHA-256 rather than Argon2id. Unlike a password these are uniform randomness with no dictionary to try, so the slow hash would buy nothing and cost 46 MiB per attempt on a box someone can aim attempts at.

### Locked out

Three ways back, in the order you would reach for them:

1. A recovery code, from anywhere.
2. `servlo users totp disable <name>`, which needs a shell but works whatever happened to the phone.
3. `servlo users password <name>`, if the password is the part you lost.

Turning the second factor off takes the secret and the unspent codes with it, so turning it back on enrols the app fresh rather than silently restoring a factor you thought was gone.

### What the login form says

A wrong password and an unknown account answer identically, because telling them apart turns the form into a way to enumerate account names.

In the same time, too. A name servlo does not have is checked against a hash of a password nobody holds, so an attempt that was never going to succeed does the same work as one that might. Without that the reply for an unknown name comes back in microseconds and the reply for a real one takes tens of milliseconds, and a gap that size is not a side channel anybody needs statistics to read: the form would be saying in the clock exactly what it refuses to print.

"That account needs a code" is different, and only ever follows a **correct** password. Saying it after a wrong one would announce that the account exists and has a second factor — the same enumeration by another route.

A missing or wrong code counts against the rate limiter like a wrong password. Without that, the second factor is six digits an attacker can try a million times.

## Accounts

```bash
servlo users                       # who can sign in
servlo users add alice --role admin
servlo users password alice        # also signs that account out everywhere
servlo users role alice developer
servlo users remove bob
```

Two roles. **Admin** runs the server: every site, plus the things that are not a site at all — services, backups, accounts, the panel's own settings. **Developer** works on the sites assigned to them and sees nothing else.

```bash
servlo users sites alice                        # what they work on
servlo users sites alice example.com shop.example.com
```

That is a shell command on purpose. A developer able to widen their own list would make the role advisory rather than enforced, and nothing in the panel reaches account management at all: accounts, roles and assignments are `servlo users` and a shell on the box.

An assignment is a domain, stored as you typed it, and nothing prunes it when the site holding that domain is removed. Listing them marks any that no site on this server currently answers on, and assigning one warns if it matches nothing yet, so a typo and a leftover both show. They are left rather than deleted because a domain can be assigned before the site that will answer on it exists. Worth knowing what that means if you reuse a domain: an old assignment naming it would match the new site too, so review the list when a domain changes hands.

A developer with nothing assigned sees nothing, which is the state an account is in the moment it is made. Reading an empty list as "unrestricted" would make every new developer an admin until somebody noticed.

Assignments take effect on the next request. Narrowing a list is not a reason to sign somebody out mid-deploy, and every route is checked against the live account rather than anything the session remembers.

The dashboard's WebSocket is held to the same list, which matters because it is pushed rather than asked for. Every frame is filtered per connection: the sites list down to what the account may see, the services blanked for anyone who may not administer them, worker health blanked the same way because the route beside it is admin-only, and a notification dropped when it names a site the account cannot open. The one payload sent whole is the machine's own status, because the route that serves it is open to any signed-in account. Gating the routes and leaving the socket alone would mean a developer who can never open another team's site but can watch it fail.

The decision about each of those payloads is written down beside them rather than left to whoever last touched the file, and the build reads it: a payload added to the frame without a narrowing function, or declared and then passed through whole anyway, fails the test suite. The socket is a door, and the way a door stops being locked is somebody adding a window next to it.

### How it is enforced

Every route declares what authority it needs, in one table, and the build fails on a route that declares none — or on a declaration no route registers, which is how a renamed route quietly loses its own.

Five permissions. `public` is reachable without a session: the login routes and what a browser fetches before there is one. `self` is about the signed-in account. `site` acts on one site, named as the first path segment after the route's prefix. `site:list` returns many and filters. `admin` is everything else.

One site permission rather than a read/write pair: with two roles they would be the same check, and a distinction that changes nothing is a distinction to keep in step for no benefit. Splitting it is the work of whichever story introduces a role that reads without writing.

Deny by default. A route this table does not recognise is refused to a developer, so one added without a thought for roles breaks for them loudly rather than working for everyone quietly. The scan turns that into a build failure, so the person adding the route finds out rather than the person using it.

::: warning Log routes are admin for now
`servlo`'s worker and unit log streams are keyed by container or unit name rather than by domain, so their paths carry nothing to check a developer's assignment against. They are `admin` until those routes say which site they are about. Claiming site scope on a path Servlo cannot read a site out of would be a leak dressed as a feature.
:::

The websocket is scoped too, not just the routes. It pushes a snapshot of the sites to every connection, and a developer who cannot open another team's site should not be able to watch it either — gating the routes and leaving the socket open is the door beside the door. A developer's frames carry their own sites and no services at all; filtering in the browser would put the enforcement in the client.

A snapshot Servlo cannot parse is filtered to empty rather than passed through, or a malformed frame becomes the way to see everything.

Changing a password signs that account out everywhere by default. If you are changing it because it may be known, the sessions opened with it may be too. `--keep-sessions` is for the ordinary rotation where nothing is suspected.

The last admin cannot be removed or demoted. That leaves a panel nobody can administer, and undoing it needs a shell on the box.

::: tip Locked out
A shell on the server is the recovery path for all of it:

```bash
servlo users password alice
```
:::

## The audit log

Everything that changed on this machine, and who changed it.

```bash
servlo audit
servlo audit --limit 200
```

It is also a card on the panel's System page, for administrators only: the log records what every account did and from where, which is not a developer's to read.

Each line answers the five questions asked after something goes wrong — when, who, from where, what to, and did it work:

```json
{"at":"2026-08-07T10:42:11Z","action":"site.deploy","subject":"example.com","actor":"alice","ip":"203.0.113.9","result":"ok"}
```

Recording happens in the middleware, not in each handler. Seventy handlers is seventy chances to forget, and the one that forgets is the one somebody later needs. A route added tomorrow is audited the day it is added, without anyone remembering to.

Six routes are the exception, and they record for themselves: signing in, signing out, claiming the panel, and turning the second factor on or off. They run before there is a session for the middleware to read an actor from, or in the case of claiming the panel before there is an account at all, so recording them there would put an empty name on every sign-in, which is the one field those entries exist for. A refused sign-in is recorded too, with the username that was tried. The form still will not say whether a username or a password was wrong, because that would turn it into a way to enumerate accounts, but the log is 0600 and it is allowed to know. Turning the second factor off through the panel writes the same `users.totp.disabled` the CLI writes, so one search finds both doors.

Reads are not recorded. A log with every page view in it is a log nobody reads, and the question an audit answers is what changed. **Refusals are** — somebody reaching for something they may not have is most of why the log exists.

A command run from a shell is attributed to the Unix user running it, with no IP. On a box where one operator has the login and the rest reach the panel, that is exactly the distinction worth recording, and a loopback address would suggest the entry knows something it does not. An entry with no actor at all is Servlo itself, on a timer: a renewal, a self-heal.

### Append-only, and what rotation means

The application appends and never edits. A log a process can rewrite is a log that tells you what the last writer wanted you to believe.

That leaves the disk, which rotation handles: past four megabytes the live file is **renamed**, not emptied, and a fresh one starts. Nothing written is unwritten; it is somewhere else. Five rotations are kept, because rotation that never prunes trades one unbounded file for an unbounded number of them.

Not logrotate. That would put the guarantee in a config file Servlo does not own, on a schedule Servlo cannot see, and an operator who disabled it would get a full disk rather than a rotation.

Reading spans the rotations, so the history does not vanish the moment the log grows past its threshold — which is exactly when it becomes interesting.

Secrets are redacted on the way in. The file is `0600`, and it is also the file an operator pastes into a support thread, so a token must not be in it in the first place. Query strings are never recorded for the same reason.

## What authentication does not grant

Signing in says who you are, not what you may do. Filesystem browsing, raw `.env` reads, database drops and the machine's own controls are admin, and a Developer signing in reaches the sites assigned to them and nothing else.

There used to be a further rule underneath that one: a set of routes no remote client could reach whatever its password, on the reasoning that being at the machine is itself a credential. That belongs to a local development tool. Servlo's panel is reached over the internet by design, so the rule only ever hid the panel from the person who owns it, and it is gone. The permission a route declares is the whole answer, and it is the same answer wherever the request came from.
