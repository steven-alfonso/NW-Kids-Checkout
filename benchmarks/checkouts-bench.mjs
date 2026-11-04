// Shared benchmark harness for the three checkouts spikes.
//
// Run one framework at a time so the ports and CPU are not shared:
//   node benchmarks/checkouts-bench.mjs --name=vue \
//     --bundle=internal/web/static/dist/vue/checkouts.js
//
// Results are written to benchmarks/results-<name>.json. This is a local
// comparison tool, not a CI gate; absolute numbers vary by machine.
import { spawn } from 'node:child_process';
import { readFileSync, statSync, writeFileSync } from 'node:fs';
import { gzipSync } from 'node:zlib';
import { chromium } from '@playwright/test';
import {
  BASE_TIME,
  defaultChildren,
  makeChild,
  minutesAgo
} from '../test-support/conformance/fixtures.js';
import { createState, installFakeApi } from '../test-support/conformance/fake-api.js';

const args = new Map(
  process.argv.slice(2).map((arg) => {
    const [key, value = 'true'] = arg.replace(/^--/, '').split('=');
    return [key, value];
  })
);

const NAME = args.get('name') || 'checkouts';
const BUNDLE =
  args.get('bundle') || `internal/web/static/dist/${NAME}/checkouts.js`;
const RUNS = Number(args.get('runs') || 5);
const CPU = Number(args.get('cpu') || 1);
const PAGE_PATH =
  process.env.CHECKOUTS_PAGE || '/static/pages/checkoutsv1/checkouts.html';
const BASE_URL = 'http://127.0.0.1:4173';
const VIEWPORT = { width: 1280, height: 720 };

function stats(samples) {
  const sorted = [...samples].sort((a, b) => a - b);
  const pct = (p) => sorted[Math.min(sorted.length - 1, Math.floor((p / 100) * sorted.length))];
  const sum = sorted.reduce((total, value) => total + value, 0);
  return {
    count: sorted.length,
    min: Number(sorted[0]?.toFixed(2)),
    median: Number(pct(50)?.toFixed(2)),
    p95: Number(pct(95)?.toFixed(2)),
    max: Number(sorted.at(-1)?.toFixed(2)),
    mean: Number((sum / sorted.length).toFixed(2)),
    samples: samples.map((value) => Number(value.toFixed(2)))
  };
}

function bundleMetrics(path) {
  const bytes = statSync(path).size;
  const gzipBytes = gzipSync(readFileSync(path)).length;
  return { path, bytes, gzipBytes };
}

