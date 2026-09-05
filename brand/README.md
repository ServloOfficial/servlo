# brand/

Raster and off-site brand assets. Everything here is derived from
`docs/public/assets/logo.svg`, which is the mark itself and the only file to
edit if the shape ever changes. `BRANDING.md` at the repository root is the
reasoning; this is the output.

| File | What it is for |
|---|---|
| `org-avatar.png` | The GitHub organisation profile picture. Upload this one. |
| `org-avatar-light.png` | The same mark on white, for a light ground that needs a defined edge. |
| `mark-1024.png` | Transparent, for a slide or a header that brings its own background. |
| `org-profile/` | The organisation front page, which lives in a separate `.github` repository. `HOWTO.md` says how. |

## Why the dark one

An organisation avatar is judged at 40px in a sidebar, not at 1024. Rendered at
every size GitHub actually uses, on both of GitHub's page grounds, the dark
variant is the only one that works everywhere: on a light page it has a
defined edge, and on a dark page it recedes into the background in a way that
reads as intentional. The light variant is the reverse and is wrong as a
default, since GitHub's light theme is still the one most people see.

Below 40px the mark becomes a red smudge. That is true of nearly any mark at a
commit byline and is not worth designing around.

## Regenerating

```bash
node brand/render.mjs      # the assets
node brand/preview.mjs     # the proof sheet, at every size on both grounds
```

Playwright is not a dependency of this repository, because nothing automated
runs these and a browser driver would be a cost on every install. Install it
however suits (`npm i -g playwright` works) and both scripts find it.

Look at `preview.png` after changing the mark. An avatar that was only ever
seen at full size is an avatar nobody has checked.
