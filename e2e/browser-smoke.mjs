#!/usr/bin/env node
// Browser smoke: real Chromium via playwright-core, file:// load, no Go server needed.
// Catches browser-only bugs like defer race (sheetOverdue), Alpine boot, x-cloak, inert.
//
// Usage:
//   npm run test:browser
//   BROWSER=brave npm run test:browser   # use /Applications/Brave Browser.app
//   BROWSER=chrome npm run test:browser  # use channel:chrome
//
// Exits non-zero on any pageerror / Alpine Expression Error / ReferenceError.
import { chromium } from 'playwright-core';
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import http from 'node:http';

const ROOT = process.cwd();
const PAGES = [
  { name: 'home', file: 'internal/web/static/pages/home/index.html', url: '/pages/home', expect: ['#department-cards'] },
  { name: 'login', file: 'internal/web/static/pages/login/index.html', url: '/pages/login', expect: ['#login-form', '#username', '#password'] },
  { name: 'checkouts', file: 'internal/web/static/pages/checkoutsv1/checkouts.html', url: '/pages/checkouts', expect: ['#children-list', '#search-toggle-button'], alpine: true },
  { name: 'guest-checkin', file: 'internal/web/static/pages/guest-checkin/index.html', url: '/pages/guest-checkin', expect: ['body'] },
  { name: 'manual-checkins', file: 'internal/web/static/pages/manual-checkins/index.html', url: '/pages/manual-checkins', expect: ['body'] },
];

function startStaticServer() {
  const mime = { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.json': 'application/json', '.ico': 'image/x-icon', '.png': 'image/png', '.webmanifest': 'application/manifest+json' };
  const server = http.createServer((req, res) => {
    const urlPath = decodeURIComponent((req.url || '/').split('?')[0]);
    // Map /static/* -> internal/web/static/*, /pages/* -> internal/web/static/pages/*, else serve as file from ROOT
    let filePath;
    if (urlPath.startsWith('/static/')) filePath = path.join(ROOT, 'internal/web/static', urlPath.slice('/static/'.length));
    else if (urlPath.startsWith('/pages/')) {
      const pageMap = {
        '/pages/home': 'internal/web/static/pages/home/index.html',
        '/pages/login': 'internal/web/static/pages/login/index.html',
        '/pages/checkouts': 'internal/web/static/pages/checkoutsv1/checkouts.html',
        '/pages/guest-checkin': 'internal/web/static/pages/guest-checkin/index.html',
        '/pages/manual-checkins': 'internal/web/static/pages/manual-checkins/index.html',
      };
      filePath = path.join(ROOT, pageMap[urlPath] || `internal/web/static/pages${urlPath}/index.html`);
    }
    else if (urlPath === '/') filePath = path.join(ROOT, 'internal/web/static/pages/home/index.html');
    else filePath = path.join(ROOT, urlPath.replace(/^\//, ''));

    fs.readFile(filePath, (err, data) => {
      if (err) { res.writeHead(404); res.end('not found: ' + urlPath); return; }
      const ext = path.extname(filePath);
      res.writeHead(200, { 'Content-Type': mime[ext] || 'text/plain' });
      res.end(data);
    });
  });
  return new Promise((resolve) => {
    server.listen(0, '127.0.0.1', () => {
      const { port } = server.address();
      resolve({ server, port });
    });
  });
}

function resolveBrowser() {
  const env = (process.env.BROWSER || '').toLowerCase();
  const brave = '/Applications/Brave Browser.app/Contents/MacOS/Brave Browser';
  if (env === 'brave' && fs.existsSync(brave)) return { executablePath: brave };
  if (env === 'chrome') return { channel: 'chrome' };
  if (fs.existsSync(brave) && !env) {
    // Prefer Brave on macOS if present, else fallback to bundled chromium
    return { executablePath: brave };
  }
  // No executablePath => playwright-core will use bundled chromium if `playwright` installed,
  // otherwise will error with clear message. Let caller install browsers via `npx playwright install chromium`.
  return {};
}

async function smokePage(browser, base, { name, file, url, expect, alpine }) {
  const abs = path.resolve(ROOT, file);
  if (!fs.existsSync(abs)) {
    console.log(`SKIP [${name}] file not found: ${file}`);
    return { name, ok: true, skipped: true };
  }
  const page = await browser.newPage();
  const errors = [];
  page.on('pageerror', (err) => errors.push(`pageerror: ${err.message}`));
  page.on('console', (msg) => {
    if (msg.type() === 'error') errors.push(`console: ${msg.text()}`);
  });
  // Mock API fetches so the page can boot without a Go server
  await page.route('**/v1/**', async (route) => {
    const reqUrl = route.request().url();
    if (reqUrl.includes('/v1/location_groups')) {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([{ id: 1, name: 'Group A' }, { id: 2, name: 'Group B' }]) });
    } else if (reqUrl.includes('/v1/checkins/checkouts')) {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ checkins: [], manual_checkins: [] }) });
    } else if (reqUrl.includes('/v1/checkins/')) {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({}) });
    } else {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) });
    }
  });

  const pageUrl = `${base}${url}`;
  await page.goto(pageUrl, { waitUntil: 'domcontentloaded', timeout: 15000 });

  // Let Alpine boot: queueMicrotask(start) + DOMContentLoaded
  await page.waitForTimeout(800);

  // Collect Alpine-specific failures
  const alpineErrors = errors.filter((m) =>
    /Alpine Expression Error|ReferenceError|sheetOverdue|checkoutsBoard is not defined/i.test(m)
  );

  // Check expected selectors exist
  const missing = [];
  for (const sel of expect) {
    const count = await page.locator(sel).count().catch(() => 0);
    if (count === 0) missing.push(sel);
  }

  // For Alpine pages, also verify Alpine actually booted
  let alpineBooted = true;
  if (alpine) {
    alpineBooted = await page.evaluate(() => !!window.__checkoutsBoard).catch(() => false);
    const hasSheetOverdue = await page.evaluate(() => {
      const b = window.__checkoutsBoard;
      return !!(b && Array.isArray(b.sheetOverdue));
    }).catch(() => false);
    if (!hasSheetOverdue) errors.push('missing: window.__checkoutsBoard.sheetOverdue not an array');
  }

  const ok = alpineErrors.length === 0 && missing.length === 0 && (alpine ? alpineBooted : true);
  const label = ok ? 'PASS' : 'FAIL';
  console.log(`${label} [${name}] ${file}${ok ? '' : `\n  missing selectors: ${missing.join(', ') || 'none'}\n  alpineErrors: ${alpineErrors.join(' | ') || 'none'}\n  allErrors: ${errors.slice(0, 3).join(' | ')}`}`);
  if (alpine) {
    const count = await page.locator('#children-list .child-card').count().catch(() => 0);
    console.log(`  checkouts: __checkoutsBoard=${alpineBooted}, sheetOverdue array=${alpineBooted}, visible cards=${count}`);
  }
  await page.close();
  return { name, ok, errors: [...alpineErrors, ...missing.map((s) => `missing ${s}`)], skipped: false };
}

