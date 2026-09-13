import { describe, it, expect, vi } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const root = process.cwd();
const scriptPath = path.resolve(root, 'internal/web/static/pages/checkoutsv1/checkouts.js');
const htmlPath = path.resolve(root, 'internal/web/static/pages/checkoutsv1/checkouts.html');
const alpinePath = path.resolve(root, 'internal/web/static/js/alpine.min.js');
const script = fs.readFileSync(scriptPath, 'utf8');
const html = fs.readFileSync(htmlPath, 'utf8');
const alpineScript = fs.readFileSync(alpinePath, 'utf8');

// ---- eval harness (no Alpine): pure helpers + board factory ----

function loadWindow({ html: bodyHtml, url = 'http://localhost/', fetchImpl } = {}) {
    const dom = new JSDOM(bodyHtml || '<!doctype html><html><body></body></html>', {
        runScripts: 'dangerously',
        url
    });
    dom.window.fetch = fetchImpl || (async () => ({
        ok: true,
        json: async () => [],
        text: async () => ''
    }));
    dom.window.setInterval = () => 0;
    dom.window.requestAnimationFrame = () => 0;
    dom.window.scrollTo = () => {};
    dom.window.eval(script);
    return dom.window;
}

function pcChild(id, overrides = {}) {
    return {
        source: 'planning_center',
        planning_center_id: id,
        first_name: id,
        last_name: 'X',
        security_code: id,
        location_group_id: 1,
        checked_out_at: new Date().toISOString(),
        checked_out_confirmed_at: null,
        ...overrides
    };
}

function boardWith(w, children) {
    const board = w.checkoutsBoardData();
    board.children = children.map((c) => w.withChildMeta(c));
    return board;
}

// ---- shared waitFor: replaces brittle fixed sleeps ----
async function waitFor(fn, { timeout = 1000, interval = 10 } = {}) {
    const start = Date.now();
    let lastErr;
    while (Date.now() - start < timeout) {
        try {
            const res = await fn();
            if (res) return res;
        } catch (e) {
            lastErr = e;
        }
        await new Promise((r) => setTimeout(r, interval));
    }
    if (lastErr) throw lastErr;
    throw new Error(`waitFor timeout after ${timeout}ms`);
}

// ---- integration harness: real page + real Alpine ----

async function loadBoardPage({ url = 'http://localhost/', checkouts = [], groups = [], patchImpl } = {}) {
    const dom = new JSDOM(html, {
        url,
        runScripts: 'outside-only',
        pretendToBeVisual: true
    });
    const w = dom.window;
    const patches = [];
    w.fetch = async (input, init = {}) => {
        const target = String(input);
        if (init.method === 'PATCH') {
            patches.push({ url: target, body: init.body ? JSON.parse(init.body) : null });
            if (patchImpl) return patchImpl(target, init);
            return { ok: true, json: async () => ({}) };
        }
        if (target.includes('/v1/location_groups')) {
            return { ok: true, json: async () => groups };
        }
        return { ok: true, json: async () => checkouts };
    };
    w.setInterval = () => 0;
    w.scrollTo = () => {};
    const warns = [];
    w.console.warn = (msg) => { warns.push(String(msg)); };
    w.eval(alpineScript);
    w.eval(script);
    try {
        w.Alpine.start();
    } catch (e) { /* already initialized — registration still applied */ }
    w.document.dispatchEvent(new w.Event('DOMContentLoaded'));
    await waitFor(() => w.__checkoutsBoard != null, { timeout: 2000 });
    // let Alpine finish initial tick and fetchLocationGroups→fetchChildrenData chain
    await waitFor(() => w.__checkoutsBoard.groupsReady === true, { timeout: 2000 }).catch(() => {});
    // small extra tick for x-show/x-text bindings to flush
    await new Promise((r) => setTimeout(r, 20));
    return { window: w, board: w.__checkoutsBoard, patches, warns };
}

const flush = () => new Promise((r) => setTimeout(r, 20));
const nextTick = async (w) => {
    const board = w.__checkoutsBoard;
    if (board && board.$nextTick) {
        await new Promise((resolve) => board.$nextTick(resolve));
    }
    await new Promise((r) => setTimeout(r, 10));
};

