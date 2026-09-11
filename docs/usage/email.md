# Email

Servlo runs no mail server and never will. There is no queue to drain, no reputation to nurse, no port 25 listening on the droplet. What Servlo does is hold the SMTP account somebody else runs, and put it where it is needed.

There are two of those accounts, and they are separate on purpose:

- **Per site.** The account that site's application sends its own mail through: password resets, receipts, whatever the framework's mailer emits. Configured under a site's **Settings** tab, written into the site's env file.
- **The panel's own.** The account Servlo sends *its* alerts through: a certificate renewal that started failing, and the other alerts as they land. Configured under **System → Mail**.

Keeping them apart is not tidiness. A renewal alert that arrives from a client's verified domain is the wrong sender, and a client's provider is usually not the one you want your own alerts on.

---

## Per-site SMTP

Open a site, go to **Settings**, and fill in the **Outgoing mail** card:

| Field | What it is |
|---|---|
| SMTP server | The provider's hostname, e.g. `smtp.postmarkapp.com` |
| Port | 587 for STARTTLS, 465 for implicit TLS, 25 for an unencrypted internal relay |
| Encryption | STARTTLS, TLS, or none. Pick the one that matches the port |
| Username / Password | The provider's credential. Leave the username blank for a relay that authenticates by IP |
| Sender address | Where mail comes from. Most providers only accept an address they have verified |
| Sender name | The display name beside it. Optional |

Saving does two things. It stores the account in `~/.config/servlo/smtp.json`, which is `0600` and holds nothing else, and it writes the settings into the site's env file. The card names the keys it is about to write before you press anything.

**Send test email** actually sends. Not a connection check, not a DNS lookup: one real message, through the real account, to whichever address you give it. When it fails you get the mail server's own reply, because `550 5.7.1 Sender address not verified` tells you what to fix and "could not send" does not. The whole exchange is bounded by a timeout, so a provider that accepts the connection and then goes quiet fails in seconds rather than hanging the panel.

**Remove these settings** forgets the password. Whatever was already written into the env file stays there until it is changed: emptying it would leave the application with no transport at all, mid-request.

### Which keys get written

That is the framework's business, not Servlo's, and it is declared in the framework definition alongside the database keys. A Laravel site gets `MAIL_HOST`, `MAIL_PORT`, `MAIL_USERNAME`, `MAIL_PASSWORD`, `MAIL_ENCRYPTION` and the two `MAIL_FROM_*` keys in `.env`. A Symfony site gets one `MAILER_DSN`. A WordPress site gets `SMTP_HOST` and friends as constants in `wp-config.php`, which is what every SMTP plugin reads. A CodeIgniter site gets `email.SMTPHost` and the rest of its dotted paths.

A framework definition declares them under `env.smtp`, with placeholders Servlo fills from the site's account:

```yaml
env:
  file: .env
  format: dotenv
  smtp:
    vars:
      - MAIL_MAILER=smtp
      - MAIL_HOST={{smtp_host}}
      - MAIL_PORT={{smtp_port}}
      - MAIL_USERNAME={{smtp_user}}
      - MAIL_PASSWORD={{smtp_password}}
      - MAIL_ENCRYPTION={{smtp_encryption}}
      - MAIL_FROM_ADDRESS={{smtp_from_address}}
      - MAIL_FROM_NAME="{{smtp_from_name}}"
```

The placeholders:

| Placeholder | Value |
|---|---|
| `{{smtp_host}}` | The server hostname |
| `{{smtp_port}}` | The port |
| `{{smtp_user}}` / `{{smtp_password}}` | The credential, verbatim |
| `{{smtp_user_urlencoded}}` / `{{smtp_password_urlencoded}}` | The same, URL-encoded, for a definition that builds a DSN. A password with an `@` in it truncates the host out of an unencoded `smtp://user:pass@host` |
| `{{smtp_encryption}}` | `tls`, or the literal `null` when encryption is off. Laravel's spelling |
| `{{smtp_crypto}}` | `tls`, or empty. The bare spelling, for CodeIgniter and the WordPress plugins |
| `{{smtp_tls}}` | `true` or `false`, for a DSN query parameter |

A definition writes `KEY={{placeholder}}` and nothing more: the quoting is servlo's, not the definition's. A `.env` value carrying a space, a tab or a `#` is quoted on the way in, because phpdotenv refuses the whole file over the first two and truncates the value at the third, and a from name is a company's while an application password from Google is four groups with spaces between them. A value going into a PHP config file has its quotes and backslashes escaped for the same reason: a password with an apostrophe in it would otherwise end the string and leave the file unparseable. Nothing is quoted that does not need it, so an ordinary host, port or generated password is written exactly as it always was.

Adding mail support to a framework is a YAML change to its definition and nothing else. A framework whose mail settings do not live in an env file at all, as Magento's do not, declares no `smtp` block, and the card says so rather than writing keys the application will ignore.

---

## Panel SMTP

**System → Mail**, the same seven fields. This one writes no file: it is the account Servlo itself sends through.

Alerts go to the account's **sender address**. There is deliberately no separate destination field, because two addresses are two things that can disagree, and an alert delivered to the wrong one is the failure the alert existed to prevent. Point the sender address at the inbox you actually read.

Right now one thing sends: a failing certificate renewal. Servlo emails you the first time issuance for a domain starts failing, with the reason the authority gave. The first time only, not once an hour for three weeks; the dashboard banner and the audit log carry the repetition. A success clears the record, so the next failure is news again.

With no panel account configured, alerts stay in the panel and the audit log, which is where they were before. That is the ordinary state of a fresh install, not a fault.

---

## What is stored, and where

Both accounts live in `~/.config/servlo/smtp.json`, mode `0600`, and that file holds nothing but SMTP settings. The panel never sends a password back to the browser: it sends "a password is stored", which is what the form needs to offer *leave blank to keep the current one*. Saving a site's settings also fixes its env file at `0600`, since Servlo has just written a credential into it.
