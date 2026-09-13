// A/B validation: main vs alpine branch checkouts board.
// Drives installed Brave via playwright-core (no downloaded browsers).
//
// Procedure (fresh timestamps each run — fixture ages in wall-clock time):
//   cp /tmp/ab-fixture.db /tmp/validate-main.db
//   cp /tmp/ab-fixture.db /tmp/validate-alpine.db
//   <restart both apiservers>
//   AB_PASSWORD='<LOGIN_PASSWORD_ADMIN>' node e2e/ab-validate/checkouts.mjs
//
// Exits non-zero on any behavior mismatch or page error.
import { chromium } from 'playwright-core';
import fs from 'node:fs';

const BRAVE = '/Applications/Brave Browser.app/Contents/MacOS/Brave Browser';
const SIDES = [
    { name: 'main', base: process.env.AB_MAIN || 'http://localhost:3000' },
    { name: 'alpine', base: process.env.AB_ALPINE || 'http://localhost:3001' },
];
const BOARD = '/v1/checkins/checkouts';
const SHOTS = '/tmp/ab-validate';
const PASSWORD = process.env.AB_PASSWORD;
if (!PASSWORD) {
    console.error('AB_PASSWORD is required (admin login password)');
    process.exit(2);
}
fs.mkdirSync(SHOTS, { recursive: true });