describe('checkoutsv1/checkouts helpers', () => {
    it('builds child ids by source', () => {
        const window = loadWindow();
        expect(window.getChildId({ source: 'manual', public_id: '123' })).toBe('manual:123');
        expect(window.getChildId({ source: 'manual' })).toBe('');
        expect(window.getChildId({ source: 'planning_center', planning_center_id: 'pc-1' })).toBe('pc:pc-1');
        expect(window.getChildId({ planning_center_id: 'pc-2' })).toBe('pc:pc-2');
        expect(window.getChildId({ public_id: 'pub-1' })).toBe('public:pub-1');
    });

    it('normalizes checkout payloads into a single list', () => {
        const window = loadWindow();
        expect(window.normalizeCheckoutsResponse({
            checkins: [{ id: 'a' }],
            manual_checkins: [{ id: 'b' }]
        })).toEqual([{ id: 'a' }, { id: 'b' }]);
        expect(window.normalizeCheckoutsResponse({
            checkins: { checkins: [{ id: 'c' }] },
            manual_checkins: { checkins: [{ id: 'd' }] }
        })).toEqual([{ id: 'c' }, { id: 'd' }]);
    });

    it('parses checkout timestamps and minute labels', () => {
        const window = loadWindow();
        expect(window.getCheckedOutTimestamp('')).toBe(0);
        expect(window.getCheckedOutTimestamp('not-a-date')).toBe(0);
        const now = Date.now();
        expect(window.calculateMinutesAgoFromTimestamp(0, now)).toBe('0 min ago');
        expect(window.calculateMinutesAgoFromTimestamp(now - 3 * 60 * 1000, now)).toBe('3 min ago');
    });

    it('colors time pills by age and confirmation', () => {
        const window = loadWindow();
        const now = Date.now();
        expect(window.getTimePillClass(now - 60 * 1000, false, now)).toBe('bg-green-500');
        expect(window.getTimePillClass(now - 5 * 60 * 1000, false, now)).toBe('bg-yellow-500');
        expect(window.getTimePillClass(now - 9 * 60 * 1000, false, now)).toBe('bg-red-500');
        expect(window.getTimePillClass(now - 9 * 60 * 1000, true, now)).toBe('bg-gray-400');
    });

    it('colors location groups deterministically with gray fallback', () => {
        const window = loadWindow();
        expect(window.getLocationGroupColor(null)).toBe(window.GRAY_UNASSIGNED);
        expect(window.getLocationGroupColor('nope')).toBe(window.GRAY_UNASSIGNED);
        expect(window.getLocationGroupColor(1)).toBe(window.getLocationGroupColor(1));
        expect(window.getLocationGroupColor(1)).not.toBe(window.GRAY_UNASSIGNED);
    });

    it('reads the group filter from the URL, resolving names via groups', () => {
        const window = loadWindow({ url: 'http://localhost/?location_group_id=2&location_group_name=Grace' });
        const sel = window.getSelectedFromURL([{ id: 1, name: 'Grace' }, { id: 2, name: 'Ada' }]);
        expect(sel.names.has('Grace')).toBe(true);
        expect(sel.ids.has(1)).toBe(true);
        expect(sel.ids.has(2)).toBe(true);
        expect(sel.isEmpty).toBe(false);
    });

    it('marks an explicit empty filter', () => {
        const window = loadWindow({ url: 'http://localhost/?location_group_id=' });
        expect(window.getSelectedFromURL([]).isEmpty).toBe(true);
        const plain = loadWindow({ url: 'http://localhost/' });
        const sel = plain.getSelectedFromURL([]);
        expect(sel.isEmpty).toBe(false);
        expect(sel.ids.size).toBe(0);
    });

    it('attaches stable ids and numeric timestamps', () => {
        const window = loadWindow();
        const meta = window.withChildMeta(pcChild('a'));
        expect(meta._id).toBe('pc:a');
        expect(meta.checked_out_at_ms).toBeGreaterThan(0);
    });

    it('diffs arrivals against known ids', () => {
        const window = loadWindow();
        const kids = [pcChild('a'), pcChild('b')].map((c) => window.withChildMeta(c));
        const first = window.computeNewChildIds(kids, new Set());
        expect(first.appeared.size).toBe(0);
        const more = [...kids, window.withChildMeta(pcChild('c'))];
        const second = window.computeNewChildIds(more, first.current);
        expect(Array.from(second.appeared)).toEqual(['pc:c']);
    });

    it('filters visible children by confirmation, search, and groups', () => {
        const window = loadWindow();
        const kids = [
            window.withChildMeta(pcChild('a', { first_name: 'Amy', location_group_id: 1 })),
            window.withChildMeta(pcChild('b', { first_name: 'Bo', location_group_id: 2, checked_out_confirmed_at: '2024-01-01' })),
            window.withChildMeta({ source: 'manual', public_id: 'm1', first_name: 'Mo', location_group_id: null, checked_out_at: new Date().toISOString() })
        ];
        const confirmedById = new Map([['pc:a', false], ['pc:b', true], ['manual:m1', false]]);

        expect(window.filterVisibleChildren(kids, { confirmedById }).length).toBe(3);
        expect(window.filterVisibleChildren(kids, { confirmedById, hideConfirmed: true }).map((c) => c._id))
            .toEqual(['pc:a', 'manual:m1']);
        expect(window.filterVisibleChildren(kids, { confirmedById, searchQuery: 'amy' }).map((c) => c._id))
            .toEqual(['pc:a']);
        expect(window.filterVisibleChildren(kids, { confirmedById, filterEmpty: true })).toEqual([]);
        const grouped = window.filterVisibleChildren(kids, {
            confirmedById, filterActive: true, selectedIds: new Set([2]), includeUnassigned: false
        }).map((c) => c._id);
        expect(grouped).toEqual(['pc:b', 'manual:m1']);
        const unassigned = window.filterVisibleChildren(
            [window.withChildMeta(pcChild('c', { location_group_id: null }))],
            { confirmedById: new Map(), filterActive: true, selectedIds: new Set([1]), includeUnassigned: true }
        );
        expect(unassigned.length).toBe(1);
    });

    it('lists overdue checkouts oldest-first, skipping confirmed', () => {        const window = loadWindow();
        const now = Date.now();
        const old = (min) => new Date(now - min * 60 * 1000).toISOString();
        const kids = [
            window.withChildMeta(pcChild('fresh', { checked_out_at: old(1) })),
            window.withChildMeta(pcChild('late', { checked_out_at: old(9) })),
            window.withChildMeta(pcChild('later', { checked_out_at: old(6) })),
            window.withChildMeta(pcChild('done', { checked_out_at: old(9) }))
        ];
        const confirmedById = new Map([['pc:fresh', false], ['pc:late', false], ['pc:later', false], ['pc:done', true]]);
        expect(window.getOverdueList(kids, confirmedById, now).map((c) => c._id)).toEqual(['pc:late', 'pc:later']);
    });

    it('orders tied timestamps deterministically regardless of API row order', () => {
        const window = loadWindow();
        const stamp = new Date().toISOString();
        const make = (id) => window.withChildMeta(pcChild(id, { checked_out_at: stamp }));
        const forward = [make('a'), make('b'), make('c')];
        const backward = [...forward].reverse();
        expect(window.sortByCheckoutDesc(backward).map((c) => c._id))
            .toEqual(window.sortByCheckoutDesc(forward).map((c) => c._id));
        expect(window.sortByCheckoutAsc(backward).map((c) => c._id))
            .toEqual(window.sortByCheckoutAsc(forward).map((c) => c._id));
        expect(window.sortByCheckoutDesc(forward).map((c) => c._id)).toEqual(['pc:a', 'pc:b', 'pc:c']);
    });
});