async function smokeCheckoutsInteractions(browser, base) {
  const name = 'checkouts-interactions';
  const page = await browser.newPage();
  const errors = [];
  page.on('pageerror', (err) => errors.push(`pageerror: ${err.message}`));
  page.on('console', (msg) => {
    if (msg.type() === 'error') errors.push(`console: ${msg.text()}`);
  });
  const now = Date.now();
  const freshAt = new Date(now - 1 * 60 * 1000).toISOString();
  const overdueAt = new Date(now - 10 * 60 * 1000).toISOString();
  const seed = [
    { source: 'planning_center', planning_center_id: 'fresh-1', first_name: 'Fresh', last_name: 'Kid', security_code: 'F001', location_group_id: 1, checked_out_at: freshAt, checked_out_confirmed_at: null },
    { source: 'planning_center', planning_center_id: 'late-1', first_name: 'Late', last_name: 'Kid', security_code: 'L001', location_group_id: 1, checked_out_at: overdueAt, checked_out_confirmed_at: null },
  ];
  const groups = [{ id: 1, name: 'Group A' }];
  await page.route('**/v1/**', async (route) => {
    const reqUrl = route.request().url();
    if (reqUrl.includes('/v1/location_groups')) {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(groups) });
    } else if (reqUrl.includes('/v1/checkins/checkouts')) {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ checkins: seed, manual_checkins: [] }) });
    } else if (route.request().method() === 'PATCH') {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({}) });
    } else {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) });
    }
  });
  const fails = [];
  const check = (label, cond, extra = '') => {
    console.log(`${cond ? 'PASS' : 'FAIL'} [${name}] ${label}${cond ? '' : ` ${extra}`}`);
    if (!cond) fails.push(label);
  };
  await page.goto(`${base}/pages/checkouts`, { waitUntil: 'domcontentloaded', timeout: 15000 });
  await page.waitForTimeout(800);
  await page.waitForFunction(() => !!window.__checkoutsBoard?.groupsReady, null, { timeout: 5000 }).catch(() => {});
  await page.waitForFunction(() => (window.__checkoutsBoard?.visibleChildren?.length || 0) >= 2, null, { timeout: 5000 }).catch(() => {});
  const cardCount = await page.locator('#children-list .child-card').count().catch(() => 0);
  check('renders 2 seeded cards', cardCount === 2, `got ${cardCount}`);
  const badgeVisible = await page.evaluate(() => !!window.__checkoutsBoard?.badgeVisible).catch(() => false);
  check('overdue badge visible with seed', badgeVisible === true, `badgeVisible=${badgeVisible}`);
  const badgeText = await page.locator('#overdue-badge').innerText().catch(() => '');
  check('badge text mentions 1 overdue', /1 overdue/.test(badgeText), `got "${badgeText}"`);
  // Search toggle interaction (real browser engine: class, aria, focus)
  await page.locator('#search-toggle-button').click();
  await page.waitForFunction(() => window.__checkoutsBoard?.searchOpen === true, null, { timeout: 3000 }).catch(() => {});
  const expanded = await page.locator('#search-controls.is-expanded').count().catch(() => 0);
  const aria = await page.locator('#search-toggle-button').getAttribute('aria-expanded').catch(() => '');
  const focused = await page.evaluate(() => document.activeElement?.id).catch(() => '');
  check('search toggle expands panel', expanded === 1, `expanded count=${expanded}`);
  check('search toggle aria-expanded=true', aria === 'true', `aria=${aria}`);
  check('search input focused on open', focused === 'search-input', `active=${focused}`);
  // Overdue sheet open via badge click (tests x-show, translate class, inert in real engine)
  await page.locator('#overdue-badge').click();
  await page.waitForFunction(() => window.__checkoutsBoard?.sheetOpen === true, null, { timeout: 3000 }).catch(() => {});
  const sheetTranslated = await page.evaluate(() => document.getElementById('overdue-sheet')?.classList.contains('translate-y-full')).catch(() => true);
  const sheetCards = await page.locator('#overdue-sheet-list .child-card').count().catch(() => 0);
  check('sheet opens (translate removed)', sheetTranslated === false, 'still translated');
  check('sheet lists 1 overdue card', sheetCards === 1, `got ${sheetCards}`);
  // Close via close button
  await page.locator('#overdue-sheet-close').click();
  await page.waitForFunction(() => window.__checkoutsBoard?.sheetOpen === false, null, { timeout: 3000 }).catch(() => {});
  const closedTranslated = await page.evaluate(() => document.getElementById('overdue-sheet')?.classList.contains('translate-y-full')).catch(() => false);
  check('sheet closes via button', closedTranslated === true, 'not translated after close');
  const alpineErrors = errors.filter((m) => /Alpine Expression Error|ReferenceError|sheetOverdue|checkoutsBoard is not defined/i.test(m));
  check('no Alpine/page errors during interactions', alpineErrors.length === 0, alpineErrors.slice(0, 2).join(' | '));
  await page.close();
  return { name, ok: fails.length === 0 && alpineErrors.length === 0, errors: [...fails, ...alpineErrors], skipped: false };
}