async function waitForHealth(timeoutMs = 10_000) {
  const started = Date.now();
  while (Date.now() - started < timeoutMs) {
    try {
      const response = await fetch(`${BASE_URL}/health`);
      if (response.ok) return;
    } catch {
      // Server not up yet.
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error('benchmark static server did not become healthy');
}

async function openPage(browser, state, { clock = false, expectedCount = null } = {}) {
  const context = await browser.newContext({ viewport: VIEWPORT, timezoneId: 'UTC' });
  const page = await context.newPage();
  if (CPU > 1) {
    const cdp = await context.newCDPSession(page);
    await cdp.send('Emulation.setCPUThrottlingRate', { rate: CPU });
  }
  await page.addInitScript(() => {
    window.__benchLcp = null;
    try {
      new PerformanceObserver((list) => {
        for (const entry of list.getEntries()) {
          if (entry.name === 'largest-contentful-paint') {
            window.__benchLcp = entry.startTime;
          }
        }
      }).observe({ type: 'largest-contentful-paint', buffered: true });
    } catch {
      // LCP is best-effort.
    }
  });
  const api = await installFakeApi(page, state);
  if (clock) {
    await page.clock.install({ time: BASE_TIME });
    await page.clock.pauseAt(BASE_TIME);
  }
  const started = performance.now();
  await page.goto(`${BASE_URL}${PAGE_PATH}`, { waitUntil: 'load' });
  const expected =
    expectedCount ??
    state.children.filter((child) => child.checked_out_at).length;
  await page.waitForFunction(
    (count) => document.querySelectorAll('#children-list .child-time').length === count,
    expected
  );
  const boardReady = performance.now() - started;
  const paint = await page.evaluate(() => {
    const paintEntries = performance.getEntriesByType('paint');
    const nav = performance.getEntriesByType('navigation')[0];
    const firstPaint = paintEntries.find((entry) => entry.name === 'first-paint');
    const fcp = paintEntries.find((entry) => entry.name === 'first-contentful-paint');
    return {
      firstPaint: firstPaint?.startTime ?? null,
      fcp: fcp?.startTime ?? null,
      lcp: window.__benchLcp,
      domContentLoaded: nav?.domContentLoadedEventEnd ?? null,
      load: nav?.loadEventEnd ?? null
    };
  });
  return { context, page, api, boardReady, paint };
}

async function measureInitial(browser, state, runs) {
  const samples = [];
  for (let index = 0; index < runs; index += 1) {
    const { context, boardReady, paint } = await openPage(browser, state, {
      expectedCount: state.children.filter((child) => child.checked_out_at).length
    });
    samples.push({
      boardReady,
      firstPaint: paint.firstPaint,
      fcp: paint.fcp,
      lcp: paint.lcp,
      domContentLoaded: paint.domContentLoaded,
      load: paint.load
    });
    await context.close();
  }
  return {
    boardReady: stats(samples.map((sample) => sample.boardReady)),
    firstPaint: stats(samples.map((sample) => sample.firstPaint).filter((value) => value != null)),
    fcp: stats(samples.map((sample) => sample.fcp).filter((value) => value != null)),
    lcp: stats(samples.map((sample) => sample.lcp).filter((value) => value != null)),
    domContentLoaded: stats(samples.map((sample) => sample.domContentLoaded).filter((value) => value != null)),
    load: stats(samples.map((sample) => sample.load).filter((value) => value != null)),
    samples
  };
}

async function measurePoll(browser, state, runs) {
  const { context, page, api } = await openPage(browser, state, { clock: true });
  const samples = [];
  for (let index = 0; index < runs; index += 1) {
    api.addChild(
      makeChild({
        planning_center_id: `bench-${index}`,
        first_name: 'Bench',
        last_name: `Child ${index}`,
        checked_out_at: minutesAgo(index / 60)
      })
    );
    const started = performance.now();
    await page.clock.runFor(3000);
    await page.waitForFunction(
      (count) => document.querySelectorAll('#children-list .child-time').length === count,
      state.children.length
    );
    samples.push(performance.now() - started);
  }
  await context.close();
  return stats(samples);
}

async function measurePoll100(browser) {
  const children = Array.from({ length: 100 }, (_, index) =>
    makeChild({
      planning_center_id: `bench-100-${index}`,
      first_name: 'Bench',
      last_name: `Child ${index}`,
      checked_out_at: minutesAgo(index / 60)
    })
  );
  const state = createState({ children });
  const { context, page, api } = await openPage(browser, state, {
    clock: true,
    expectedCount: 100
  });
  const samples = [];
  for (let index = 0; index < 3; index += 1) {
    api.state.children[0].first_name = `Changed ${index}`;
    const started = performance.now();
    await page.clock.runFor(3000);
    await page.waitForFunction(
      (name) => document.querySelector('#children-list')?.textContent.includes(name),
      `Changed ${index}`
    );
    samples.push(performance.now() - started);
  }
  await context.close();
  return stats(samples);
}

async function measureTick100(browser) {
  const children = Array.from({ length: 100 }, (_, index) =>
    makeChild({
      planning_center_id: `tick-100-${index}`,
      first_name: 'Tick',
      last_name: `Child ${index}`,
      checked_out_at: minutesAgo(index / 60)
    })
  );
  const { context, page } = await openPage(browser, createState({ children }), {
    clock: true,
    expectedCount: 100
  });
  const samples = [];
  for (let index = 0; index < 5; index += 1) {
    const started = performance.now();
    await page.clock.runFor(1000);
    samples.push(performance.now() - started);
  }
  await context.close();
  return stats(samples);
}

async function measureInteractions(browser) {
  const { context, page } = await openPage(browser, createState(), {
    clock: true,
    expectedCount: 7
  });
  const samples = { openSheet: [], closeSheet: [], search: [], confirm: [] };

  // Warm up the sheet transition once so the first sample is not cold-start.
  await page.locator('#overdue-badge').click();
  await page.waitForFunction(
    () => document.getElementById('overdue-sheet')?.getAttribute('aria-hidden') === 'false'
  );
  await page.locator('#overdue-sheet-close').click();
  await page.waitForFunction(
    () => document.getElementById('overdue-sheet')?.getAttribute('aria-hidden') === 'true'
  );

  for (let index = 0; index < 3; index += 1) {
    let started = performance.now();
    await page.locator('#overdue-badge').click();
    await page.locator('#overdue-sheet').waitFor({ state: 'visible' });
    await page.waitForFunction(
      () => document.getElementById('overdue-sheet')?.getAttribute('aria-hidden') === 'false'
    );
    samples.openSheet.push(performance.now() - started);

    started = performance.now();
    await page.locator('#overdue-sheet-close').click();
    await page.waitForFunction(
      () => document.getElementById('overdue-sheet')?.getAttribute('aria-hidden') === 'true'
    );
    samples.closeSheet.push(performance.now() - started);
  }

  await page.locator('#search-toggle-button').click();
  await page.locator('#search-input').waitFor({ state: 'visible' });
  for (let index = 0; index < 3; index += 1) {
    const query = index % 2 === 0 ? 'fresh' : 'kid';
    const started = performance.now();
    await page.locator('#search-input').fill(query);
    await page.waitForTimeout(50);
    await page.waitForFunction(
      (value) =>
        document.querySelectorAll('#children-list .child-time').length ===
        (value === 'fresh' ? 1 : 6),
      query
    );
    samples.search.push(performance.now() - started);
  }

  await page.locator('#search-input').fill('');
  await page.waitForFunction(
    () => document.querySelectorAll('#children-list .child-time').length === 7
  );
  for (const id of ['pc:fresh', 'pc:edge', 'pc:late']) {
    const label = page.locator(
      `#children-list div:has([data-child-id="${id}"]) [data-confirmed-label]`
    );
    if ((await label.count()) === 0) continue;
    const started = performance.now();
    await label.click();
    await page.waitForFunction(
      (childId) =>
        document
          .querySelector(`input.child-confirmed-checkbox[data-child-id="${childId}"]`)
          ?.closest('[data-confirmed-label]')
          ?.getAttribute('data-confirmed-state') === 'confirmed',
      id
    );
    samples.confirm.push(performance.now() - started);
  }

  await context.close();
  return Object.fromEntries(
    Object.entries(samples).map(([key, values]) => [key, stats(values)])
  );
}

const server = spawn(process.execPath, ['test-support/conformance/server.mjs'], {
  stdio: 'ignore'
});

let browser;
try {
  await waitForHealth();
  browser = await chromium.launch();
  const initial7 = await measureInitial(browser, createState(), RUNS);
  const initial100 = await measureInitial(
    browser,
    createState({
      children: Array.from({ length: 100 }, (_, index) =>
        makeChild({
          planning_center_id: `initial-100-${index}`,
          first_name: 'Initial',
          last_name: `Child ${index}`,
          checked_out_at: minutesAgo(index / 60)
        })
      )
    }),
    3
  );
  const poll7 = await measurePoll(browser, createState(), RUNS);
  const poll100 = await measurePoll100(browser);
  const tick100 = await measureTick100(browser);
  const interactions = await measureInteractions(browser);
  const report = {
    name: NAME,
    pagePath: PAGE_PATH,
    environment: {
      node: process.version,
      platform: process.platform,
      arch: process.arch,
      viewport: VIEWPORT,
      runs: RUNS,
      cpuThrottle: CPU,
      note: 'Local comparison only; static assets served from localhost.'
    },
    bundle: bundleMetrics(BUNDLE),
    initial7,
    initial100,
    poll7,
    poll100,
    tick100,
    interactions
  };
  const output = `benchmarks/results-${NAME}.json`;
  writeFileSync(output, `${JSON.stringify(report, null, 2)}\n`);
  console.log(JSON.stringify(report, null, 2));
} finally {
  if (browser) await browser.close();
  server.kill('SIGTERM');
}