describe('checkoutsv1/checkoutsBoard factory', () => {
    it('derives visible children and empty messages', () => {
        const w = loadWindow({ url: 'http://localhost/' });
        const board = boardWith(w, [pcChild('a', { first_name: 'Amy' }), pcChild('b', { first_name: 'Bo' })]);
        expect(board.visibleChildren.map((c) => c._id)).toEqual(['pc:a', 'pc:b']);

        board.searchQuery = 'zzz';
        expect(board.visibleChildren).toEqual([]);
        expect(board.emptyMessage).toBe('No matching children');

        board.searchQuery = '';
        board.hideConfirmed = true;
        board.children[0].checked_out_confirmed_at = '2024-01-01';
        expect(board.visibleChildren.map((c) => c._id)).toEqual(['pc:b']);
        expect(board.emptyMessage).toBe('No unconfirmed children');
    });

    it('applies an explicit empty group filter', () => {
        const w = loadWindow({ url: 'http://localhost/?location_group_id=' });
        const board = w.checkoutsBoardData();
        board.setGroups([{ id: 1, name: 'A' }]);
        expect(board.filterEmpty).toBe(true);
        board.children = [w.withChildMeta(pcChild('a'))];
        expect(board.visibleChildren).toEqual([]);
    });

    it('selects all groups by default with no URL filter', () => {
        const w = loadWindow({ url: 'http://localhost/' });
        const board = w.checkoutsBoardData();
        board.setGroups([{ id: 1, name: 'A' }, { id: 2, name: 'B' }]);
        expect(board.selected).toEqual(['1', '2']);
        expect(board.includeUnassigned).toBe(true);
        expect(board.selectAllLabel).toBe('Deselect all');
        expect(board.filterActive).toBe(false);
    });

    it('applyURL respects an explicit id filter', () => {
        const w = loadWindow({ url: 'http://localhost/?location_group_id=2' });
        const board = w.checkoutsBoardData();
        board.setGroups([{ id: 1, name: 'A' }, { id: 2, name: 'B' }]);
        expect(board.selected).toEqual(['2']);
        expect(board.filterActive).toBe(true);
        expect(board.selectAllLabel).toBe('Select all');
    });

    it('onFilterChange writes the selection to the URL', async () => {
        const w = loadWindow({
            html: '<!doctype html><html><body><div id="children-list"></div></body></html>',
            url: 'http://localhost/'
        });
        const board = w.checkoutsBoardData();
        board.setGroups([{ id: 1, name: 'A' }, { id: 2, name: 'B' }]);
        board.selected = ['2'];
        board.onFilterChange();
        expect(w.location.search).toContain('location_group_id=2');
        await board.fetchChildrenData();
    });

    it('toggleSelectAll selects all, then empties', async () => {
        const w = loadWindow({ url: 'http://localhost/?location_group_id=2' });
        const board = w.checkoutsBoardData();
        board.setGroups([{ id: 1, name: 'A' }, { id: 2, name: 'B' }]);
        board.toggleSelectAll();
        expect(w.location.search).not.toContain('location_group_id');
        expect(board.selectAllLabel).toBe('Deselect all');
        board.toggleSelectAll();
        expect(board.filterEmpty).toBe(true);
        await board.fetchChildrenData();
    });

    it('honors confirmation overrides until they expire', () => {
        const w = loadWindow();
        const board = boardWith(w, [pcChild('a')]);
        const child = board.children[0];
        expect(board.isConfirmed(child)).toBe(false);
        {
            const m = new Map(board.confirmationOverrides);
            m.set('pc:a', { confirmed: true, timestamp: Date.now() });
            board.confirmationOverrides = m;
        }
        expect(board.isConfirmed(child)).toBe(true);
        {
            const m = new Map(board.confirmationOverrides);
            m.set('pc:a', { confirmed: true, timestamp: Date.now() - 60 * 1000 });
            board.confirmationOverrides = m;
        }
        expect(board.isConfirmed(child)).toBe(false);
    });

    it('confirms a checkout with PATCH and tracks the override', async () => {
        const calls = [];
        const w = loadWindow({
            fetchImpl: async (url, init = {}) => {
                calls.push({ url: String(url), method: init.method, body: init.body });
                return { ok: true, json: async () => ({}) };
            }
        });
        const board = boardWith(w, [pcChild('abc')]);
        board.refreshOverdue();
        await board.onConfirmToggle(board.children[0], { target: { checked: true } });
        expect(calls).toHaveLength(1);
        expect(calls[0].url).toContain('/v1/checkins/abc/checked_out_confirmed');
        expect(JSON.parse(calls[0].body)).toEqual({ confirmed: true });
        expect(board.isConfirmed(board.children[0])).toBe(true);
    });

    it('reverts the override when PATCH fails', async () => {
        const w = loadWindow({
            fetchImpl: async () => ({ ok: false, status: 500, json: async () => ({}) })
        });
        const board = boardWith(w, [pcChild('abc')]);
        const box = { checked: true, disabled: false };
        await board.onConfirmToggle(board.children[0], { target: box });
        expect(board.isConfirmed(board.children[0])).toBe(false);
    });

    it('skips unknown sources without calling the API', async () => {
        let called = false;
        const w = loadWindow({
            fetchImpl: async () => { called = true; return { ok: true, json: async () => ({}) }; }
        });
        const board = boardWith(w, [{ source: 'walk-in', planning_center_id: 'x' }]);
        await board.onConfirmToggle(board.children[0], { target: { checked: true } });
        expect(called).toBe(false);
        expect(board.isConfirmed(board.children[0])).toBe(false);
    });

    it('flags new arrivals and reseeds the baseline when the filter changes', async () => {
        const w = loadWindow({
            url: 'http://localhost/',
            fetchImpl: async () => ({ ok: true, json: async () => [pcChild('a')] })
        });
        const board = w.checkoutsBoardData();
        await board.fetchChildrenData();
        expect(board.loading).toBe(false);
        expect(board.loadError).toBe(false);
        expect(board.flashIds.size).toBe(0);

        w.history.replaceState(null, '', '?location_group_id=1');
        await board.fetchChildrenData();
        expect(Array.from(board.flashIds)).toEqual([]);
    });

    it('marks fetch errors', async () => {
        const w = loadWindow({
            html: '<!doctype html><html><body><div id="children-list"></div></body></html>',
            fetchImpl: async () => ({ ok: false, status: 500, json: async () => ({}) })
        });
        const board = w.checkoutsBoardData();
        board.children = [w.withChildMeta(pcChild('a'))];
        const prevLen = board.children.length;
        await board.fetchChildrenData();
        expect(board.loadError).toBe(true);
        expect(board.children.length).toBe(prevLen);
    });

    it('retains confirmed overdue rows in the sheet until close', () => {
        const w = loadWindow();
        const old = new Date(Date.now() - 10 * 60 * 1000).toISOString();
        const board = boardWith(w, [pcChild('a', { checked_out_at: old })]);
        board.refreshOverdue();
        expect(board.badgeVisible).toBe(true);
        expect(board.sheetOverdue.map((c) => c._id)).toEqual(['pc:a']);

        board.openOverdueSheet();
        {
            const m = new Map(board.confirmationOverrides);
            m.set('pc:a', { confirmed: true, timestamp: Date.now() });
            board.confirmationOverrides = m;
        }
        board.refreshOverdue();
        expect(board.overdueChildren).toEqual([]);
        {
            const s = new Set(board.overdueRetainedIds);
            s.add('pc:a');
            board.overdueRetainedIds = s;
        }
        board.refreshOverdue();
        expect(board.sheetOverdue.map((c) => c._id)).toEqual(['pc:a']);

        board.closeOverdueSheet();
        expect(board.sheetOverdue).toEqual([]);
        expect(board.badgeVisible).toBe(false);
    });

    it('keeps drawer order stable when tied rows arrive in different orders', async () => {
        const stamp = new Date(Date.now() - 10 * 60 * 1000).toISOString();
        const tied = () => [pcChild('b', { checked_out_at: stamp }), pcChild('a', { checked_out_at: stamp })];
        let flip = false;
        const w = loadWindow({
            fetchImpl: async (url) => {
                if (String(url).includes('/v1/location_groups')) return { ok: true, json: async () => [] };
                const rows = tied();
                flip = !flip;
                return { ok: true, json: async () => (flip ? rows : [...rows].reverse()) };
            }
        });
        const board = w.checkoutsBoardData();
        board.openOverdueSheet();
        await board.fetchChildrenData();
        const first = board.sheetOverdue.map((c) => c._id);
        expect(first).toEqual(['pc:a', 'pc:b']);
        await board.onConfirmToggle(board.children.find((c) => c._id === 'pc:b'), { target: { checked: true } });
        await board.fetchChildrenData();
        await board.fetchChildrenData();
        expect(board.sheetOverdue.map((c) => c._id)).toEqual(first);
        board.destroy();
    });
});