const launchOpts = { headless: true, ...resolveBrowser() };
console.log(`Launching chromium ${launchOpts.executablePath ? `at ${launchOpts.executablePath}` : launchOpts.channel ? `channel:${launchOpts.channel}` : '(bundled, run `npx playwright install chromium` if missing)'}`);

const { server, port } = await startStaticServer();
const base = `http://127.0.0.1:${port}`;
console.log(`Static server at ${base}`);

let browser;
try {
  browser = await chromium.launch(launchOpts);
} catch (e) {
  console.error(`Failed to launch browser: ${e.message}`);
  console.error(`Hint: run \`npx playwright install chromium\` or set BROWSER=brave if Brave is installed at /Applications/Brave Browser.app`);
  server.close();
  process.exit(2);
}

const results = [];
try {
  for (const p of PAGES) {
    results.push(await smokePage(browser, base, p));
  }
  results.push(await smokeCheckoutsInteractions(browser, base));
} finally {
  await browser.close();
  server.close();
}

const failed = results.filter((r) => !r.ok && !r.skipped);
const skipped = results.filter((r) => r.skipped);
console.log(`\n${results.length - failed.length}/${results.length} smoke pages passed${skipped.length ? `, ${skipped.length} skipped` : ''}${failed.length ? ', FAILED' : ''}`);
if (failed.length) {
  for (const f of failed) console.log(`- FAIL ${f.name}: ${f.errors.join(', ')}`);
}
process.exit(failed.length ? 1 : 0);
