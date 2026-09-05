# The organisation profile page

GitHub renders an organisation's front page from a **public repository named
`.github`**, at `profile/README.md`. It is a separate repository from the
product, which is why this copy lives here instead.

To put it up:

1. Create a public repository called `.github` in the `ServloOfficial`
   organisation. The name is exact, dot included, and it must be public even if
   the product repository is not, or the page will not render.
2. Add `README.md` from this directory at the path `profile/README.md`.
3. Visit `https://github.com/ServloOfficial`. The page is the profile now.

The copy is the README's opening, deliberately. Someone landing on the
organisation and someone landing on the repository should be told the same
thing in the same words. Both link to the product repository, and neither has
badges, because an organisation page with build badges on it is a page about
the build rather than the product.

The image is referenced by raw URL rather than committed twice, so the mark
stays in one place. That URL only resolves once `ServloOfficial/servlo` is
public; before then the page renders with a broken image.