describe('checkoutsv1/checkoutsBoard with Alpine', () => {
    it('boots without Alpine expression errors', async () => {
        const old = new Date(Date.now() - 10 * 60 * 1000).toISOString();
        const { board, warns } = await loadBoardPage({
            checkouts: [pcChild('Late', { checked_out_at: old })],
            groups: [{ id: 1, name: 'G' }]
        });
        const problems = warns.filter((m) => /Alpine Expression Error|Maximum recursive|undefined is not/i)
            .filter((m) => !/already been initialized/i.test(m));
        expect(problems).toEqual([]);
        board.destroy();
    });

    it('renders location groups from the API', async () => {
        const { window: w } = await loadBoardPage({
            groups: [{ id: 1, name: 'Group A' }, { id: 2, name: 'Group B' }]
        });
        const labels = [...w.document.querySelectorAll('#location-group-checkboxes label')]
            .map((l) => l.textContent.trim()).filter(Boolean);
        expect(labels).toEqual(['Group A', 'Group B', 'Unassigned']);
        w.__checkoutsBoard.destroy();
    });

    it('renders checkout cards and filters by search', async () => {
        const { window: w, board } = await loadBoardPage({
            checkouts: [pcChild('Amy'), pcChild('Bo')],
            groups: [{ id: 1, name: 'G' }]
        });
        expect(board.visibleChildren).toHaveLength(2);
        let cards = w.document.querySelectorAll('#children-list .child-card');
        expect(cards.length).toBe(2);
        expect(cards[0].textContent).toContain('Amy');
        const bar = cards[0].querySelector(':scope > div[aria-hidden="true"]');
        expect(bar.getAttribute('style')).toContain('background-color');
        expect(bar.getAttribute('style')).not.toContain('width');
        expect(bar.classList.contains('w-[6px]')).toBe(true);
        expect(bar.classList.contains('shrink-0')).toBe(true);

        const input = w.document.getElementById('search-input');
        input.value = 'bo';
        input.dispatchEvent(new w.Event('input', { bubbles: true }));
        await waitFor(() => w.document.querySelectorAll('#children-list .child-card').length === 1);
        cards = w.document.querySelectorAll('#children-list .child-card');
        expect(cards.length).toBe(1);
        expect(cards[0].textContent).toContain('Bo');
        board.destroy();
    });

    it('keeps DOM nodes stable across polls (focus/scroll safe)', async () => {
        const kid = pcChild('Amy');
        const { window: w, board } = await loadBoardPage({ checkouts: [kid] });
        const before = w.document.querySelector('#children-list .child-card[data-child-id="pc:Amy"]');
        expect(before).not.toBeNull();
        before.querySelector('input[type="checkbox"]').focus();
        await board.fetchChildrenData();
        await nextTick(w);
        const after = w.document.querySelector('#children-list .child-card[data-child-id="pc:Amy"]');
        expect(after).toBe(before);
        expect(w.document.activeElement).toBe(after.querySelector('input[type="checkbox"]'));
        board.destroy();
    });

    it('confirms via checkbox and turns the pill gray', async () => {
        const { window: w, board, patches } = await loadBoardPage({ checkouts: [pcChild('Amy')] });
        const box = w.document.querySelector('#children-list .child-confirmed-checkbox');
        box.checked = true;
        box.dispatchEvent(new w.Event('change', { bubbles: true }));
        await waitFor(() => patches.length === 1);
        expect(patches).toHaveLength(1);
        expect(patches[0].url).toContain('/v1/checkins/Amy/checked_out_confirmed');
        expect(patches[0].body).toEqual({ confirmed: true });
        expect(board.isConfirmed(board.children[0])).toBe(true);
        const pill = w.document.querySelector('#children-list .child-time');
        expect(pill.className).toContain('bg-gray-400');
        const label = w.document.querySelector('#children-list .child-card label');
        expect(label.getAttribute('data-confirmed-state')).toBe('confirmed');
        expect(label.querySelector('img[data-confirmed-icon]')).not.toBeNull();
        board.destroy();
    });

    it('shows the overdue badge and sheet for stale checkouts', async () => {
        const old = new Date(Date.now() - 10 * 60 * 1000).toISOString();
        const { window: w, board } = await loadBoardPage({
            checkouts: [pcChild('Late', { checked_out_at: old })]
        });
        const badge = w.document.getElementById('overdue-badge');
        expect(badge.textContent).toContain('1 overdue');
        expect(badge.style.display).not.toBe('none');
        expect(badge.getAttribute('aria-label')).toBe('1 overdue checkouts, tap to view');

        badge.dispatchEvent(new w.MouseEvent('click', { bubbles: true }));
        await waitFor(() => !w.document.getElementById('overdue-sheet').classList.contains('translate-y-full'));
        const sheet = w.document.getElementById('overdue-sheet');
        expect(sheet.classList.contains('translate-y-full')).toBe(false);
        expect(w.document.querySelectorAll('#overdue-sheet-list .child-card').length).toBe(1);

        w.document.getElementById('overdue-sheet-close')
            .dispatchEvent(new w.MouseEvent('click', { bubbles: true }));
        await waitFor(() => w.document.getElementById('overdue-sheet').classList.contains('translate-y-full'));
        expect(sheet.classList.contains('translate-y-full')).toBe(true);
        board.destroy();
    });

    it('hides confirmed children with the toggle', async () => {
        const { window: w, board } = await loadBoardPage({
            checkouts: [pcChild('Amy'), pcChild('Bo', { checked_out_confirmed_at: '2024-01-01T00:00:00Z' })]
        });
        expect(w.document.querySelectorAll('#children-list .child-card').length).toBe(2);
        const toggle = w.document.getElementById('hide-confirmed-toggle');
        toggle.checked = true;
        toggle.dispatchEvent(new w.Event('change', { bubbles: true }));
        await waitFor(() => w.document.querySelectorAll('#children-list .child-card').length === 1);
        const cards = w.document.querySelectorAll('#children-list .child-card');
        expect(cards.length).toBe(1);
        expect(cards[0].textContent).toContain('Amy');
        board.destroy();
    });

    // ---- F1 coverage: search toggle expand/focus/resize ----
    it('toggles search panel expanded state, aria, rotate and focus', async () => {
        const { window: w, board } = await loadBoardPage({ checkouts: [pcChild('Amy')] });
        const btn = w.document.getElementById('search-toggle-button');
        const controls = w.document.getElementById('search-controls');
        const icon = btn.querySelector('svg');
        const input = w.document.getElementById('search-input');

        // initial collapsed
        expect(board.searchOpen).toBe(false);
        expect(controls.classList.contains('is-expanded')).toBe(false);
        expect(btn.getAttribute('aria-expanded')).toBe('false');
        expect(controls.getAttribute('aria-hidden')).toBe('true');
        expect(icon.classList.contains('rotate-180')).toBe(false);

        btn.dispatchEvent(new w.MouseEvent('click', { bubbles: true }));
        await waitFor(() => board.searchOpen === true);
        await nextTick(w);
        expect(controls.classList.contains('is-expanded')).toBe(true);
        expect(btn.getAttribute('aria-expanded')).toBe('true');
        expect(controls.getAttribute('aria-hidden')).toBe('false');
        expect(icon.classList.contains('rotate-180')).toBe(true);
        expect(w.document.activeElement).toBe(input);

        btn.dispatchEvent(new w.MouseEvent('click', { bubbles: true }));
        await waitFor(() => board.searchOpen === false);
        await nextTick(w);
        expect(controls.classList.contains('is-expanded')).toBe(false);
        expect(btn.getAttribute('aria-expanded')).toBe('false');
        expect(controls.getAttribute('aria-hidden')).toBe('true');
        expect(icon.classList.contains('rotate-180')).toBe(false);

        board.destroy();
    });

    it('onResize preserves clamped height while search panel is expanded', async () => {
        const { window: w, board } = await loadBoardPage({ checkouts: [pcChild('Amy')] });
        const controls = w.document.getElementById('search-controls');
        // mock scrollHeight for expanded panel
        Object.defineProperty(controls, 'scrollHeight', { value: 123, configurable: true });
        Object.defineProperty(controls, 'offsetHeight', { value: 123, configurable: true });

        // open
        w.document.getElementById('search-toggle-button').dispatchEvent(new w.MouseEvent('click', { bubbles: true }));
        await waitFor(() => board.searchOpen === true);
        await nextTick(w);
        const expandedHeight = controls.style.height;
        expect(expandedHeight).toBe('123px');

        // resize while open should re-clamp to scrollHeight
        Object.defineProperty(controls, 'scrollHeight', { value: 200, configurable: true });
        board.onResize();
        expect(controls.style.height).toBe('200px');

        // close
        w.document.getElementById('search-toggle-button').dispatchEvent(new w.MouseEvent('click', { bubbles: true }));
        await waitFor(() => board.searchOpen === false);
        await nextTick(w);
        // while closed, onResize should not force expanded height
        controls.style.height = '0px';
        board.onResize();
        expect(controls.style.height).toBe('0px');

        board.destroy();
    });

    // ---- XSS via x-text ----
    it('escapes checkout names via x-text and contains no x-html', async () => {
        const evil = pcChild('<img src=x onerror=alert(1)>', { first_name: '<img src=x onerror=alert(1)>', last_name: '"><svg onload=alert(1)>', security_code: '<script>' });
        const { window: w, board } = await loadBoardPage({ checkouts: [evil] });
        await waitFor(() => w.document.querySelectorAll('#children-list .child-card').length === 1);
        const card = w.document.querySelector('#children-list .child-card');
        // x-text ensures the raw HTML is escaped: no extra img/svg element should be injected
        expect(card.querySelector('img[src="x"]')).toBeNull();
        expect(card.querySelector('svg:not([viewBox])')).toBeNull(); // svg injection would be without viewBox
        expect(card.querySelectorAll('img[data-confirmed-icon]').length).toBe(1);
        expect(card.textContent).toContain('<img src=x onerror=alert(1)>');
        expect(card.textContent).toContain('"><svg');
        // displayName span should have escaped markup via textContent, not innerHTML injection
        const nameSpan = card.querySelector('span[x-text="displayName(child)"]');
        expect(nameSpan.textContent).toContain('<img src=x onerror=alert(1)>');
        // static grep: checkouts.html must have zero x-html
        expect(html).not.toContain('x-html');
        board.destroy();
    });

    // ---- Jiggle on increase only ----
    it('jiggles badge only when overdue count increases', async () => {
        const old = (min) => new Date(Date.now() - min * 60 * 1000).toISOString();
        // start with 0 overdue
        const { window: w, board } = await loadBoardPage({ checkouts: [pcChild('Fresh', { checked_out_at: new Date().toISOString() })] });
        const badge = w.document.getElementById('overdue-badge');
        // ensure badge starts hidden
        expect(badge.style.display === 'none' || board.badgeVisible === false).toBeTruthy();

        // push 1 overdue → should jiggle
        board.children = [w.withChildMeta(pcChild('Late1', { checked_out_at: old(6) }))];
        board.tick(); // triggers refreshOverdue + jiggle check
        await nextTick(w);
        expect(badge.classList.contains('overdue-badge-jiggle')).toBe(true);
        badge.classList.remove('overdue-badge-jiggle');

        // same count again → no jiggle
        board.tick();
        await nextTick(w);
        expect(badge.classList.contains('overdue-badge-jiggle')).toBe(false);

        // increase to 2 → jiggle again
        board.children = [w.withChildMeta(pcChild('Late1', { checked_out_at: old(6) })), w.withChildMeta(pcChild('Late2', { checked_out_at: old(7) }))];
        board.tick();
        await nextTick(w);
        expect(badge.classList.contains('overdue-badge-jiggle')).toBe(true);
        badge.classList.remove('overdue-badge-jiggle');

        // decrease to 1 → no jiggle
        board.children = [w.withChildMeta(pcChild('Late1', { checked_out_at: old(6) }))];
        board.tick();
        await nextTick(w);
        expect(badge.classList.contains('overdue-badge-jiggle')).toBe(false);

        board.destroy();
    });

    // ---- Backdrop click + scroll-lock ----
    it('backdrop click closes sheet and scroll-lock locks body', async () => {
        const old = new Date(Date.now() - 10 * 60 * 1000).toISOString();
        const { window: w, board } = await loadBoardPage({ checkouts: [pcChild('Late', { checked_out_at: old })] });
        const backdrop = w.document.getElementById('overdue-sheet-backdrop');
        const badge = w.document.getElementById('overdue-badge');

        // open via method (also tests lockBodyScroll)
        board.openOverdueSheet();
        await waitFor(() => board.sheetOpen === true);
        await nextTick(w);
        // syncScrollLock is called via x-effect, but also ensure body is locked directly
        // In JSDOM scrollY is 0; check position fixed
        expect(w.document.body.style.position).toBe('fixed');
        expect(backdrop.style.display).not.toBe('none');

        // click backdrop should close and unlock
        backdrop.dispatchEvent(new w.MouseEvent('click', { bubbles: true }));
        await waitFor(() => board.sheetOpen === false);
        await nextTick(w);
        // Need to wait for syncScrollLock effect to run; also call manually to ensure
        board.syncScrollLock();
        expect(w.document.body.style.position).toBe('');
        expect(w.document.getElementById('overdue-sheet').classList.contains('translate-y-full')).toBe(true);

        // also test direct lock/unlock via syncScrollLock toggle
        board.sheetOpen = true;
        board.syncScrollLock();
        expect(w.document.body.style.position).toBe('fixed');
        board.sheetOpen = false;
        board.syncScrollLock();
        expect(w.document.body.style.position).toBe('');

        // restore
        board.destroy();
        w.document.body.style.position = '';
        w.document.body.style.top = '';
        w.document.body.style.overflow = '';
    });

    it('syncScrollLock is idempotent', async () => {
        const { window: w, board } = await loadBoardPage({ checkouts: [] });
        board.sheetOpen = false;
        board.syncScrollLock();
        expect(board.scrollState.locked).toBe(false);
        board.sheetOpen = true;
        board.syncScrollLock();
        expect(board.scrollState.locked).toBe(true);
        const firstTop = w.document.body.style.top;
        board.syncScrollLock();
        expect(w.document.body.style.top).toBe(firstTop);
        board.sheetOpen = false;
        board.syncScrollLock();
        expect(board.scrollState.locked).toBe(false);
        board.destroy();
    });

    // ---- Sheet sync: confirm via sheet flips both lists ----
    it('confirm via sheet checkbox flips pill in both main list and sheet', async () => {
        const old = new Date(Date.now() - 10 * 60 * 1000).toISOString();
        const { window: w, board } = await loadBoardPage({ checkouts: [pcChild('Late', { checked_out_at: old })] });
        // open sheet
        w.document.getElementById('overdue-badge').dispatchEvent(new w.MouseEvent('click', { bubbles: true }));
        await waitFor(() => board.sheetOpen === true);
        await nextTick(w);
        const mainPillBefore = w.document.querySelector('#children-list .child-time');
        const sheetPillBefore = w.document.querySelector('#overdue-sheet-list .child-time');
        expect(mainPillBefore.className).toContain('bg-red-500');
        expect(sheetPillBefore.className).toContain('bg-red-500');

        const sheetBox = w.document.querySelector('#overdue-sheet-list .child-confirmed-checkbox');
        sheetBox.checked = true;
        sheetBox.dispatchEvent(new w.Event('change', { bubbles: true }));
        await waitFor(() => board.isConfirmed(board.children[0]) === true);
        await nextTick(w);
        await waitFor(() => w.document.querySelector('#children-list .child-time').className.includes('bg-gray-400'));
        const mainPillAfter = w.document.querySelector('#children-list .child-time');
        const sheetPillAfter = w.document.querySelector('#overdue-sheet-list .child-time');
        expect(mainPillAfter.className).toContain('bg-gray-400');
        expect(sheetPillAfter.className).toContain('bg-gray-400');
        board.destroy();
    });

    // ---- Error / loading / empty DOM states ----
    it('shows error DOM and hides loading on fetch failure, empty shows correct variant', async () => {
        // Error case: fetch fails
        const dom = new JSDOM(html, { url: 'http://localhost/', runScripts: 'outside-only', pretendToBeVisual: true });
        const w = dom.window;
        w.fetch = async (input) => {
            const target = String(input);
            if (target.includes('/v1/location_groups')) return { ok: true, json: async () => [] };
            return { ok: false, status: 500, json: async () => ({}) };
        };
        w.setInterval = () => 0;
        w.scrollTo = () => {};
        w.eval(alpineScript);
        w.eval(script);
        try { w.Alpine.start(); } catch {}
        w.document.dispatchEvent(new w.Event('DOMContentLoaded'));
        await waitFor(() => w.__checkoutsBoard != null, { timeout: 2000 });
        const board = w.__checkoutsBoard;
        // wait for loadError to be set
        await waitFor(() => board.loadError === true, { timeout: 2000 });
        await nextTick(w);
        // loading should be false, error div visible, loading div hidden
        expect(board.loading).toBe(false);
        expect(board.loadError).toBe(true);
        const loadingEl = w.document.querySelector('[x-show="loading"]');
        const errorEl = w.document.querySelector('[x-show="loadError"]');
        // Alpine hides via display:none
        expect(loadingEl.style.display).toBe('none');
        expect(errorEl.style.display).not.toBe('none');
        board.destroy();
    });

    it('empty DOM shows correct variant per state', async () => {
        // no children, no filter -> "No children called yet"
        const { window: w1, board: b1 } = await loadBoardPage({ checkouts: [], groups: [] });
        await waitFor(() => b1.loading === false);
        await nextTick(w1);
        const empty1 = w1.document.querySelector('[x-text="emptyMessage"]');
        expect(b1.emptyMessage).toBe('No children called yet');
        expect(empty1.textContent).toContain('No children called yet');
        b1.destroy();

        // search with no match -> "No matching children"
        const { window: w2, board: b2 } = await loadBoardPage({ checkouts: [pcChild('Amy')], groups: [] });
        b2.searchQuery = 'zzz-nope';
        await nextTick(w2);
        // need Alpine to update visibleChildren and emptyMessage
        await waitFor(() => b2.visibleChildren.length === 0);
        expect(b2.emptyMessage).toBe('No matching children');
        const empty2 = w2.document.querySelector('[x-text="emptyMessage"]');
        expect(empty2.textContent).toContain('No matching children');
        b2.destroy();

        // hideConfirmed empties board -> "No unconfirmed children"
        const { window: w3, board: b3 } = await loadBoardPage({ checkouts: [pcChild('Only', { checked_out_confirmed_at: '2024-01-01T00:00:00Z' })], groups: [] });
        b3.hideConfirmed = true;
        await nextTick(w3);
        await waitFor(() => b3.visibleChildren.length === 0);
        expect(b3.emptyMessage).toBe('No unconfirmed children');
        const empty3 = w3.document.querySelector('[x-text="emptyMessage"]');
        expect(empty3.textContent).toContain('No unconfirmed children');
        b3.destroy();
    });

    it('truncation footer shows when >100 filtered children', async () => {
        const many = Array.from({ length: 101 }, (_, i) => pcChild(`K${String(i).padStart(3, '0')}`));
        const { window: w, board } = await loadBoardPage({ checkouts: many });
        await waitFor(() => board.visibleChildren.length === 100);
        expect(board.isTruncated).toBe(true);
        expect(board.truncationText).toBe('Showing 100 of 101');
        await nextTick(w);
        const truncEl = w.document.querySelector('[x-text="truncationText"]');
        expect(truncEl.textContent).toContain('Showing 100 of 101');
        expect(truncEl.style.display).not.toBe('none');
        board.destroy();
    });
});

