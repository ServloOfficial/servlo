// Proof sheet: the avatar at the sizes GitHub actually renders it, on both of
// GitHub's page grounds. An avatar is judged at 40px in a sidebar, not at 1024.
import { readFileSync, writeFileSync } from 'node:fs';
import { execSync } from 'node:child_process';

async function loadChromium() {
  const roots = [
    new URL('../internal/ui/web/node_modules/', import.meta.url).href,
    'file://' + execSync('npm root -g').toString().trim() + '/',
  ];
  for (const root of roots)
    for (const pkg of ['playwright-core', 'playwright/node_modules/playwright-core'])
      try { const m = await import(root + pkg + '/index.js');
            const c = (m.default ?? m).chromium; if (c) return c; } catch {}
  throw new Error('playwright not found');
}
const chromium = await loadChromium();

const b64 = (f) => 'data:image/png;base64,' +
  readFileSync(new URL(f, import.meta.url)).toString('base64');
const dark = b64('org-avatar.png');
const light = b64('org-avatar-light.png');

// 260 is the org page header, 40 the sidebar and contributor lists, 20 a
// commit byline. Those three are where it either works or does not.
const SIZES = [260, 100, 64, 40, 20];
const row = (src) => SIZES.map((s) => `
  <div class="c"><img src="${src}" style="width:${s}px;height:${s}px">
  <span>${s}px</span></div>`).join('');

const html = `
<style>
 body{margin:0;font:13px system-ui}
 .g{padding:32px 40px}
 .g.d{background:#0d1117;color:#8b949e}
 .g.l{background:#ffffff;color:#59636e}
 h3{margin:0 0 20px;font-size:12px;text-transform:uppercase;letter-spacing:.08em;font-weight:600}
 .r{display:flex;align-items:flex-end;gap:36px}
 .c{display:flex;flex-direction:column;align-items:center;gap:10px}
 img{border-radius:6px;display:block}
</style>
<div class="g l"><h3>Light page, dark avatar (the one to upload)</h3><div class="r">${row(dark)}</div></div>
<div class="g d"><h3>Dark page, dark avatar</h3><div class="r">${row(dark)}</div></div>
<div class="g d"><h3>Dark page, light avatar (the alternative)</h3><div class="r">${row(light)}</div></div>`;

const browser = await chromium.launch({ executablePath: '/opt/pw-browsers/chromium' });
const p = await browser.newPage({ viewport: { width: 700, height: 1000 }, deviceScaleFactor: 2 });
await p.setContent(html);
await p.waitForTimeout(300);
await p.screenshot({ path: new URL('preview.png', import.meta.url).pathname, fullPage: true });
await browser.close();
console.log('preview.png');
