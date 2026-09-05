// Renders the Servlo mark into the raster assets GitHub and the docs need.
// The mark itself is docs/public/assets/logo.svg; nothing here redraws it, so
// changing the geometry in one place changes every output.
//
//   node brand/render.mjs
//
// Output lands in brand/. Playwright is not a dependency of this repository:
// nothing automated runs this, and a browser driver would be a cost on every
// install. Install it however suits (`npm i -g playwright` works) and this
// finds it.

import { readFileSync, mkdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { execSync } from 'node:child_process';

async function loadChromium() {
  const roots = [
    new URL('../internal/ui/web/node_modules/', import.meta.url).href,
    'file://' + execSync('npm root -g').toString().trim() + '/',
  ];
  for (const root of roots) {
    for (const pkg of ['playwright-core', 'playwright/node_modules/playwright-core']) {
      try {
        // CommonJS under the hood, so the whole module lands on .default.
        const m = await import(root + pkg + '/index.js');
        const c = (m.default ?? m).chromium;
        if (c) return c;
      } catch {}
    }
  }
  throw new Error('playwright not found; try: npm i -g playwright');
}
const chromium = await loadChromium();

const ROOT = fileURLToPath(new URL('../', import.meta.url));
const OUT = ROOT + 'brand/';
const MARK = readFileSync(ROOT + 'docs/public/assets/logo.svg', 'utf8');

mkdirSync(OUT, { recursive: true });

// The mark's geometry occupies x 14-86, y 20-80 of its 0 0 100 100 viewBox, so
// rendering the file as-is wastes a third of the frame on the SVG's own empty
// margin and the mark comes out too small to read once GitHub scales an avatar
// to 40px in a sidebar. Crop to the bounding box and size it here instead.
const BBOX = { x: 14, y: 20, w: 72, h: 60 };
const cropped = MARK.replace('viewBox="0 0 100 100"',
  `viewBox="${BBOX.x} ${BBOX.y} ${BBOX.w} ${BBOX.h}"`);

// Fraction of the avatar's width the mark spans. 0.68 leaves a margin that
// survives GitHub's rounded-square crop without the mark swimming in space.
const page = (bg, fill = 0.68) => {
  const w = Math.round(1024 * fill);
  const h = Math.round((w * BBOX.h) / BBOX.w);
  return `
<style>
  html,body{margin:0;padding:0}
  .a{width:1024px;height:1024px;background:${bg};display:flex;
     align-items:center;justify-content:center}
  .a svg{width:${w}px;height:${h}px}
</style>
<div class="a">${cropped}</div>`;
};

const SHOTS = [
  // The one to upload. Dark, because the panel is dark by default, the README
  // hero is dark, and a red mark on near-black is the product's own look.
  { name: 'org-avatar', bg: '#0d0d0d' },
  // For anywhere the avatar sits on a light page and must not be a black box.
  { name: 'org-avatar-light', bg: '#ffffff' },
  // Transparent, for a slide or a header that brings its own ground.
  { name: 'mark-1024', bg: 'transparent', fill: 0.92 },
];

const browser = await chromium.launch({ executablePath: '/opt/pw-browsers/chromium' });
for (const s of SHOTS) {
  const p = await browser.newPage({ viewport: { width: 1024, height: 1024 } });
  await p.setContent(page(s.bg, s.fill));
  await p.waitForTimeout(150);
  await p.screenshot({ path: OUT + s.name + '.png', omitBackground: s.bg === 'transparent' });
  console.log(s.name + '.png');
  await p.close();
}
await browser.close();
