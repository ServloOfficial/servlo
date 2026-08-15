# BRANDING.md — Servlo's visual identity

Servlo has never had one. It inherited the upstream project's, the rename swept
the strings and left the pixels, and the pixels are the part people see. This
records what was inherited, what replaced it, and why.

## What was actually inherited

**The mark was a letter L.** `docs/public/assets/logo.svg` is a red rounded
square with a white "L" set in Nunito Bold, 22KB of it because the whole font is
embedded to render one glyph. It is still the upstream logo, unchanged, on the
docs site.

**The brand colour is Laravel's.** `--color-servlo-red: #ff2d20` is Laravel's
brand red exactly. The project this was forked from was a Laravel-focused local
development tool, so borrowing it made sense there. Here it is wrong twice over: it is another
project's identity, and Servlo's second design law is that nothing may be
framework-specific. A panel that runs WordPress, Symfony, Joomla and Grav should
not wear one framework's colours.

**Red is doing four jobs at once.** Look at any screenshot of the System page:
red is the brand, the active navigation item, the Autostart toggle when it is
**on**, and the colour of error text. A toggle that turns red to mean "enabled"
is backwards in a product whose whole job is telling you when something has
broken. This is the strongest argument for moving the brand off red: not taste,
but that red already means failure and cannot also mean Servlo.

**The panel's logo was a 404.** `internal/ui/web/public/` did not exist, so
`/icons/icon.svg` and `/manifest.webmanifest` both failed to load and the rail
rendered a broken-image glyph. Fixed, with an interim mark, alongside this file.

## The mark

**Stacked layers.** Three sheared bars stepping right to left, so the silhouette
reads as an S without anyone drawing a letter. Chosen from six concepts explored
with an image model, then redrawn here as geometry.

`internal/ui/web/public/icons/icon.svg` and `docs/public/assets/logo.svg`, one
shape in both. **284 bytes**, against the 22,018 the letter L cost by embedding
a whole font to draw itself.

Three decisions inside it worth not undoing:

**The middle bar is wider.** Equal bars flatten into a hamburger menu. The step
in and out is the whole S.

**No tile.** A bare mark sits correctly on the light rail and the dark one; a
tile has to pick a side, and then needs a second asset for the other. The
reference sheet used a tile for the app icon and bare in the nav, which is two
files to keep in step for no gain.

**Geometry, no font.** Three polygons. Nothing to embed, nothing to fall back
to, identical everywhere.

## Colour

**`#FF2D20`, kept.** The project owner's call, made with the constraint known.

The constraint, recorded once so nobody rediscovers it as a surprise: this is
Laravel's brand red exactly, and Servlo competes in the Laravel ecosystem
against a Laravel product. Someone will eventually notice. Shifting it a few
percent would make it Servlo's own and cost one token change in
`internal/ui/web/src/app.css`, since every other red in the UI derives from
`--color-servlo-red`. That option stays open and cheap; it is not being taken
now.

What keeping it also buys: the accent appears in **224 places** across the panel
— links, active tabs, primary buttons, badges — so none of them move.

### One thing still worth fixing, separately from the brand

Red is the accent *and* the failure colour. Mostly that is fine: chrome can be
red without meaning anything. The exception is the **Autostart toggle, which is
red when it is on**. On/off has a colour convention older than this product, and
a switch that goes red to mean *enabled* is backwards in a panel whose job is
telling you what broke. That is a component fix, not a palette one — the toggle
should use emerald for on, and leave red to the brand and to errors.

## Still to do

The mark, the panel icon, the favicon, the docs logo and the manifest are done
and verified in both themes. Outstanding:

- a **wordmark** for the README and the docs header, pairing the mark with
  "Servlo" set properly rather than in whatever the page inherits
- the **Autostart toggle** colour, above
- `--color-servlo-red` is a poor name for a token that means *accent*, and
  renaming it is how the brand stops accidentally meaning failure in the next
  person's head. 224 call sites, mechanical, worth doing before more accumulate.
