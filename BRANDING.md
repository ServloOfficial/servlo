# BRANDING.md — Servlo's visual identity

Servlo has never had one. It inherited the upstream project's, the rename swept
the strings and left the pixels, and the pixels are the part people see. This is the brief for
fixing that, plus the prompts to generate the logo.

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

## The interim mark

`internal/ui/web/public/icons/icon.svg`: three stacked rounded bars on a slate
square. A rack, which is what the product manages. Geometry only, no embedded
font, under 1KB.

It is a placeholder and should read as one. It exists so the panel is not broken
while you decide, and because a neutral slate is a more honest holding position
than a competitor's red.

## Palette

Pick one direction. Every one of these avoids the three status colours the panel
already spends — red for failure, amber for warning, emerald for healthy — which
is the constraint that rules out most obvious choices.

| Direction | Primary | Why | Against |
|---|---|---|---|
| **Slate + cyan** (interim leans here) | `#0891B2` on `#1E293B` | Reads as infrastructure without shouting. Cyan is far from all three status colours. | Cyan is common in devops tooling |
| **Deep violet** | `#6D28D9` | Distinct in the hosting space, which is overwhelmingly blue. Strong at 16px. | Close to several PaaS brands |
| **Ink + warm sand** | `#0F172A` with `#D97706` accent | Calm and unusual; the accent carries the brand and the ink carries the UI. | Sand is near the amber warning colour, needs care |

My recommendation is **slate + cyan**: the panel is already a dark slate surface,
so it needs the least reworking, and cyan is the furthest from anything the
status colours use. But this is a judgement call about how you want the product
to feel, and you should look at all three rather than take my word.

Whatever you pick, it is four values in
`internal/ui/web/src/app.css` (`--color-servlo-red` and friends, which should be
renamed to `--color-servlo-accent` at the same time, because the token being
called *red* is how the brand ended up meaning failure), plus `theme-color` in
`index.html` and the manifest.

## The logo brief

Constraints, in the order they matter:

1. **Legible at 16px.** It is a favicon and a 28px rail icon far more often than
   it is a hero image. Thin lines and fine detail disappear.
2. **Works in one colour.** It will be stamped on a terminal, a README badge and
   a monochrome favicon.
3. **Reads on `#0d0d0d` and on white.** The panel ships both themes.
4. **No gradients, no bevels, no glow.** They fall apart when scaled down and
   date badly.
5. **Not a cloud, not a gear, not a generic hexagon.** Every hosting product has
   one and none of them is memorable.

Concept directions worth exploring: stacked units suggesting a rack or layers;
an abstract S built from two or three geometric strokes; a container/box motif
that hints at Podman without being a whale; a monogram where the S doubles as a
signal or a path.

## Prompts

Be realistic about what image models are for here. They are good at exploring a
direction and bad at clean vector logos, and they cannot spell — an "S" will
come out malformed more often than not. Use these to find a direction you like,
then send me the one you want and I will redraw it as a proper SVG. Do not ship
a raster from any of them.

**Prompt 1 — abstract mark exploration**

> Design a minimalist app icon for "Servlo", a self-hosted control panel for
> managing production web servers. The mark is abstract and geometric, suggesting
> stacked server units or layered infrastructure. Flat vector style, solid
> shapes, no gradients, no shadows, no 3D. Two colours only: a deep slate
> background and a single bright accent. Centred on a rounded square. Must stay
> readable when shrunk to 16 pixels. Show 6 distinct variations on a plain grey
> background. No text, no lettering, no words anywhere in the image.

**Prompt 2 — monogram**

> Design a geometric letter S monogram for a developer tool called Servlo. The S
> is constructed from two or three thick straight or angular strokes, like a
> logotype cut from paper, not handwriting and not a script font. Flat vector,
> single accent colour on a dark slate rounded square, no gradients, no outlines,
> no shadows. Bold enough to read at 16 pixels. Show 6 variations. The only
> letterform in the image is the S itself.

**Prompt 3 — the honest one, run it as a conversation not an image**

Paste this into ChatGPT as text, without asking for a picture:

> I need a logo concept for "Servlo", a free open-source control panel for
> running PHP sites on a single Ubuntu server. It competes with cPanel, Laravel
> Forge and Ploi. It is self-hosted, deliberately plain, and the feeling I want
> is calm reliability, the thing you are glad to open at 3am, not a flashy
> startup. It must not look framework-specific, because it runs WordPress,
> Laravel, Symfony, Joomla and Grav equally. Constraints: legible at 16px, works
> in one colour, works on both black and white backgrounds, no gradients, and
> not a cloud or a gear. Give me 5 concepts described in words, each with the
> idea behind it and how it would be constructed geometrically. Do not generate
> images yet.

That third one usually beats both image prompts, because the useful output at
this stage is an idea you can have drawn properly, not a picture you cannot
edit.

## When you have picked one

Send me the concept and I will produce: the SVG mark, the panel icon, the
favicon, the docs logo, a wordmark for the README, and the palette wired through
`app.css`, `index.html` and the manifest, with a screenshot of the panel in both
themes before I call it done.
