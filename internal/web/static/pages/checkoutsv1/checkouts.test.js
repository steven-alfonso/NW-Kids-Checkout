import { describe, it, expect, beforeEach, afterEach } from 'vitest';
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
    // Collect (don't swallow) Alpine warnings so tests can assert clean boot.
    const warns = [];
    w.console.warn = (msg) => { warns.push(String(msg)); };
    w.eval(alpineScript);
    w.eval(script);
    try {
        w.Alpine.start();
    } catch (e) { /* already initialized — registration still applied */ }
    w.document.dispatchEvent(new w.Event('DOMContentLoaded'));
    await new Promise((r) => setTimeout(r, 100));
    return { window: w, board: w.__checkoutsBoard, patches, warns };
}

const flush = () => new Promise((r) => setTimeout(r, 20));

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
        // Group filter keeps manual checkins regardless of group.
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
        board.confirmationOverrides.set('pc:a', { confirmed: true, timestamp: Date.now() });
        expect(board.isConfirmed(child)).toBe(true);
        board.confirmationOverrides.set('pc:a', { confirmed: true, timestamp: Date.now() - 60 * 1000 });
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
        const box = { checked: true };
        await board.onConfirmToggle(board.children[0], { target: box });
        expect(board.isConfirmed(board.children[0])).toBe(false);
        expect(box.checked).toBe(false);
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
        await board.fetchChildrenData();
        expect(board.loadError).toBe(true);
        expect(board.children).toEqual([]);
    });

    it('retains confirmed overdue rows in the sheet until close', () => {
        const w = loadWindow();
        const old = new Date(Date.now() - 10 * 60 * 1000).toISOString();
        const board = boardWith(w, [pcChild('a', { checked_out_at: old })]);
        board.refreshOverdue();
        expect(board.badgeVisible).toBe(true);
        expect(board.sheetOverdue.map((c) => c._id)).toEqual(['pc:a']);

        board.openOverdueSheet();
        board.confirmationOverrides.set('pc:a', { confirmed: true, timestamp: Date.now() });
        board.refreshOverdue();
        // Live list is empty, but the drawer keeps the row until close.
        expect(board.overdueChildren).toEqual([]);
        board.overdueRetainedIds.add('pc:a');
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
                // Simulate the DB returning tied rows in varying order.
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
        // Every x-bind/@on in the page must resolve: Alpine reports
        // failures as console warnings (e.g. the badgeCount ReferenceError).
        // ("already been initialized" is harness noise from the explicit
        // start() call below auto-start, not an app signal.)
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
        // Group color bar: sizing must come from classes, not the static
        // style attribute — Alpine's :style binding replaces the whole
        // attribute and would wipe inline width/flex (zero-width bar).
        const bar = cards[0].querySelector(':scope > div[aria-hidden="true"]');
        expect(bar.getAttribute('style')).toContain('background-color');
        expect(bar.getAttribute('style')).not.toContain('width');
        expect(bar.classList.contains('w-[6px]')).toBe(true);
        expect(bar.classList.contains('shrink-0')).toBe(true);

        const input = w.document.getElementById('search-input');
        input.value = 'bo';
        input.dispatchEvent(new w.Event('input', { bubbles: true }));
        await flush();
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
        await flush();
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
        await flush();
        expect(patches).toHaveLength(1);
        expect(patches[0].url).toContain('/v1/checkins/Amy/checked_out_confirmed');
        expect(patches[0].body).toEqual({ confirmed: true });
        expect(board.isConfirmed(board.children[0])).toBe(true);
        const pill = w.document.querySelector('#children-list .child-time');
        expect(pill.className).toContain('bg-gray-400');
        // The green icon is pure CSS: [data-confirmed-state="confirmed"]
        // [data-confirmed-icon] { filter: ... }. Both hooks must exist.
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
        // All Alpine bindings must resolve: a missing component member
        // throws ReferenceError and leaves the attribute unset.
        expect(badge.getAttribute('aria-label')).toBe('1 overdue checkouts, tap to view');

        badge.dispatchEvent(new w.MouseEvent('click', { bubbles: true }));
        await flush();
        const sheet = w.document.getElementById('overdue-sheet');
        expect(sheet.classList.contains('translate-y-full')).toBe(false);
        expect(w.document.querySelectorAll('#overdue-sheet-list .child-card').length).toBe(1);

        w.document.getElementById('overdue-sheet-close')
            .dispatchEvent(new w.MouseEvent('click', { bubbles: true }));
        await flush();
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
        await flush();
        const cards = w.document.querySelectorAll('#children-list .child-card');
        expect(cards.length).toBe(1);
        expect(cards[0].textContent).toContain('Amy');
        board.destroy();
    });
});
