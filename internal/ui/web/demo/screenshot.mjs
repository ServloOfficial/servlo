// Render the panel and take screenshots of it, so a view can be looked at
// before it is called done (CLAUDE.md step 4.5).
//
// The demo harness stubs window.fetch, so this needs no Go backend, no
// containers and no database: a view is exactly as reachable here as it is in
// the real panel, and everything about how it looks is the same.
//
//   cd internal/ui/web
//   npx vite --config vite.demo.config.ts --port 5199 --host 127.0.0.1 &
//   node demo/screenshot.mjs                 # everything below, light and dark
//   node demo/screenshot.mjs services        # just the shots whose name matches
//
// playwright-core is not a dependency of this package, because CI never runs
// this and a browser driver in the install is a cost every build would pay.
// Install it where you are running from: npm i playwright-core.
//
// Adding a view? Add a SHOT for it, and a fixture under demo/fixtures plus a
// line in demo/stubs.ts, or it renders its empty state and you have looked at
// nothing.

import { chromium } from 'playwright-core';
import { mkdir } from 'node:fs/promises';

const BASE = process.env.DEMO_URL ?? 'http://127.0.0.1:5199/demo/';
const OUT = process.env.SHOT_DIR ?? '/tmp/servlo-shots';
const CHROMIUM = process.env.CHROMIUM ?? '/opt/pw-browsers/chromium';
const filter = process.argv[2];

/** Open the panel section behind one of the left rail's buttons. */
const rail = (name) => async (page) => {
  await page.getByRole('button', { name: new RegExp(`^${name}$`) }).first().click();
  await page.waitForTimeout(900);
};

/** Open a site's detail from its card on the Sites overview, then one of its tabs. */
const siteTab = (card, tab) => async (page) => {
  await rail('Sites')(page);
  await page.getByText(card, { exact: true }).first().click();
  await page.waitForTimeout(1400);
  await page.getByRole('button', { name: new RegExp(`^${tab}$`, 'i') }).first().click();
  await page.waitForTimeout(1000);
};