// ---- Direct unit tests for tick/tickClock/sweepOverrides and preview semantics ----

describe('checkoutsv1/tick and preview semantics', () => {
    it('tickClock advances nowMs and formats clock', () => {
        const w = loadWindow();
        const board = boardWith(w, []);
        const fixed = new Date('2024-06-15T14:30:00Z').getTime();
        const origNow = w.Date.now;
        const origGlobalNow = Date.now;
        w.Date.now = () => fixed;
        Date.now = () => fixed;
        board.tickClock();
        expect(board.nowMs).toBe(fixed);
        expect(board.clock).toMatch(/\d{2}:\d{2}/);
        w.Date.now = origNow;
        Date.now = origGlobalNow;
    });

    it('sweepOverrides removes expired entries and returns changed', () => {
        const w = loadWindow();
        const board = boardWith(w, [pcChild('a'), pcChild('b')]);
        const now = Date.now();
        const origNow = w.Date.now;
        const origGlobalNow = Date.now;
        w.Date.now = () => now;
        Date.now = () => now;
        board.confirmationOverrides = new Map([
            ['pc:a', { confirmed: true, timestamp: now - 20000 }], // expired (>15s)
            ['pc:b', { confirmed: true, timestamp: now - 1000 }] // fresh
        ]);
        const changed = board.sweepOverrides();
        expect(changed).toBe(true);
        expect(board.confirmationOverrides.has('pc:a')).toBe(false);
        expect(board.confirmationOverrides.has('pc:b')).toBe(true);
        // second sweep with no expired should return false and not reassign
        const beforeRef = board.confirmationOverrides;
        const changed2 = board.sweepOverrides();
        expect(changed2).toBe(false);
        expect(board.confirmationOverrides).toBe(beforeRef);
        w.Date.now = origNow;
        Date.now = origGlobalNow;
    });

    it('sweepOverrides is no-op when no overrides', () => {
        const w = loadWindow();
        const board = boardWith(w, []);
        expect(board.sweepOverrides()).toBe(false);
    });

    it('tick calls tickClock, sweepOverrides and refreshOverdue', () => {
        const w = loadWindow();
        const old = new Date(Date.now() - 10 * 60 * 1000).toISOString();
        const board = boardWith(w, [pcChild('a', { checked_out_at: old })]);
        let clockCalled = false;
        let sweepCalled = false;
        let refreshCalled = false;
        const origTickClock = board.tickClock.bind(board);
        const origSweep = board.sweepOverrides.bind(board);
        const origRefresh = board.refreshOverdue.bind(board);
        board.tickClock = () => { clockCalled = true; origTickClock(); };
        board.sweepOverrides = () => { sweepCalled = true; return origSweep(); };
        board.refreshOverdue = () => { refreshCalled = true; origRefresh(); };
        board.tick();
        expect(clockCalled).toBe(true);
        expect(sweepCalled).toBe(true);
        expect(refreshCalled).toBe(true);
    });

    it('getBackoffMs exponential backoff', () => {
        const w = loadWindow();
        expect(w.getBackoffMs(1)).toBe(3000);
        expect(w.getBackoffMs(2)).toBe(6000);
        expect(w.getBackoffMs(3)).toBe(12000);
        expect(w.getBackoffMs(10)).toBe(30000);
    });

    it('previewChildren preserves authored order (not sorted)', () => {
        const w = loadWindow();
        const board = w.checkoutsBoardData();
        const raw = [
            { source: 'planning_center', planning_center_id: 'z', first_name: 'Z', checked_out_at: new Date(Date.now() - 1 * 60 * 1000).toISOString(), location_group_id: 1 },
            { source: 'planning_center', planning_center_id: 'a', first_name: 'A', checked_out_at: new Date(Date.now() - 10 * 60 * 1000).toISOString(), location_group_id: 1 },
            { source: 'planning_center', planning_center_id: 'm', first_name: 'M', checked_out_at: new Date(Date.now() - 5 * 60 * 1000).toISOString(), location_group_id: 1 }
        ];
        board.previewChildren(raw);
        expect(board.children.map((c) => c.planning_center_id)).toEqual(['z', 'a', 'm']);
        expect(board.fetchBlocked).toBe(true);
        expect(board.previewMode).toBe(true);
        if (board.previewTimeoutId) clearTimeout(board.previewTimeoutId);
        board.clearPreview();
    });

    it('fetchBlocked blocks fetchChildrenData unless forced, and clearPreview restores', async () => {
        const w = loadWindow();
        let calls = 0;
        w.fetch = async () => { calls++; return { ok: true, json: async () => [] }; };
        const board = w.checkoutsBoardData();
        board.previewChildren([pcChild('x')]);
        expect(board.fetchBlocked).toBe(true);
        await board.fetchChildrenData();
        expect(calls).toBe(0);
        await board.fetchChildrenData({ force: true });
        expect(calls).toBe(1);
        board.clearPreview();
        expect(board.fetchBlocked).toBe(false);
        await board.fetchChildrenData();
        expect(calls).toBe(2);
        if (board.previewTimeoutId) clearTimeout(board.previewTimeoutId);
        board.destroy();
    });

    it('board.init gates first fetch on groups and uses waitFor semantics', async () => {
        // This test routes via real board.init() + Alpine.start() path, using fake fetch.
        const dom = new JSDOM(html, {
            url: 'http://localhost/',
            runScripts: 'outside-only',
            pretendToBeVisual: true
        });
        const w = dom.window;
        const fetched = [];
        w.fetch = async (input) => {
            const t = String(input);
            fetched.push(t);
            if (t.includes('/v1/location_groups')) {
                return { ok: true, json: async () => [{ id: 1, name: 'Grace' }] };
            }
            return { ok: true, json: async () => [pcChild('InitKid')] };
        };
        w.setInterval = () => 99;
        w.scrollTo = () => {};
        w.eval(alpineScript);
        w.eval(script);
        try { w.Alpine.start(); } catch {}
        w.document.dispatchEvent(new w.Event('DOMContentLoaded'));
        const board = await waitFor(() => w.__checkoutsBoard, { timeout: 2000 });
        // wait for groupsReady and children populated via init's gated fetch
        await waitFor(() => board.groupsReady === true, { timeout: 2000 });
        await waitFor(() => board.children.length === 1, { timeout: 2000 });
        expect(board.visibleChildren.map((c) => c.first_name)).toContain('InitKid');
        expect(fetched.some((u) => u.includes('/v1/location_groups'))).toBe(true);
        expect(fetched.some((u) => u.includes('/v1/checkins/checkouts'))).toBe(true);
        board.destroy();
    });
});
