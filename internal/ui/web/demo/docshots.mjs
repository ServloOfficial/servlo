// Regenerate the screenshots the documentation site embeds.
//
// Separate from screenshot.mjs, which exists to look at a view before calling
// it done and so shoots both themes at whatever height suits. These are
// published images: one theme, one viewport, named exactly as the docs
// reference them, written straight into the directory VitePress serves.
//
//   cd internal/ui/web
//   npx vite --config vite.demo.config.ts --port 5199 --host 127.0.0.1 &
//   node demo/docshots.mjs                # all of them
//   node demo/docshots.mjs site           # just the ones whose name matches
//
// Needs playwright-core, which is deliberately not a dependency here: nothing
// automated runs this, and a browser driver would be a cost on every install.

import { chromium } from 'playwright-core';
import { mkdir } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

const BASE = process.env.DEMO_URL ?? 'http://127.0.0.1:5199/demo/';
const CHROMIUM = process.env.CHROMIUM ?? '/opt/pw-browsers/chromium';
// The directory VitePress actually serves. docs/assets/ is not it, which is how
// every one of these came to be a broken image on the published site.
const OUT = fileURLToPath(new URL('../../../../docs/public/assets/screenshots/', import.meta.url));
const filter = process.argv[2];

// Dark, because that is the panel's own default and it matches the README hero.
// 1440x900 at 2x: wide enough for the three-column dashboard, and it lands at
// roughly the width VitePress gives an image in the content column.
const VIEWPORT = { width: 1440, height: 900 };
const SCALE = 2;

const rail = (name) => async (page) => {
  await page.getByRole('button', { name: new RegExp(`^${name}$`) }).first().click();
  await page.waitForTimeout(1000);
};

const siteTab = (card, tab) => async (page) => {
  await rail('Sites')(page);
  await page.getByText(card, { exact: true }).first().click();
  await page.waitForTimeout(1400);
  await page.getByRole('button', { name: new RegExp(`^${tab}$`, 'i') }).first().click();
  await page.waitForTimeout(1100);
};

const SHOTS = [
  { name: 'dashboard', act: rail('Dashboard') },
  { name: 'sites-list', act: rail('Sites') },
  { name: 'services-list', act: rail('Services') },
  // System opens on Servlo, so shooting it bare would produce a file identical
  // to system-servlo below. Nginx is the first section and shows the pane
  // switching, which is what a reader wants from "the System tab".
  { name: 'system', act: async (page) => {
      await rail('System')(page);
      await page.getByRole('button', { name: /^Nginx$/ }).first().click();
      await page.waitForTimeout(1000);
    } },

  { name: 'site-detail-overview', act: siteTab('Acme', 'Overview') },
  { name: 'site-detail-applogs', act: async (page) => {
      await siteTab('Acme', 'Logs')(page);
      await page.getByRole('button', { name: /^App Logs$/ }).first().click();
      await page.waitForTimeout(900);
    } },

  // A paused site shows the Resume placeholder instead of its live panes, which
  // is the whole point of the shot. Ledger is the paused one in the fixtures.
  { name: 'site-detail-paused', act: async (page) => {
      await rail('Sites')(page);
      await page.getByText('Ledger', { exact: true }).first().click();
      await page.waitForTimeout(1400);
    } },

  { name: 'site-detail-phpfpm', act: async (page) => {
      await siteTab('Acme', 'Logs')(page);
      await page.getByRole('button', { name: /^PHP-FPM$/ }).first().click();
      await page.waitForTimeout(900);
    } },

  { name: 'system-servlo', act: async (page) => {
      await rail('System')(page);
      await page.getByRole('button', { name: /^Servlo$/ }).first().click();
      await page.waitForTimeout(1000);
    } },

  { name: 'preset-picker-modal', act: async (page) => {
      await rail('Services')(page);
      await page.getByRole('button', { name: /^Add a service preset$/ }).first().click();
      await page.waitForTimeout(1200);
    } },

  { name: 'command-palette', act: async (page) => {
      await page.keyboard.press('Control+k');
      await page.waitForTimeout(800);
    } },
];

await mkdir(OUT, { recursive: true });
const browser = await chromium.launch({ executablePath: CHROMIUM });
let failed = 0;

for (const shot of SHOTS) {
  if (filter && !shot.name.includes(filter)) continue;
  const page = await browser.newPage({
    viewport: VIEWPORT, colorScheme: 'dark', deviceScaleFactor: SCALE
  });
  const errors = [];
  page.on('console', (m) => m.type() === 'error' && errors.push(m.text()));
  page.on('pageerror', (e) => errors.push(String(e)));
  try {
    await page.goto(BASE);
    await page.waitForTimeout(1600);
    // The notification prompt is a demo-time overlay rather than the panel's
    // resting state, so it stays out of a published image.
    await page.evaluate(() => {
      document.querySelectorAll('*').forEach((el) => {
        if (el.textContent?.trim().startsWith('Get notifications from')) {
          let n = el;
          for (let i = 0; i < 5 && n; i++) {
            if (getComputedStyle(n).position === 'fixed') { n.remove(); return; }
            n = n.parentElement;
          }
        }
      });
    });
    await shot.act(page);
    await page.waitForTimeout(400);
    await page.screenshot({ path: OUT + shot.name + '.png' });
    console.log(`${shot.name}.png` + (errors.length ? `  (console: ${errors.length})` : ''));
  } catch (err) {
    failed++;
    console.error(`${shot.name}: ${err.message.split('\n')[0]}`);
  }
  await page.close();
}

await browser.close();
if (failed) { console.error(`\n${failed} shot(s) failed`); process.exit(1); }