const SHOTS = [
  { name: 'services', act: rail('Services') },
  { name: 'services-connections-add', height: 2400, act: async (page) => {
      await rail('Services')(page);
      await page.getByRole('button', { name: /Add connection/i }).first().click();
      await page.waitForTimeout(500);
      const managed = page.getByRole('button', { name: /^Managed$/ }).first();
      if (await managed.count()) { await managed.click(); await page.waitForTimeout(400); }
    } },
  { name: 'site-settings', height: 2200, act: siteTab('Acme', 'Settings'), bottom: true },
  { name: 'site-deploy', height: 1600, act: siteTab('Acme', 'Deploy') },
  { name: 'site-cron', height: 1700, act: siteTab('Acme', 'Cron') },
  { name: 'site-cron-form', height: 1900, act: async (page) => {
      await siteTab('Acme', 'Cron')(page);
      await page.getByRole('button', { name: /Add a scheduled command/i }).first().click();
      await page.waitForTimeout(400);
    } },
  { name: 'site-cron-output', height: 1900, act: async (page) => {
      await siteTab('Acme', 'Cron')(page);
      await page.getByRole('button', { name: /Show the last run's output/i }).last().click();
      await page.waitForTimeout(400);
    } },
  { name: 'site-cron-delete', height: 1400, act: async (page) => {
      await siteTab('Acme', 'Cron')(page);
      await page.getByRole('button', { name: /^Delete$/ }).first().click();
      await page.waitForTimeout(500);
    } },
  { name: 'site-cron-wordpress', height: 1500, act: siteTab('blog.orbitlabs.app', 'Cron') },
  { name: 'site-files', height: 1700, act: siteTab('Acme', 'Files') },
  // Upload opens the browser's own file picker, so there is no page state to
  // shoot; the states worth looking at here are the listing and the editor.
  { name: 'site-db-user', height: 2600, act: siteTab('Acme', 'Settings'), bottom: true },
  { name: 'site-backups', height: 3000, act: siteTab('Acme', 'Settings'), bottom: true },
  { name: 'site-backups-form', height: 3400, act: async (page) => {
      await siteTab('Acme', 'Settings')(page);
      await page.getByRole('button', { name: /Change schedule|Add a schedule/i }).first().click();
      await page.waitForTimeout(500);
      await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
      await page.waitForTimeout(300);
    } },
  { name: 'site-backups-none', height: 2600, act: siteTab('blog.orbitlabs.app', 'Settings'), bottom: true },
  // Both sides of the staging relationship: the copy, and the live site that
  // has one.
  { name: 'site-staging', height: 3200, bottom: true, act: siteTab('Acme (staging)', 'Settings') },
  { name: 'site-staging-live', height: 3200, bottom: true, act: siteTab('Acme', 'Settings') },
  { name: 'dashboard', act: rail('Dashboard') },
  { name: 'dashboard-alerts-cleared', act: async (page) => {
      await rail('Dashboard')(page);
      // The other state of the alerts card: dismiss every one and confirm it
      // disappears rather than sitting there empty, and that the all-good
      // strip comes back with it.
      for (let i = 0; i < 8; i++) {
        const button = page.getByRole('button', { name: /^Dismiss$/ }).first();
        if (!(await button.count())) break;
        await button.click();
        await page.waitForTimeout(250);
      }
      await page.waitForTimeout(500);
    } },
  { name: 'system', height: 2200, act: rail('System') },
  { name: 'system-mail', height: 1800, act: async (page) => {
      await rail('System')(page);
      await page.getByText(/^Mail$/).first().click();
      await page.waitForTimeout(900);
    } },
  { name: 'system-security', height: 2600, act: async (page) => {
      await rail('System')(page);
      await page.getByText(/^Security$/).first().click();
      await page.waitForTimeout(900);
    } },
  { name: 'system-security-bottom', height: 2600, bottom: true, act: async (page) => {
      await rail('System')(page);
      await page.getByText(/^Security$/).first().click();
      await page.waitForTimeout(900);
    } },
  { name: 'system-sftp', height: 1800, act: async (page) => {
      await rail('System')(page);
      await page.getByText(/SFTP|File access/i).first().click();
      await page.waitForTimeout(900);
    } },
];

await mkdir(OUT, { recursive: true });
const browser = await chromium.launch({ executablePath: CHROMIUM });

for (const shot of SHOTS) {
  if (filter && !shot.name.includes(filter)) continue;
  for (const dark of [false, true]) {
    const ctx = await browser.newContext({
      viewport: { width: shot.width ?? 1440, height: shot.height ?? 1000 },
      colorScheme: dark ? 'dark' : 'light',
      deviceScaleFactor: 2,
    });
    const page = await ctx.newPage();
    const problems = [];
    page.on('pageerror', (e) => problems.push(e.message));
    page.on('console', (m) => m.type() === 'error' && problems.push(m.text()));

    await page.goto(BASE, { waitUntil: 'networkidle' });
    await page.waitForTimeout(1200);
    try {
      await shot.act?.(page);
    } catch (e) {
      console.log(`${shot.name}: could not reach the view: ${e.message}`);
    }
    // The notification prompt covers the foot of every page it appears on.
    await page.evaluate(() => {
      const enable = [...document.querySelectorAll('button')].find((b) => /^Enable$/.test(b.textContent?.trim() ?? ''));
      enable?.closest('div[class*="fixed"], div[class*="absolute"]')?.remove();
    });
    if (shot.bottom) {
      await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
      await page.waitForTimeout(400);
    }

    const file = `${OUT}/${shot.name}-${dark ? 'dark' : 'light'}.png`;
    await page.screenshot({ path: file });
    console.log(file + (problems.length ? `  (console: ${problems.length})` : ''));
    await ctx.close();
  }
}

await browser.close();