const results = [];
function check(side, flow, name, actual, expected) {
    const a = JSON.stringify(actual);
    const e = JSON.stringify(expected);
    const ok = a === e;
    results.push({ side, flow, name, ok, actual, expected });
    console.log(`${ok ? 'PASS' : 'FAIL'} [${side}] ${flow}: ${name}${ok ? '' : `\n  expected: ${e}\n  actual:   ${a}`}`);
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const CARD_SEL = '#children-list > div:has(.child-confirmed-checkbox)';

// Filter/confirm inputs are sr-only or opacity-0 by design; toggle them
// directly (both frameworks listen to change/input events).
async function setBox(locator, value) {
    await locator.evaluate((el, v) => {
        el.checked = v;
        el.dispatchEvent(new Event('input', { bubbles: true }));
        el.dispatchEvent(new Event('change', { bubbles: true }));
    }, value);
}

// Time-dependent expectations are derived from live API data with the same
// formula the app uses, so assertions hold no matter how old the fixture is.
function pillFor(ms, confirmed, now) {
    if (confirmed) return 'bg-gray-400';
    if (!ms) return 'bg-green-500';
    const mins = Math.max(0, (now - ms) / 60000);
    if (mins >= 8) return 'bg-red-500';
    if (mins >= 4) return 'bg-yellow-500';
    return 'bg-green-500';
}
const idOf = (c) => c.source === 'manual' ? `manual:${c.public_id}`
    : `pc:${c.planning_center_id || c.public_id}`;
const nameOf = (c) => `${c.first_name || ''} ${c.last_name || ''}`.trim();
const codeOf = (c) => c.source === 'manual' ? '---' : (c.security_code || '----');

async function apiRows(page, now) {
    const data = await page.evaluate(async () => {
        const res = await fetch('/v1/checkins/checkouts/?limit=100', { headers: { Accept: 'application/json' } });
        return res.json();
    });
    const rows = [...(data.checkins || []), ...(data.manual_checkins || [])]
        .map((c) => ({ ...c, _id: idOf(c), _ms: Date.parse(c.checked_out_at) || 0 }))
        .filter((c) => c._ms)
        .sort((a, b) => b._ms - a._ms);
    return rows.map((c) => ({
        ...c,
        name: nameOf(c),
        code: codeOf(c),
        confirmed: !!c.checked_out_confirmed_at,
        pill: pillFor(c._ms, !!c.checked_out_confirmed_at, now),
        overdue: !!c._ms && !c.checked_out_confirmed_at && (now - c._ms) >= 5 * 60000
    }));
}

// Cards in either markup: direct children holding a confirm checkbox.
async function cards(page) {
    return page.$$eval(CARD_SEL, (nodes) => nodes.map((card) => {
        const nameEl = card.querySelector('.font-bold');
        const codeEl = card.querySelector('.text-black');
        const pill = card.querySelector('.child-time');
        const box = card.querySelector('.child-confirmed-checkbox');
        const pillClass = (pill?.className || '').split(/\s+/).find((c) => /^bg-(gray-400|green-500|yellow-500|red-500)$/.test(c)) || null;
        return {
            name: (nameEl?.innerText || '').trim(),
            code: (codeEl?.innerText || '').trim(),
            pill: pillClass,
            checked: !!box?.checked
        };
    }));
}

async function filterLabels(page) {
    return page.$$eval('#location-group-checkboxes label', (nodes) =>
        nodes.map((l) => l.innerText.trim()).filter(Boolean));
}

async function badge(page) {
    return page.$eval('#overdue-badge', (el) => ({
        visible: el.style.display !== 'none' && !el.classList.contains('hidden'),
        text: (el.innerText || '').trim(),
        aria: el.getAttribute('aria-label') || ''
    })).catch(() => ({ visible: false, text: '', aria: '' }));
}

// Wait until the 3s poll has settled (card count stable across a poll).
async function settled(page) {
    await page.waitForFunction(
        (sel) => document.querySelectorAll(sel).length > 0, CARD_SEL, { timeout: 15000 });
    let last = -1;
    for (let i = 0; i < 4; i++) {
        const n = await page.$$eval(CARD_SEL, (els) => els.length);
        if (n === last) return;
        last = n;
        await sleep(3600);
    }
}

async function shot(page, side, step) {
    await page.screenshot({
        path: `${SHOTS}/${side}-${step}.png`,
        mask: [page.locator('#current-time'), page.locator('.child-time')]
    });
}

async function loginAndOpen(browser, side) {
    const page = await browser.newPage();
    const errors = [];
    page.on('pageerror', (err) => errors.push(`pageerror: ${err.message}`));
    page.on('console', (msg) => {
        if (msg.type() === 'error') errors.push(`console: ${msg.text()}`);
    });
    await page.goto(side.base + '/login');
    await page.selectOption('#username', 'admin');
    await page.fill('#password', PASSWORD);
    await Promise.all([
        page.waitForLoadState('load', { timeout: 10000 }),
        page.click('button[type="submit"]')
    ]);
    if (page.url().includes('/login')) {
        throw new Error(`login failed at ${side.base}`);
    }
    await page.goto(side.base + BOARD);
    await settled(page);
    return { page, errors };
}

async function runSide(browser, side) {
    const { page, errors } = await loginAndOpen(browser, side);
    const now = Date.now();
    const rows = await apiRows(page, now);

    // 1. initial render
    let list = await cards(page);
    check(side.name, 'initial', 'card count', list.length, rows.length);
    check(side.name, 'initial', 'names', list.map((c) => c.name), rows.map((c) => c.name));
    check(side.name, 'initial', 'codes', list.map((c) => c.code), rows.map((c) => c.code));
    check(side.name, 'initial', 'pills', list.map((c) => c.pill), rows.map((c) => c.pill));
    check(side.name, 'initial', 'filter labels', await filterLabels(page), ['Group A', 'Group B', 'Unassigned']);
    const overdueRows = rows.filter((c) => c.overdue);
    const b0 = await badge(page);
    check(side.name, 'initial', 'badge visible', b0.visible, overdueRows.length > 0);
    check(side.name, 'initial', 'badge text', b0.text,
        overdueRows.length > 0 ? `${overdueRows.length} overdue. Tap to view` : '');
    await shot(page, side.name, 'initial');

    // 2. search (panel starts collapsed)
    await page.click('#search-toggle-button');
    await page.fill('#search-input', 'overdue');
    await sleep(500);
    list = await cards(page);
    const expectSearch = rows.filter((c) =>
        c.name.toLowerCase().includes('overdue') || c.code.toLowerCase().includes('overdue'));
    check(side.name, 'search', 'cards for "overdue"', list.map((c) => c.name), expectSearch.map((c) => c.name));
    await shot(page, side.name, 'search');
    await page.fill('#search-input', '');
    await sleep(500);

    // 3. hide confirmed
    await setBox(page.locator('#hide-confirmed-toggle'), true);
    await sleep(500);
    list = await cards(page);
    const expectUnhidden = rows.filter((c) => !c.confirmed);
    check(side.name, 'hide-confirmed', 'names', list.map((c) => c.name), expectUnhidden.map((c) => c.name));
    await shot(page, side.name, 'hide-confirmed');
    await setBox(page.locator('#hide-confirmed-toggle'), false);
    await sleep(500);

    // 4. group filter: uncheck Group A -> only manuals (always visible)
    const groupBoxes = page.locator('#location-group-checkboxes input[type="checkbox"]');
    await setBox(groupBoxes.first(), false);
    await sleep(500);
    list = await cards(page);
    check(side.name, 'group-filter', 'Group A off shows manuals only', list.map((c) => c.name),
        rows.filter((c) => c.source === 'manual').map((c) => c.name));
    await shot(page, side.name, 'group-filter');
    await setBox(groupBoxes.first(), true);
    await sleep(500);

    // 5. overdue sheet
    const b1 = await badge(page);
    if (b1.visible) {
        await page.click('#overdue-badge');
        await sleep(500);
        const sheetOpen = await page.$eval('#overdue-sheet',
            (el) => !el.classList.contains('translate-y-full'));
        const sheetRows = await page.$$eval('#overdue-sheet-list > div:has(.child-confirmed-checkbox)', (els) => els.length);
        check(side.name, 'overdue', 'sheet opens', sheetOpen, true);
        check(side.name, 'overdue', 'sheet rows', sheetRows, overdueRows.length);
        await shot(page, side.name, 'sheet');
        await page.click('#overdue-sheet-close');
        await sleep(500);
    } else {
        check(side.name, 'overdue', 'sheet opens', 'skipped (no overdue)', 'skipped (no overdue)');
        check(side.name, 'overdue', 'sheet rows', 'skipped (no overdue)', 'skipped (no overdue)');
    }

    // 6. confirm flow on Fresh Kid (young, unconfirmed — deterministic)
    const fresh = rows.find((c) => c.name === 'Fresh Kid' && !c.confirmed);
    if (!fresh) {
        check(side.name, 'confirm', 'fresh row present', false, true);
    } else {
        const patchSeen = page.waitForRequest((req) =>
            req.url().includes('/checked_out_confirmed') && req.method() === 'PATCH', { timeout: 10000 });
        const card = page.locator(CARD_SEL, { hasText: 'Fresh Kid' });
        await setBox(card.locator('.child-confirmed-checkbox'), true);
        const req = await patchSeen;
        const urlOk = req.url().includes('/v1/checkins/') && req.url().endsWith('/checked_out_confirmed');
        let bodyOk = false;
        try { bodyOk = JSON.stringify(req.postDataJSON()) === JSON.stringify({ confirmed: true }); } catch { /* ignore */ }
        check(side.name, 'confirm', 'PATCH url', urlOk, true);
        check(side.name, 'confirm', 'PATCH body', bodyOk, true);
        await sleep(500);
        const pill = await card.locator('.child-time')
            .evaluate((el) => (el.className.split(/\s+/).find((c) => /^bg-/.test(c)) || null));
        check(side.name, 'confirm', 'pill turns gray', pill, 'bg-gray-400');
        const greenIcon = await card.locator('label').evaluate((label) =>
            label.getAttribute('data-confirmed-state') === 'confirmed' &&
            !!label.querySelector('img[data-confirmed-icon]'));
        check(side.name, 'confirm', 'icon green hooks', greenIcon, true);
        await shot(page, side.name, 'confirm');
    }

    check(side.name, 'console', 'no page errors', errors, []);
    await page.close();
}

const browser = await chromium.launch({ executablePath: BRAVE, headless: true });
try {
    for (const side of SIDES) await runSide(browser, side);
} finally {
    await browser.close();
}

// Cross-side parity: every check must match between main and alpine.
const byKey = new Map();
for (const r of results) {
    if (r.flow === 'console') continue;
    const key = `${r.flow}:${r.name}`;
    if (!byKey.has(key)) byKey.set(key, {});
    byKey.get(key)[r.side] = r.actual;
}
let parityFail = 0;
for (const [key, sides] of byKey) {
    const a = JSON.stringify(sides.main);
    const b = JSON.stringify(sides.alpine);
    if (a !== b) {
        parityFail++;
        console.log(`PARITY-FAIL ${key}\n  main:   ${a}\n  alpine: ${b}`);
    }
}
const failed = results.filter((r) => !r.ok).length;
console.log(`\n${results.length - failed}/${results.length} checks passed, parity ${parityFail === 0 ? 'OK' : 'FAILED'}`);
process.exit(failed || parityFail ? 1 : 0);
