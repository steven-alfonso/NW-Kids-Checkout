import { describe, it, expect } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const scriptPath = path.resolve(process.cwd(), 'internal/web/static/pages/checkoutsv1/checkouts.js');
const script = fs.readFileSync(scriptPath, 'utf8');
const morphdomScriptPath = path.resolve(process.cwd(), 'internal/web/static/js/morphdom-umd.min.js');
const morphdomScript = fs.readFileSync(morphdomScriptPath, 'utf8');
const exposeInternals = `
window.__test = {
    setChildrenData: (value) => { childrenData = value; },
    setDom: () => {
        dom.childrenList = document.getElementById('children-list');
    },
    syncConfirmedStates: () => syncConfirmedStates(),
    updateUI: () => updateUI(),
    // The real 1s tick, rather than updateTimes() alone: exercising the tick is
    // what production actually does between polls.
    updateAllTimes: () => updateAllTimes(),
    setConfirmationOverride: (childId, confirmed) => setConfirmationOverride(childId, confirmed),
    setSearchQuery: (query) => setSearchQuery(query),
    setHideConfirmed: (hidden) => setHideConfirmed(hidden),
    getVisibleChildren: () => getVisibleChildren(),
    getChildrenData: () => childrenData,
    computeNewChildIds: (children) => computeNewChildIds(children),
    getFlashChildIds: () => flashChildIds,
    setFlashChildIds: (ids) => { flashChildIds = ids; },
    clearFlashStyles: () => clearFlashStyles()
};
`;

function loadWindow({ html, url = 'http://localhost/', fetchImpl } = {}) {
    const dom = new JSDOM(html || '<!doctype html><html><body></body></html>', {
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
    dom.window.eval(morphdomScript);
    dom.window.eval(`${script}\n${exposeInternals}`);
    return dom.window;
}

function pcChild(id) {
    return {
        source: 'planning_center',
        planning_center_id: id,
        first_name: id,
        last_name: 'X',
        security_code: id,
        location_group_id: 1,
        checked_out_at: new Date().toISOString()
    };
}

function childrenListWindow() {
    return loadWindow({ html: '<!doctype html><html><body><div id="children-list"></div></body></html>' });
}

function locationGroupWindow() {
    return loadWindow({ html: `<!doctype html><html><body>
        <div id="children-list"></div>
        <div id="location-group-filter">
            <div class="flex items-center justify-between mb-2">
                <span class="text-sm font-medium text-slate-700">Location groups</span>
                <button id="location-group-select-all" type="button">Select all</button>
            </div>
            <div id="location-group-checkboxes"></div>
        </div>
    </body></html>` });
}

function cardFor(window, id) {
    return window.document.querySelector(`.child-time[data-child-id="${id}"]`).closest('.bg-white.rounded-lg');
}

function boardWithFlashedChild(window) {
    window.__test.setChildrenData([pcChild('a'), pcChild('b')]);
    window.__test.updateUI();
    window.__test.setChildrenData([pcChild('d'), pcChild('a'), pcChild('b')]);
    window.__test.setFlashChildIds(new Set(['pc:d']));
    window.__test.updateUI();
}

describe('checkoutsv1/checkouts', () => {
    it('exposes core helper functions', () => {
        const window = loadWindow();
        expect(typeof window.getChildId).toBe('function');
        expect(typeof window.normalizeCheckoutsResponse).toBe('function');
        expect(typeof window.getCheckedOutTimestamp).toBe('function');
        expect(typeof window.calculateMinutesAgoFromTimestamp).toBe('function');
        expect(typeof window.renderChildren).toBe('function');
    });

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
        const combined = window.normalizeCheckoutsResponse({
            checkins: [{ id: 'a' }],
            manual_checkins: [{ id: 'b' }]
        });
        expect(combined).toEqual([{ id: 'a' }, { id: 'b' }]);

        const nested = window.normalizeCheckoutsResponse({
            checkins: { checkins: [{ id: 'c' }] },
            manual_checkins: { checkins: [{ id: 'd' }] }
        });
        expect(nested).toEqual([{ id: 'c' }, { id: 'd' }]);
    });

    it('parses timestamps and formats minutes ago', () => {
        const window = loadWindow();
        const ts = window.getCheckedOutTimestamp('2024-01-01T00:00:00Z');
        expect(ts).toBe(Date.parse('2024-01-01T00:00:00Z'));
        expect(window.getCheckedOutTimestamp('not-a-date')).toBe(0);
        expect(window.calculateMinutesAgoFromTimestamp(0, 123)).toBe('0 min ago');
        expect(window.calculateMinutesAgoFromTimestamp(2 * 60 * 1000, 5 * 60 * 1000)).toBe('3 min ago');
    });

    it('renders children with escaped text, ids, manual star, and timing', () => {
        const window = loadWindow();
        const html = window.renderChildren([
            {
                first_name: '<Ada>',
                last_name: 'Lovelace',
                security_code: '1234',
                checked_out_at_ms: 3 * 60 * 1000,
                checked_out_confirmed_at: '2024-01-01T00:05:00Z',
                planning_center_id: 'pc-1',
                public_id: 'pub-1',
                source: 'manual'
            },
            {
                first_name: 'Sam',
                last_name: '<Test>',
                security_code: '9999',
                checked_out_at_ms: 60 * 1000,
                planning_center_id: '11',
                source: 'planning_center'
            }
        ], 5 * 60 * 1000);

        expect(html).toContain('&lt;Ada&gt;');
        expect(html).toContain('/static/img/star.svg');
        expect(html).toContain('data-confirmed-state="confirmed"');
        expect(html).toContain('data-child-id="manual:pub-1"');
        expect(html).toContain('---');
        expect(html).toContain('data-child-id="pc:11"');
        expect(html).toContain('&lt;Test&gt;');
        expect(html).toContain('2 min ago');
    });

    it('steps pill color green to yellow at 4 min and red at 8 min', () => {
        const window = loadWindow();
        const base = 1000;
        const at = (minutes) => base + minutes * 60 * 1000;
        expect(window.getTimePillClass(base, false, at(0))).toBe('bg-green-500');
        expect(window.getTimePillClass(base, false, at(3))).toBe('bg-green-500');
        expect(window.getTimePillClass(base, false, at(4))).toBe('bg-yellow-500');
        expect(window.getTimePillClass(base, false, at(7))).toBe('bg-yellow-500');
        expect(window.getTimePillClass(base, false, at(8))).toBe('bg-red-500');
        expect(window.getTimePillClass(base, false, at(30))).toBe('bg-red-500');
    });

    it('uses gray for confirmed checkouts and green when no timestamp', () => {
        const window = loadWindow();
        expect(window.getTimePillClass(0, true, 30 * 60 * 1000)).toBe('bg-gray-400');
        expect(window.getTimePillClass(0, true, 0)).toBe('bg-gray-400');
        expect(window.getTimePillClass(0, false, Date.now())).toBe('bg-green-500');
    });

    it('swaps the pill class when confirmed state changes', () => {
        const html = `<!doctype html>
            <html>
                <body>
                    <div id="children-list">
                        <div class="child-time bg-green-500" data-child-id="pc:11">0 min ago</div>
                    </div>
                </body>
            </html>`;
        const window = loadWindow({ html });
        const pill = window.document.querySelector('.child-time');
        const child = {
            source: 'planning_center',
            planning_center_id: '11',
            checked_out_at_ms: 1000,
            checked_out_confirmed_at: null
        };

        window.applyPillColor(pill, child, true, Date.now());
        expect(pill.className).toContain('bg-gray-400');
        expect(pill.className).not.toContain('bg-green-500');

        window.applyPillColor(pill, child, false, 1000);
        expect(pill.className).toContain('bg-green-500');
        expect(pill.className).not.toContain('bg-gray-400');
    });

    it('renders pill with stepped background class', () => {
        const window = loadWindow();
        const html = window.renderChildren([
            {
                first_name: 'Ada',
                last_name: 'Lovelace',
                security_code: '1234',
                checked_out_at_ms: 3 * 60 * 1000,
                checked_out_confirmed_at: '2024-01-01T00:05:00Z',
                planning_center_id: 'pc-1',
                source: 'planning_center'
            },
            {
                first_name: 'Sam',
                last_name: 'Test',
                security_code: '9999',
                checked_out_at_ms: 60 * 1000,
                planning_center_id: '11',
                source: 'planning_center'
            }
        ], 5 * 60 * 1000);

        expect(html).toContain('bg-gray-400');
        expect(html).toContain('bg-yellow-500');
        expect(html).toContain('transition-colors');
        // bar uses inline background-color; pill still uses bg-* classes
        expect(html).toContain('background-color:');
        expect(html).toContain('aria-hidden="true"');
    });

    it('renders empty state when no checkouts are active', () => {
        const window = loadWindow();
        const html = window.renderChildren([], Date.now());
        expect(html).toContain('No children called yet');
    });

    it('shows error state in children list on fetch error', async () => {
        const html = `<!doctype html>
            <html>
                <body>
                    <div id="children-list">
                        <div class="child-time" data-child-id="pc:1">5 min ago</div>
                    </div>
                </body>
            </html>`;
        const window = loadWindow({
            html,
            fetchImpl: async () => {
                throw new Error('offline');
            }
        });

        const originalConsoleError = window.console.error;
        window.console.error = () => { };

        try {
            await window.fetchChildrenData();
        } finally {
            window.console.error = originalConsoleError;
        }

        expect(window.document.getElementById('children-list').innerHTML)
            .toContain('Error loading data. Please try again.');
    });

    it('keeps confirmation overrides applied until data catches up', () => {
        const html = `<!doctype html>
            <html>
                <body>
                    <div id="children-list">
                        <label data-confirmed-label data-confirmed-state="unconfirmed">
                            <input type="checkbox" class="child-confirmed-checkbox" data-child-id="manual:pub-1">
                        </label>
                    </div>
                </body>
            </html>`;
        const window = loadWindow({ html });

        window.__test.setDom();
        window.__test.setChildrenData([
            {
                source: 'manual',
                public_id: 'pub-1',
                checked_out_confirmed_at: null,
                checked_out_at: '2024-01-01T00:00:00Z'
            }
        ]);

        window.__test.setConfirmationOverride('manual:pub-1', true);
        window.__test.syncConfirmedStates();

        const checkboxes = window.document.querySelectorAll('.child-confirmed-checkbox');
        checkboxes.forEach((checkbox) => {
            expect(checkbox.checked).toBe(true);
            const label = checkbox.closest('[data-confirmed-label]');
            expect(label?.dataset.confirmedState).toBe('confirmed');
        });
    });

    it('filters visible children by name and code', () => {
        const window = loadWindow();
        window.__test.setChildrenData([
            { id: 'pc:1', first_name: 'Alice', last_name: 'Smith', security_code: '1234', source: 'planning_center' },
            { id: 'pc:2', first_name: 'Bob', last_name: 'Jones', security_code: '5678', source: 'planning_center' }
        ]);
        window.__test.setSearchQuery('ali');
        expect(window.__test.getVisibleChildren().map((c) => c.id)).toEqual(['pc:1']);
        window.__test.setSearchQuery('5678');
        expect(window.__test.getVisibleChildren().map((c) => c.id)).toEqual(['pc:2']);
        window.__test.setSearchQuery('');
        expect(window.__test.getVisibleChildren()).toHaveLength(2);
    });

    it('renders no-matching message for empty search results', () => {
        const window = loadWindow({ html: '<!doctype html><html><body><ul id="children-list"></ul></body></html>' });
        window.__test.setChildrenData([]);
        window.__test.setDom();
        window.__test.setSearchQuery('zzz');
        expect(window.document.getElementById('children-list').innerHTML).toContain('No matching children');
    });

    it('setHideConfirmed filters confirmed children from view without deleting them', () => {
        const window = loadWindow();
        window.__test.setChildrenData([
            { id: 'pc:1', first_name: 'A', last_name: 'B', security_code: '1', source: 'planning_center', checked_out_confirmed_at: '2026-08-18T10:00:00Z' },
            { id: 'pc:2', first_name: 'C', last_name: 'D', security_code: '2', source: 'planning_center', checked_out_confirmed_at: null }
        ]);
        window.__test.setHideConfirmed(true);
        const visible = window.__test.getVisibleChildren();
        expect(visible.map((c) => c.id)).toEqual(['pc:2']);
        expect(window.__test.getChildrenData()).toHaveLength(2);
    });

    it('setHideConfirmed(false) shows confirmed children again', () => {
        const window = loadWindow();
        window.__test.setChildrenData([
            { id: 'pc:1', first_name: 'A', last_name: 'B', security_code: '1', source: 'planning_center', checked_out_confirmed_at: '2026-08-18T10:00:00Z' },
            { id: 'pc:2', first_name: 'C', last_name: 'D', security_code: '2', source: 'planning_center', checked_out_confirmed_at: null }
        ]);
        window.__test.setHideConfirmed(true);
        window.__test.setHideConfirmed(false);
        expect(window.__test.getVisibleChildren()).toHaveLength(2);
    });

    function hideConfirmedToggleWindow() {
        return loadWindow({ html: '<!doctype html><html><body><input id="hide-confirmed-toggle" type="checkbox" role="switch" aria-label="Hide confirmed children"></body></html>' });
    }

    it('setHideConfirmed keeps the hide-confirmed toggle checked state in sync', () => {
        const window = hideConfirmedToggleWindow();
        const toggle = window.document.getElementById('hide-confirmed-toggle');

        window.__test.setHideConfirmed(true);
        expect(toggle.checked).toBe(true);

        window.__test.setHideConfirmed(false);
        expect(toggle.checked).toBe(false);
    });

    it('changing the hide-confirmed toggle updates the confirmed filter', () => {
        const window = hideConfirmedToggleWindow();
        window.document.dispatchEvent(new window.Event('DOMContentLoaded'));
        window.__test.setChildrenData([
            { id: 'pc:1', first_name: 'A', last_name: 'B', security_code: '1', source: 'planning_center', checked_out_confirmed_at: '2026-08-18T10:00:00Z' },
            { id: 'pc:2', first_name: 'C', last_name: 'D', security_code: '2', source: 'planning_center', checked_out_confirmed_at: null }
        ]);

        const toggle = window.document.getElementById('hide-confirmed-toggle');
        toggle.checked = true;
        toggle.dispatchEvent(new window.Event('change'));
        expect(window.__test.getVisibleChildren().map((c) => c.id)).toEqual(['pc:2']);

        toggle.checked = false;
        toggle.dispatchEvent(new window.Event('change'));
        expect(window.__test.getVisibleChildren()).toHaveLength(2);
    });

    it('search toggle button expands and collapses the search controls', () => {
        const window = loadWindow({
            html: '<!doctype html><html><body><button id="search-toggle-button" aria-expanded="false" aria-controls="search-controls"><svg data-search-toggle-icon></svg></button><div id="search-controls" class="w-full max-w-md"></div></body></html>'
        });
        window.document.dispatchEvent(new window.Event('DOMContentLoaded'));
        const toggle = window.document.getElementById('search-toggle-button');
        const controls = window.document.getElementById('search-controls');
        const icon = toggle.querySelector('[data-search-toggle-icon]');

        expect(controls.classList.contains('is-expanded')).toBe(false);
        expect(toggle.getAttribute('aria-expanded')).toBe('false');
        expect(icon.classList.contains('rotate-180')).toBe(false);

        toggle.click();
        expect(controls.classList.contains('is-expanded')).toBe(true);
        expect(toggle.getAttribute('aria-expanded')).toBe('true');
        expect(icon.classList.contains('rotate-180')).toBe(true);

        toggle.click();
        expect(controls.classList.contains('is-expanded')).toBe(false);
        expect(toggle.getAttribute('aria-expanded')).toBe('false');
        expect(icon.classList.contains('rotate-180')).toBe(false);
    });

    it('re-measures expanded search controls height on window resize', () => {
        const window = loadWindow({
            html: '<!doctype html><html><body><button id="search-toggle-button" aria-expanded="false"><svg data-search-toggle-icon></svg></button><div id="search-controls"></div></body></html>'
        });
        window.document.dispatchEvent(new window.Event('DOMContentLoaded'));
        const toggle = window.document.getElementById('search-toggle-button');
        const controls = window.document.getElementById('search-controls');

        toggle.click();
        const measuredHeight = controls.style.height;

        controls.style.height = '123px';
        window.dispatchEvent(new window.Event('resize'));
        expect(controls.style.height).toBe(measuredHeight);

        toggle.click();
        controls.style.height = '456px';
        window.dispatchEvent(new window.Event('resize'));
        expect(controls.style.height).toBe('456px');
    });

    it('renders no-unconfirmed message when hiding confirmed empties the board', () => {
        const window = loadWindow();
        window.__test.setChildrenData([
            { id: 'pc:1', first_name: 'A', last_name: 'B', security_code: '1', source: 'planning_center', checked_out_confirmed_at: '2026-08-18T10:00:00Z' }
        ]);
        window.__test.setHideConfirmed(true);
        const html = window.renderChildren(window.__test.getVisibleChildren(), Date.now(), false);
        expect(html).toContain('No unconfirmed children');
    });

    it('computeNewChildIds seeds on first call and detects later additions', () => {
        const window = loadWindow();
        const pc1 = { source: 'planning_center', planning_center_id: 'pc1' };
        const pc2 = { source: 'planning_center', planning_center_id: 'pc2' };
        const first = window.__test.computeNewChildIds([pc1]);
        expect(first.size).toBe(0);
        const second = window.__test.computeNewChildIds([pc1, pc2]);
        expect(Array.from(second)).toEqual(['pc:pc2']);
    });

    it('does not flash children when the location group filter changes', async () => {
        const w = loadWindow({
            html: '<!doctype html><html><body><div id="children-list"></div></body></html>',
            url: 'http://localhost/?location_group_id=1',
            fetchImpl: async (url) => {
                if (String(url).includes('location_group_id=1')) {
                    return { ok: true, json: async () => [pcChild('a')] };
                }
                if (String(url).includes('location_group_id=2')) {
                    return { ok: true, json: async () => [pcChild('b'), pcChild('c')] };
                }
                return { ok: true, json: async () => [] };
            }
        });
        await w.fetchChildrenData();
        expect(w.__test.getFlashChildIds().size).toBe(0);

        w.history.replaceState(null, '', '?location_group_id=2');
        await w.fetchChildrenData();

        expect(w.__test.getFlashChildIds().size).toBe(0);
        expect(w.document.querySelectorAll('.child-card-flash').length).toBe(0);
    });

    it('still flashes net-new children when the filter is unchanged', async () => {
        const children = [pcChild('a'), pcChild('b')];
        const w = loadWindow({
            url: 'http://localhost/?location_group_id=1',
            fetchImpl: async () => ({ ok: true, json: async () => children.slice() })
        });
        await w.fetchChildrenData();
        expect(w.__test.getFlashChildIds().size).toBe(0);

        children.push(pcChild('c'));
        await w.fetchChildrenData();

        expect(Array.from(w.__test.getFlashChildIds())).toEqual(['pc:c']);
    });

    it('clears an in-flight flash when the filter changes', async () => {
        const children = [pcChild('a'), pcChild('b')];
        const w = loadWindow({
            html: '<!doctype html><html><body><div id="children-list"></div></body></html>',
            url: 'http://localhost/?location_group_id=1',
            fetchImpl: async () => ({ ok: true, json: async () => children.slice() })
        });
        await w.fetchChildrenData();
        children.push(pcChild('d'));
        await w.fetchChildrenData();
        expect(Array.from(w.__test.getFlashChildIds())).toEqual(['pc:d']);

        w.history.replaceState(null, '', '?location_group_id=2');
        await w.fetchChildrenData();
        expect(w.__test.getFlashChildIds().size).toBe(0);
    });

    it('renders child-card-flash class for flashing ids', () => {
        const window = loadWindow();
        const child = pcChild('pc1');
        const html = window.renderChildren([child], Date.now(), false);
        expect(html).not.toContain('child-card-flash');
        window.__test.setFlashChildIds(new Set(['pc:pc1']));
        const flashing = window.renderChildren([child], Date.now(), false);
        expect(flashing).toContain('child-card-flash');
    });

    it('clearFlashStyles removes flash class from the board and resets flash ids', () => {
        const window = childrenListWindow();
        window.__test.setChildrenData([pcChild('pc1')]);
        window.__test.setDom();
        window.__test.setFlashChildIds(new Set(['pc:pc1']));
        window.__test.updateUI();
        expect(window.document.querySelector('.child-card-flash')).not.toBeNull();
        window.__test.clearFlashStyles();
        expect(window.document.querySelector('.child-card-flash')).toBeNull();
        expect(window.__test.getFlashChildIds().size).toBe(0);
    });

    it('strips stale flash classes before morphing so a new child re-flashes', () => {
        const window = childrenListWindow();
        window.__test.setDom();
        boardWithFlashedChild(window);
        expect(cardFor(window, 'pc:d').className).toContain('child-card-flash');
        let classesAtMorph = null;
        const realMorphdom = window.morphdom;
        window.morphdom = (target, template, opts) => {
            classesAtMorph = [...target.querySelectorAll('.bg-white.rounded-lg')].map((el) => el.className);
            realMorphdom(target, template, opts);
        };
        window.__test.setChildrenData([pcChild('e'), pcChild('d'), pcChild('a'), pcChild('b')]);
        window.__test.setFlashChildIds(new Set(['pc:e']));
        window.__test.updateUI();
        expect(classesAtMorph.length).toBeGreaterThan(0);
        expect(classesAtMorph.every((className) => !className.includes('child-card-flash'))).toBe(true);
    });

    it('forces a reflow after clearing stale flash classes so the animation restarts', () => {
        const window = childrenListWindow();
        window.__test.setDom();
        boardWithFlashedChild(window);
        expect(cardFor(window, 'pc:d').className).toContain('child-card-flash');
        let reflowCount = 0;
        Object.defineProperty(window.document.getElementById('children-list'), 'offsetHeight', {
            configurable: true,
            get: () => {
                reflowCount += 1;
                return 0;
            }
        });
        window.__test.setChildrenData([pcChild('e'), pcChild('d'), pcChild('a'), pcChild('b')]);
        window.__test.setFlashChildIds(new Set(['pc:e']));
        window.__test.updateUI();
        expect(reflowCount).toBeGreaterThan(0);
    });

    it('maps location_group_id deterministically to Paul Tol muted, gray for null', () => {
        const w = loadWindow();
        expect(w.getLocationGroupColor(null)).toBe('#9CA3AF');
        expect(w.getLocationGroupColor(undefined)).toBe('#9CA3AF');
        const c1 = w.getLocationGroupColor(1);
        expect(c1).toBe(w.getLocationGroupColor(1));
        expect(w.PAUL_TOL_MUTED).not.toContain('#9CA3AF');
        expect(w.getLocationGroupColor(2)).not.toBe(c1);
    });

    it('renders color bar matching location_group_id', () => {
        const w = loadWindow();
        const html = w.renderChildren([{ first_name: 'A', last_name: 'B', location_group_id: 1, planning_center_id: '1', checked_out_at_ms: Date.now() }], Date.now());
        expect(html).toContain('background-color:' + w.getLocationGroupColor(1));
        expect(html).toContain('width:6px');
        expect(html).toContain('flex-shrink:0');
        expect(html).toContain('aria-hidden="true"');
        expect(html).toContain('rounded-l-lg');
        expect(html).not.toContain('overflow-hidden');
        expect(html).toContain('flex-1');
        const htmlNull = w.renderChildren([{ first_name: 'C', last_name: 'D', location_group_id: null, planning_center_id: '2', checked_out_at_ms: Date.now() }], Date.now());
        expect(htmlNull).toContain('#9CA3AF');
        expect(htmlNull).toContain('background-color:#9CA3AF');
    });

    it('renders color bar with flash class on outer container', () => {
        const w = loadWindow();
        w.__test.setFlashChildIds(new Set(['pc:1']));
        const html = w.renderChildren([{ first_name: 'A', last_name: 'B', location_group_id: 1, planning_center_id: '1', checked_out_at_ms: Date.now() }], Date.now());
        expect(html).toContain('child-card-flash');
        expect(html).toContain('flex child-card-flash');
    });

    it('syncLocationGroupUIFromURL Select All toggles text and hides/shows children', async () => {
        const w = locationGroupWindow();
        w.fetchChildrenData = async () => {};
        w.__test.setChildrenData([
            { source: 'planning_center', planning_center_id: '1', first_name: 'A', last_name: 'B', location_group_id: 1 },
            { source: 'planning_center', planning_center_id: '2', first_name: 'C', last_name: 'D', location_group_id: 2 }
        ]);
        w.document.dispatchEvent(new w.Event('DOMContentLoaded'));
        await new Promise(r=>setTimeout(r,10));

        let btn = w.document.getElementById('location-group-select-all');
        let checks = w.document.querySelectorAll('#location-group-checkboxes input');
        // Initially all checked (no filter params) => text should be Deselect all
        expect(btn.textContent.trim()).toBe('Deselect all');
        expect([...checks].every(c=>c.checked)).toBe(true);
        // isEmpty should be false when no params
        expect(w.getSelectedFromURL().isEmpty).toBe(false);
        // visible should show all children
        expect(w.__test.getVisibleChildren().length).toBe(2);

        // Click Deselect all
        btn.click();
        expect(btn.textContent.trim()).toBe('Select all');
        expect(w.getSelectedFromURL().isEmpty).toBe(true);
        expect(w.__test.getVisibleChildren().length).toBe(0);

        // Click Select all
        btn.click();
        expect(btn.textContent.trim()).toBe('Deselect all');
        expect(w.getSelectedFromURL().isEmpty).toBe(false);
        expect(w.__test.getVisibleChildren().length).toBe(2);
    });

    it('manual checkbox uncheck all hides all children via sentinel URL', async () => {
        const w = locationGroupWindow();
        w.fetchChildrenData = async () => {};
        w.__test.setChildrenData([
            { source: 'planning_center', planning_center_id: '1', first_name: 'A', last_name: 'B', location_group_id: 1 },
            { source: 'planning_center', planning_center_id: '2', first_name: 'C', last_name: 'D', location_group_id: 2 }
        ]);
        w.document.dispatchEvent(new w.Event('DOMContentLoaded'));
        await new Promise(r=>setTimeout(r,10));

        let btn = w.document.getElementById('location-group-select-all');
        let checks = w.document.querySelectorAll('#location-group-checkboxes input');

        // Uncheck all checkboxes manually
        checks.forEach(c => { c.checked = false; c.dispatchEvent(new w.Event('change', {bubbles:true})); });
        await new Promise(r=>setTimeout(r,10));

        expect(btn.textContent.trim()).toBe('Select all');
        expect(w.getSelectedFromURL().isEmpty).toBe(true);
        expect(w.__test.getVisibleChildren().length).toBe(0);
    });

    it('select all when URL has empty location_group_id param shows Select all and hides all children', async () => {
        const w = locationGroupWindow();
        w.fetchChildrenData = async () => {};
        w.window.history.replaceState(null, '', '?location_group_id=');
        w.__test.setChildrenData([
            { source: 'planning_center', planning_center_id: '1', first_name: 'A', last_name: 'B', location_group_id: 1 },
            { source: 'planning_center', planning_center_id: '2', first_name: 'C', last_name: 'D', location_group_id: 2 }
        ]);
        w.document.dispatchEvent(new w.Event('DOMContentLoaded'));
        await new Promise(r=>setTimeout(r,10));

        let btn = w.document.getElementById('location-group-select-all');
        let checks = w.document.querySelectorAll('#location-group-checkboxes input');
        expect(btn.textContent.trim()).toBe('Select all');
        expect([...checks].every(c=>c.checked)).toBe(false);
        expect(w.getSelectedFromURL().isEmpty).toBe(true);
        expect(w.__test.getVisibleChildren().length).toBe(0);
    });

    it('still fetches (unfiltered) while the location group selection is empty but hides filtered children', async () => {
        let checkoutsFetchCount = 0;
        const w = loadWindow({
            html: '<!doctype html><html><body><div id="children-list"></div><button id="overdue-badge" class="hidden"></button><div id="overdue-sheet" class="translate-y-full"></div><div id="overdue-sheet-backdrop" class="hidden"></div><div id="overdue-sheet-list"></div><div id="toast-stack"></div></body></html>',
            url: 'http://localhost/?location_group_id=',
            fetchImpl: async (url) => {
                if (String(url).includes('/v1/checkins/checkouts')) checkoutsFetchCount++;
                return { ok: true, json: async () => [pcChild('a')] };
            }
        });
        await w.fetchChildrenData();
        // Single unfiltered poll: still hits the server so the overdue badge can be fed
        expect(checkoutsFetchCount).toBe(1);
        // Board is empty due to client-side isEmpty filter
        expect(w.document.getElementById('children-list').textContent).toContain('No children called yet');
        expect(w.__test.getVisibleChildren().length).toBe(0);
        // But childrenData still holds the fetched record (for overdue badge)
        expect(w.__test.getChildrenData().length).toBe(1);

        await w.fetchChildrenData();
        expect(checkoutsFetchCount).toBe(2);
    });

    const overdueChild = (id, minutesAgo) => ({
        source: 'planning_center',
        planning_center_id: id,
        first_name: id,
        last_name: 'X',
        security_code: id,
        checked_out_at_ms: Date.now() - minutesAgo * 60 * 1000,
        checked_out_confirmed_at: null
    });

    function overdueBadgeWindow() {
        return loadWindow({
            html: '<!doctype html><html><body><div id="children-list"></div><button id="overdue-badge" class="hidden"></button><div id="overdue-sheet" class="translate-y-full"></div><div id="overdue-sheet-backdrop" class="hidden"></div><div id="overdue-sheet-list"></div></body></html>',
            fetchImpl: async () => ({ ok: true, json: async () => [] })
        });
    }

    it('jiggles the overdue badge when the overdue count increases', async () => {
        const w = overdueBadgeWindow();
        w.document.dispatchEvent(new w.Event('DOMContentLoaded'));
        await new Promise(r=>setTimeout(r,10));

        const badge = w.document.getElementById('overdue-badge');
        w.__test.setChildrenData([overdueChild('a', 6)]);
        w.updateOverdueUI();
        expect(badge.classList.contains('hidden')).toBe(false);
        expect(badge.textContent).toContain('1 overdue');
        expect(badge.classList.contains('overdue-badge-jiggle')).toBe(true);

        // Same count: animation must not restart
        badge.classList.remove('overdue-badge-jiggle');
        w.updateOverdueUI();
        expect(badge.classList.contains('overdue-badge-jiggle')).toBe(false);

        // Count increases again: jiggle restarts
        w.__test.setChildrenData([overdueChild('a', 6), overdueChild('b', 7)]);
        w.updateOverdueUI();
        expect(badge.classList.contains('overdue-badge-jiggle')).toBe(true);
    });

    it('does not jiggle the overdue badge when the count decreases', async () => {
        const w = overdueBadgeWindow();
        w.document.dispatchEvent(new w.Event('DOMContentLoaded'));
        await new Promise(r=>setTimeout(r,10));

        const badge = w.document.getElementById('overdue-badge');
        w.__test.setChildrenData([overdueChild('a', 6), overdueChild('b', 7)]);
        w.updateOverdueUI();
        expect(badge.classList.contains('overdue-badge-jiggle')).toBe(true);
        badge.classList.remove('overdue-badge-jiggle');

        w.__test.setChildrenData([overdueChild('a', 6)]);
        w.updateOverdueUI();
        expect(badge.classList.contains('overdue-badge-jiggle')).toBe(false);
    });

    it('closes the overdue sheet when clicking the backdrop', async () => {
        const w = overdueBadgeWindow();
        w.document.dispatchEvent(new w.Event('DOMContentLoaded'));
        await new Promise(r=>setTimeout(r,10));

        w.__test.setChildrenData([overdueChild('a', 6)]);
        w.updateOverdueUI();
        w.openOverdueSheet();

        const sheet = w.document.getElementById('overdue-sheet');
        const backdrop = w.document.getElementById('overdue-sheet-backdrop');
        expect(sheet.classList.contains('translate-y-full')).toBe(false);
        expect(backdrop.classList.contains('hidden')).toBe(false);
        expect(w.document.body.style.overflow).toBe('hidden');

        backdrop.dispatchEvent(new w.MouseEvent('click', { bubbles: true }));

        expect(sheet.classList.contains('translate-y-full')).toBe(true);
        expect(backdrop.classList.contains('hidden')).toBe(true);
        expect(w.document.body.style.overflow).toBe('');
    });

    it('updates the main-list pill color when confirming via the overdue sheet', async () => {
        const w = overdueBadgeWindow();
        w.document.dispatchEvent(new w.Event('DOMContentLoaded'));
        await new Promise(r=>setTimeout(r,10));

        const child = overdueChild('a', 9);
        w.__test.setChildrenData([child]);
        w.updateUI();
        // The drawer only renders while open, and a child can only be confirmed
        // from it while it is open.
        w.openOverdueSheet();

        const mainPill = w.document.querySelector('#children-list .child-time[data-child-id="pc:a"]');
        const sheetPill = w.document.querySelector('#overdue-sheet-list .child-time[data-child-id="pc:a"]');
        expect(mainPill).not.toBeNull();
        expect(sheetPill).not.toBeNull();
        expect(mainPill.className).toContain('bg-red-500');
        expect(sheetPill.className).toContain('bg-red-500');

        const sheetCheckbox = w.document.querySelector('#overdue-sheet-list .child-confirmed-checkbox[data-child-id="pc:a"]');
        sheetCheckbox.checked = true;
        sheetCheckbox.dispatchEvent(new w.Event('change', { bubbles: true }));
        await new Promise(r=>setTimeout(r,10));

        // Main-list pill must flip to confirmed gray even though the cached
        // single entry pointed at the sheet pill.
        const updatedMainPill = w.document.querySelector('#children-list .child-time[data-child-id="pc:a"]');
        expect(updatedMainPill.className).toContain('bg-gray-400');
        expect(updatedMainPill.className).not.toContain('bg-red-500');
    });

    describe('overdue drawer rendering', () => {
        // Mirrors the real page: checkouts.html ships a placeholder child in
        // #overdue-sheet-list and a #location-group-checkboxes container.
        function drawerWindow() {
            return loadWindow({
                html: `<!doctype html><html><body>
                    <div id="children-list"></div>
                    <button id="overdue-badge" class="hidden"></button>
                    <div id="overdue-sheet" class="translate-y-full" role="dialog" tabindex="-1"></div>
                    <div id="overdue-sheet-backdrop" class="hidden"></div>
                    <div id="location-group-checkboxes"></div>
                    <span id="overdue-sheet-count"></span>
                    <div id="overdue-sheet-list"><div class="text-center py-6 text-sm text-slate-500">No overdue checkouts</div></div>
                </body></html>`,
                fetchImpl: async () => ({ ok: true, json: async () => ({ checkins: [], manual_checkins: [] }) })
            });
        }

        // Absolute-timestamp fixture so minute labels are exact regardless of
        // when the suite runs.
        function childAt(id, minutesAgo, nowMs, extra = {}) {
            return {
                source: 'planning_center',
                planning_center_id: id,
                first_name: 'Kida',
                last_name: 'Test',
                security_code: 'CODE' + id,
                location_group_id: 1,
                checked_out_at_ms: nowMs - minutesAgo * 60 * 1000,
                checked_out_confirmed_at: null,
                ...extra
            };
        }

        // The page reads Date.now() from its own window, so the clock has to be
        // patched there rather than with fake timers.
        function freezeClock(w, startMs) {
            let nowMs = startMs;
            w.Date.now = () => nowMs;
            return {
                advance: (ms) => { nowMs += ms; },
                at: () => nowMs
            };
        }

        function sheetList(w) {
            return w.document.getElementById('overdue-sheet-list');
        }
        function sheetRows(w) {
            return [...sheetList(w).querySelectorAll(':scope > div')];
        }
        function sheetRowFor(w, id) {
            return w.document.querySelector(`#overdue-sheet-list .child-time[data-child-id="pc:${id}"]`)?.closest('div.bg-white');
        }
        function sheetPill(w, id) {
            return w.document.querySelector(`#overdue-sheet-list .child-time[data-child-id="pc:${id}"]`);
        }
        function sheetBox(w, id) {
            return w.document.querySelector(`#overdue-sheet-list .child-confirmed-checkbox[data-child-id="pc:${id}"]`);
        }
        function mainBox(w, id) {
            return w.document.querySelector(`#children-list .child-confirmed-checkbox[data-child-id="pc:${id}"]`);
        }
        // Counts DOM mutations in the drawer. Behaviour-level on purpose: it
        // catches an innerHTML rebuild, a morph that changes something, and any
        // future approach alike, where spying on `morphdom` alone would only
        // prove that one particular function was not called.
        function watchSheetMutations(w) {
            const seen = { count: 0 };
            const observer = new w.MutationObserver((records) => { seen.count += records.length; });
            observer.observe(w.document.getElementById('overdue-sheet-list'), {
                childList: true, subtree: true, attributes: true, characterData: true,
            });
            return {
                mutations: () => seen.count,
                stop: () => observer.disconnect(),
            };
        }

        // Records which drawer row each mutation landed in, so a test can assert
        // a re-render was confined to the rows that actually changed rather than
        // asserting on a mock or on a node count.
        function watchSheetRowTouches(w) {
            const touched = new Set();
            const observer = new w.MutationObserver((records) => {
                for (const record of records) {
                    const target = record.target;
                    const element = target.nodeType === 3 ? target.parentElement : target;
                    const row = element && element.closest ? element.closest('div.bg-white') : null;
                    const pill = row && row.querySelector('.child-time[data-child-id]');
                    if (pill) touched.add(pill.dataset.childId);
                }
            });
            observer.observe(w.document.getElementById('overdue-sheet-list'), {
                childList: true, subtree: true, attributes: true, characterData: true,
            });
            return {
                touched: () => [...touched].sort(),
                stop: () => observer.disconnect(),
            };
        }

        async function ready(children, nowMs = 1_700_000_000_000) {
            const w = drawerWindow();
            w.document.dispatchEvent(new w.Event('DOMContentLoaded'));
            await new Promise(r => setTimeout(r, 10));
            const clock = freezeClock(w, nowMs);
            w.__test.setChildrenData(children);
            w.updateUI();
            return { w, clock };
        }

        // "untouched" means the markup: no rebuild while nobody is looking. The
        // 1s tick still refreshes the closed drawer's labels in place -- see the
        // companion test below.
        it('leaves the hidden drawer markup untouched while it is closed', async () => {
            const { w } = await ready([childAt('a', 6, 1_700_000_000_000), childAt('b', 7, 1_700_000_000_000)]);
            const before = sheetList(w).innerHTML;

            w.updateOverdueUI();

            // The badge is the visible surface while closed, so it updates...
            expect(w.document.getElementById('overdue-badge').textContent).toContain('2 overdue');
            // ...but the hidden drawer must not be rebuilt for an audience of nobody.
            expect(before).toContain('No overdue checkouts');
            expect(sheetList(w).innerHTML).toBe(before);
        });

        // Companion to the test above: the closed drawer is not frozen, only
        // un-rebuilt. updateTimes walks the drawer every tick regardless of
        // visibility, which is what keeps it fresh for the next open.
        it('still refreshes the closed drawer labels on the 1s tick', async () => {
            const now0 = 1_700_000_000_000;
            const { w, clock } = await ready([childAt('a', 6, now0)], now0);
            w.openOverdueSheet();
            w.closeOverdueSheet();
            expect(sheetPill(w, 'a').textContent.trim()).toBe('6 min ago');

            clock.advance(90_000);
            w.__test.updateAllTimes();

            expect(sheetPill(w, 'a').textContent.trim()).toBe('7 min ago');
        });

        // Guards the invariant the closed-drawer skip depends on: whatever the
        // drawer shows must be a fresh snapshot taken when it becomes visible.
        it('renders the current overdue set when the drawer is opened', async () => {
            const { w } = await ready([childAt('a', 6, 1_700_000_000_000), childAt('b', 7, 1_700_000_000_000)]);
            expect(sheetList(w).textContent).toContain('No overdue checkouts');

            w.openOverdueSheet();

            expect(sheetRows(w).length).toBe(2);
            expect(sheetPill(w, 'a')).not.toBeNull();
        });

        it('reuses untouched drawer rows when a sibling row does change', async () => {
            const now0 = 1_700_000_000_000;
            const { w } = await ready([childAt('a', 6, now0), childAt('b', 7, now0)], now0);
            w.openOverdueSheet();
            // Grab 'b' by identity, and confirm the drawer is ordered oldest
            // first so the row we mutate is not the one we assert on.
            expect(sheetRows(w).length).toBe(2);
            const untouchedRow = sheetRowFor(w, 'b');

            // Rename 'a' so the signature changes and a re-render actually runs.
            w.__test.setChildrenData([childAt('a', 6, now0, { first_name: 'Renamed' }), childAt('b', 7, now0)]);
            w.updateOverdueUI();

            expect(sheetRowFor(w, 'a').textContent).toContain('Renamed');
            // A keyed morph leaves the untouched row's DOM node in place; an
            // innerHTML assignment would have replaced it.
            expect(sheetRowFor(w, 'b')).toBe(untouchedRow);
        });

        it('does not rebuild the drawer when only time has passed', async () => {
            const now0 = 1_700_000_000_000;
            const { w, clock } = await ready([childAt('a', 6, now0), childAt('b', 6, now0)], now0);
            w.openOverdueSheet();
            // 6.0 and 7.5 minutes are both in the yellow band, so the pill class
            // cannot be what makes the text differ.
            expect(sheetPill(w, 'a').className).toContain('bg-yellow-500');
            expect(sheetPill(w, 'a').textContent.trim()).toBe('6 min ago');

            const watcher = watchSheetMutations(w);
            clock.advance(90_000);
            w.updateOverdueUI();
            // MutationObserver delivers on a microtask. Without this the count is
            // read before any record can have been queued, so the assertion below
            // holds no matter what the render did.
            await Promise.resolve();

            // Unchanged row set: nothing on screen may be touched at all.
            expect(watcher.mutations()).toBe(0);
            expect(sheetPill(w, 'a').textContent.trim()).toBe('6 min ago');
            watcher.stop();

            // updateTimes owns the labels, so they must still advance without
            // any rebuild. This is what lets the signature omit them.
            w.__test.updateAllTimes();
            expect(sheetPill(w, 'a').textContent.trim()).toBe('7 min ago');
            expect(sheetPill(w, 'b').textContent.trim()).toBe('7 min ago');
        });

        // Companion to the test above. That one covers "the guard skipped the
        // work", which no observer can tell apart from "the work happened a
        // different way" -- with the signature unchanged nothing runs at all.
        // This one forces a render by changing the signature and asserts the
        // render was confined to the row that actually changed, which an
        // innerHTML rebuild cannot pass.
        it('touches only the changed row when a re-render is genuinely required', async () => {
            const now0 = 1_700_000_000_000;
            const { w } = await ready([childAt('a', 6, now0), childAt('b', 7, now0)], now0);
            w.openOverdueSheet();
            expect(sheetRows(w).length).toBe(2);
            const rowB = sheetRowFor(w, 'b');

            const watcher = watchSheetRowTouches(w);
            w.__test.setChildrenData([childAt('a', 6, now0, { first_name: 'Renamed' }), childAt('b', 7, now0)]);
            w.updateOverdueUI();
            await Promise.resolve();

            expect(sheetRowFor(w, 'a').textContent).toContain('Renamed');
            // Bravo's row is neither replaced nor written to.
            expect(sheetRowFor(w, 'b')).toBe(rowB);
            expect(watcher.touched()).toEqual(['pc:a']);
            watcher.stop();
        });

        it('paints the drawer row confirmed when the child is confirmed from the main list', async () => {
            const now0 = 1_700_000_000_000;
            const { w } = await ready([childAt('a', 6, now0), childAt('b', 7, now0)], now0);
            w.openOverdueSheet();

            // Confirming in the main list never touches the drawer's own
            // checkbox, so only a re-render can paint the drawer row.
            const box = mainBox(w, 'a');
            expect(box).not.toBeNull();
            box.checked = true;
            box.dispatchEvent(new w.Event('change', { bubbles: true }));
            await new Promise(r => setTimeout(r, 10));

            expect(sheetRows(w).length).toBe(2);
            expect(sheetBox(w, 'a').checked).toBe(true);
            expect(sheetBox(w, 'a').closest('[data-confirmed-label]').dataset.confirmedState).toBe('confirmed');
            expect(sheetPill(w, 'a').className).toContain('bg-gray-400');
        });




        it('updates the drawer group label when location groups load late', async () => {
            const now0 = 1_700_000_000_000;
            const { w } = await ready([childAt('a', 6, now0)], now0);
            w.openOverdueSheet();
            expect(sheetRowFor(w, 'a').textContent).toContain('Group 1');

            // locationGroups is fetched asynchronously, so it can land after the
            // drawer has already rendered.
            w.renderLocationGroupSettings([{ id: 1, name: 'Room A' }]);
            w.updateOverdueUI();

            expect(sheetRowFor(w, 'a').textContent).toContain('Room A');
            expect(sheetRowFor(w, 'a').textContent).not.toContain('Group 1');
        });

        it('re-renders the drawer when a child\'s name changes', async () => {
            const now0 = 1_700_000_000_000;
            const { w } = await ready([childAt('a', 6, now0)], now0);
            w.openOverdueSheet();
            expect(sheetRowFor(w, 'a').textContent).toContain('Kida');

            w.__test.setChildrenData([childAt('a', 6, now0, { first_name: 'Renamed' })]);
            w.updateOverdueUI();

            expect(sheetRowFor(w, 'a').textContent).toContain('Renamed');
        });

        // The signature decides whether a re-render happens, so it has to be
        // injective: two different children must never produce the same one. A
        // '|' inside a name is what breaks that when the fields are joined.
        it('re-renders when a name shift would otherwise collide in the signature', async () => {
            const now0 = 1_700_000_000_000;
            const { w } = await ready([childAt('a', 6, now0, { first_name: 'Ada', last_name: 'B|C' })], now0);
            w.openOverdueSheet();
            expect(sheetRowFor(w, 'a').textContent).toContain('Ada B|C');

            // 'Ada|B' + 'C' is indistinguishable from 'Ada' + 'B|C' once the
            // fields are '|'-joined, so the drawer would skip the render and
            // keep showing the old name on screen.
            w.__test.setChildrenData([childAt('a', 6, now0, { first_name: 'Ada|B', last_name: 'C' })]);
            w.updateOverdueUI();

            expect(sheetRowFor(w, 'a').textContent).toContain('Ada|B C');
            expect(sheetRowFor(w, 'a').textContent).not.toContain('Ada B|C');
        });

        it('moves focus into the sheet on open and restores it on close', async () => {
            const { w } = await ready([childAt('a', 6, 1_700_000_000_000), childAt('b', 7, 1_700_000_000_000)]);
            const badge = w.document.getElementById('overdue-badge');
            badge.focus();
            expect(w.document.activeElement).toBe(badge);

            w.openOverdueSheet();

            // The sheet is a role="dialog". Leaving focus on the badge -- which
            // the open sheet occludes -- strands keyboard and screen-reader users
            // on an invisible control with a dialog on screen.
            const sheet = w.document.getElementById('overdue-sheet');
            expect(sheet.contains(w.document.activeElement)).toBe(true);

            w.closeOverdueSheet();

            expect(w.document.activeElement).toBe(badge);
        });

        // ---- the drawer is a frozen snapshot ----
        // Three properties, together, make the list unable to move under the
        // reader: no row is ever inserted, the only removals are children aging
        // out of the polled window, and newest-first puts those at the tail.
        describe('the drawer as a frozen snapshot', () => {
            const T0 = 1_700_000_000_000;
            // Newest first: Charlie (10m), Bravo (15m), Alpha (20m)
            const kids = () => [
                childAt('a', 20, T0, { first_name: 'Alpha' }),
                childAt('b', 15, T0, { first_name: 'Bravo' }),
                childAt('c', 10, T0, { first_name: 'Charlie' }),
            ];
            const names = (w) => sheetRows(w).map((r) => r.querySelector('.font-bold')?.textContent.trim());

            it('sorts newest first', async () => {
                const { w } = await ready(kids(), T0);
                w.openOverdueSheet();

                expect(names(w)).toEqual(['Charlie Test', 'Bravo Test', 'Alpha Test']);
            });

            it('does not add a child that becomes overdue while it is open', async () => {
                const { w } = await ready(kids(), T0);
                w.openOverdueSheet();
                expect(names(w)).toEqual(['Charlie Test', 'Bravo Test', 'Alpha Test']);

                // Delta crosses the 5-minute threshold while the drawer is open.
                w.__test.setChildrenData([...kids(), childAt('d', 6, T0, { first_name: 'Delta' })]);
                w.updateOverdueUI();

                // A snapshot of who was already overdue when it opened.
                expect(names(w)).toEqual(['Charlie Test', 'Bravo Test', 'Alpha Test']);
            });

            it('picks up the newly overdue child after a close and reopen', async () => {
                const { w } = await ready(kids(), T0);
                w.openOverdueSheet();
                w.__test.setChildrenData([...kids(), childAt('d', 6, T0, { first_name: 'Delta' })]);

                w.closeOverdueSheet();
                w.openOverdueSheet();
                w.updateOverdueUI();

                expect(names(w)).toEqual(['Delta Test', 'Charlie Test', 'Bravo Test', 'Alpha Test']);
            });

            it('drops a child that leaves the payload from the tail, leaving the rest in place', async () => {
                const { w } = await ready(kids(), T0);
                w.openOverdueSheet();
                expect(names(w)).toEqual(['Charlie Test', 'Bravo Test', 'Alpha Test']);
                const rowC = sheetRowFor(w, 'c');
                const rowB = sheetRowFor(w, 'b');
                const rowA = sheetRowFor(w, 'a');

                // Alpha is the oldest, so it is the one that ages out of the
                // -31m window -- and newest-first puts it at the tail, so the two
                // rows above it must not move.
                w.__test.setChildrenData([kids()[1], kids()[2]]);
                w.updateOverdueUI();

                expect(names(w)).toEqual(['Charlie Test', 'Bravo Test']);
                expect(sheetRowFor(w, 'c')).toBe(rowC);
                expect(sheetRowFor(w, 'b')).toBe(rowB);
                expect(sheetRowFor(w, 'a')).toBeUndefined();
            });

            it('does not keep a row the payload no longer has', async () => {
                const { w } = await ready(kids(), T0);
                w.openOverdueSheet();

                w.__test.setChildrenData([kids()[1], kids()[2]]);
                w.updateOverdueUI();

                // Nothing is cached, so a confirmed child that leaves the payload
                // goes too. It cannot be held in place without inventing data.
                expect(sheetRowFor(w, 'a')).toBeUndefined();
            });
        });

        // ---- cross-device confirmation ----
        // Another staff member confirming arrives as poll data, with no local
        // change event. The row must not vanish from under the reader's finger.
        describe('when another device confirms a child', () => {
            const T0 = 1_700_000_000_000;
            // drawer sorts oldest first: Alpha, Bravo, Charlie
            const trio = () => [
                childAt('a', 20, T0, { first_name: 'Alpha' }),
                childAt('b', 15, T0, { first_name: 'Bravo' }),
                childAt('c', 10, T0, { first_name: 'Charlie' }),
            ];
            const withBravoConfirmed = () => [
                trio()[0],
                { ...trio()[1], checked_out_confirmed_at: new Date(T0).toISOString() },
                trio()[2],
            ];
            const sheetNames = (w) => sheetRows(w).map((r) => r.querySelector('.font-bold')?.textContent.trim());

            it('keeps the row, checked and with a gray pill', async () => {
                const { w } = await ready(trio(), T0);
                w.openOverdueSheet();
                expect(sheetNames(w)).toEqual(['Charlie Test', 'Bravo Test', 'Alpha Test']);

                w.__test.setChildrenData(withBravoConfirmed());
                w.updateOverdueUI();

                expect(sheetNames(w)).toEqual(['Charlie Test', 'Bravo Test', 'Alpha Test']);
                expect(sheetBox(w, 'b').checked).toBe(true);
                expect(sheetBox(w, 'b').closest('[data-confirmed-label]').dataset.confirmedState).toBe('confirmed');
                expect(sheetPill(w, 'b').className).toContain('bg-gray-400');
            });


            it('keeps the confirmed row in its original position', async () => {
                const { w } = await ready(trio(), T0);
                w.openOverdueSheet();
                const rowB = sheetRowFor(w, 'b');

                w.__test.setChildrenData(withBravoConfirmed());
                w.updateOverdueUI();

                // Same node, same index: the list must not reflow.
                expect(sheetRows(w).indexOf(sheetRowFor(w, 'b'))).toBe(1);
                expect(sheetRowFor(w, 'b')).toBe(rowB);
            });

            it('drops the row once the drawer is closed and reopened', async () => {
                const { w } = await ready(trio(), T0);
                w.openOverdueSheet();
                w.__test.setChildrenData(withBravoConfirmed());
                w.updateOverdueUI();
                expect(sheetRows(w).length).toBe(3);

                w.closeOverdueSheet();
                w.openOverdueSheet();
                // Pruning runs on the next update, not during openOverdueSheet,
                // so without this tick a stale tombstone survives the reopen.
                w.updateOverdueUI();

                expect(sheetNames(w)).toEqual(['Charlie Test', 'Alpha Test']);
            });

            // Reopening with NO data change in between. The board polls every 3s,
            // so in a quiet room this is the common case, not a race. The drawer
            // rebuilds its "rows on screen" state on open -- but that rebuild must
            // not sit behind the signature guard, or the state stays empty after
            // closeOverdueSheet cleared it, and the reopened drawer holds nothing
            // steady for the whole of that session.
            it('keeps the row when the drawer is reopened and the data has not changed', async () => {
                const { w } = await ready(trio(), T0);
                w.openOverdueSheet();
                w.closeOverdueSheet();
                w.openOverdueSheet();
                // Bravo is not confirmed yet, so he is correctly listed.
                expect(sheetNames(w)).toEqual(['Charlie Test', 'Bravo Test', 'Alpha Test']);
                const rowB = sheetRowFor(w, 'b');

                // The drawer is open. Another device confirms Bravo, seconds later.
                w.__test.setChildrenData(withBravoConfirmed());
                w.updateOverdueUI();

                expect(sheetNames(w)).toEqual(['Charlie Test', 'Bravo Test', 'Alpha Test']);
                expect(sheetRows(w).indexOf(sheetRowFor(w, 'b'))).toBe(1);
                expect(sheetRowFor(w, 'b')).toBe(rowB);
                expect(sheetBox(w, 'b').checked).toBe(true);
                expect(sheetPill(w, 'b').className).toContain('bg-gray-400');
            });

            it('re-renders the drawer when only the location group id changes', async () => {
                const { w } = await ready(trio(), T0);
                w.openOverdueSheet();
                // Two groups sharing a name: the label the signature carries is
                // identical either way, so only the raw location_group_id in
                // getChildSignature can tell the signature that the bar colour
                // (and the group dot) are now stale.
                w.renderLocationGroupSettings([{ id: 1, name: 'Dup' }, { id: 2, name: 'Dup' }]);
                w.updateOverdueUI();
                const barOf = (id) => sheetRowFor(w, id).querySelector('div[aria-hidden="true"]').style.backgroundColor;
                const before = barOf('b');
                expect(sheetRowFor(w, 'b').textContent).toContain('Dup');

                w.__test.setChildrenData([trio()[0], { ...trio()[1], location_group_id: 2 }, trio()[2]]);
                w.updateOverdueUI();

                expect(barOf('b')).not.toBe(before);
            });

            it('escapes a location group name in the drawer', async () => {
                const { w } = await ready(trio(), T0);
                w.openOverdueSheet();
                w.renderLocationGroupSettings([{ id: 1, name: '<b class="x">Room</b>' }]);
                w.updateOverdueUI();

                // Admin-entered name must not reach the drawer as live markup.
                expect(sheetRowFor(w, 'a').querySelector('b.x')).toBeNull();
                expect(sheetRowFor(w, 'a').textContent).toContain('<b class="x">Room</b>');
            });

            it('keeps tied rows in a stable order', async () => {
                const { w } = await ready(trio(), T0);
                w.openOverdueSheet();

                // Same checkout instant for two children: the drawer is sorted
                // oldest-first, so a tie must still resolve deterministically or
                // the two rows would swap between polls.
                const tied = [trio()[0], childAt('b', 15, T0, { first_name: 'Bravo' }), trio()[2]].map(
                    (c) => ({ ...c, checked_out_at_ms: T0 - 15 * 60 * 1000 })
                );
                w.__test.setChildrenData(tied);
                w.updateOverdueUI();
                const first = sheetNames(w);

                w.__test.setChildrenData([...tied].reverse());
                w.updateOverdueUI();

                expect(sheetNames(w)).toEqual(first);
            });

            it('re-renders the drawer when a pickup code changes', async () => {
                const { w } = await ready(trio(), T0);
                w.openOverdueSheet();
                const codeOf = (id) => [...sheetRowFor(w, id).querySelectorAll('div')].map((d) => d.textContent).join('');
                expect(codeOf('b')).toContain('CODEb');

                w.__test.setChildrenData([trio()[0], { ...trio()[1], security_code: 'NEWCODE' }, trio()[2]]);
                w.updateOverdueUI();

                expect(codeOf('b')).toContain('NEWCODE');
            });

            // The un-tick direction. confirmCheckedOut re-applies the checkbox
            // state and sets a confirmation override, so the row re-enters the
            // live overdue set and the retention loop releases it.
            it('returns a tombstone to live when un-ticked from inside the drawer', async () => {
                const { w } = await ready(trio(), T0);
                w.openOverdueSheet();
                w.__test.setChildrenData(withBravoConfirmed());
                w.updateOverdueUI();
                const rowB = sheetRowFor(w, 'b');
                expect(sheetBox(w, 'b').checked).toBe(true);

                const box = w.document.querySelector('#overdue-sheet-list .child-confirmed-checkbox[data-child-id="pc:b"]');
                box.checked = false;
                box.dispatchEvent(new w.Event('change', { bubbles: true }));
                await new Promise((r) => setTimeout(r, 10));
                w.updateOverdueUI();

                // Back in his chronological slot, not appended and not shifted.
                expect(sheetNames(w)).toEqual(['Charlie Test', 'Bravo Test', 'Alpha Test']);
                expect(sheetRowFor(w, 'b')).toBe(rowB);
                expect(sheetRows(w).indexOf(sheetRowFor(w, 'b'))).toBe(1);
                expect(sheetBox(w, 'b').checked).toBe(false);
                expect(sheetPill(w, 'b').className).not.toContain('bg-gray-400');
                expect(w.document.getElementById('overdue-sheet-count').textContent.trim()).toBe('3 overdue');
            });

            // Same release, but arriving as poll data with no local change event:
            // another device cleared the confirmation.
            it('returns a tombstone to live when another device un-confirms', async () => {
                const { w } = await ready(trio(), T0);
                w.openOverdueSheet();
                w.__test.setChildrenData(withBravoConfirmed());
                w.updateOverdueUI();
                expect(sheetBox(w, 'b').checked).toBe(true);

                w.__test.setChildrenData(trio());
                w.updateOverdueUI();

                expect(sheetNames(w)).toEqual(['Charlie Test', 'Bravo Test', 'Alpha Test']);
                expect(sheetRows(w).indexOf(sheetRowFor(w, 'b'))).toBe(1);
                expect(sheetBox(w, 'b').checked).toBe(false);
                expect(sheetPill(w, 'b').className).not.toContain('bg-gray-400');
            });

            it('re-renders a retained tombstone when it is renamed upstream', async () => {
                const { w } = await ready(trio(), T0);
                w.openOverdueSheet();
                w.__test.setChildrenData(withBravoConfirmed());
                w.updateOverdueUI();
                expect(sheetNames(w)).toEqual(['Charlie Test', 'Bravo Test', 'Alpha Test']);

                w.__test.setChildrenData(withBravoConfirmed().map(
                    (c) => (c.planning_center_id === 'b' ? { ...c, first_name: 'Renamed' } : c)
                ));
                w.updateOverdueUI();

                expect(sheetNames(w)).toEqual(['Charlie Test', 'Renamed Test', 'Alpha Test']);
            });

            // A row kept from the cache is not confirmed, so unlike a tombstone
            // its pill still ages through the colour bands off the back of the
            // clock while it sits there.
            it('stops the badge claiming overdue once the last one is confirmed', async () => {
                const { w } = await ready(trio(), T0);
                w.openOverdueSheet();
                const badge = w.document.getElementById('overdue-badge');
                expect(badge.textContent).toContain('3 overdue');

                w.__test.setChildrenData(trio().map((c) => ({ ...c, checked_out_confirmed_at: new Date(T0).toISOString() })));
                w.updateOverdueUI();

                // Three rows are on screen as tombstones but nothing is
                // outstanding. Leaving the badge on its last non-zero value put
                // "3 overdue. Tap to view" directly above a header reading
                // "No overdue" -- the contradiction the header fix removed.
                expect(sheetRows(w).length).toBe(3);
                expect(w.document.getElementById('overdue-sheet-count').textContent.trim()).toBe('No overdue');
                expect(badge.className).toContain('hidden');
                expect(badge.textContent).not.toContain('3 overdue');
                expect(badge.getAttribute('aria-label')).not.toContain('3 overdue');
            });

            it('reports the live overdue count in the header, matching the badge', async () => {
                const { w } = await ready(trio(), T0);
                w.openOverdueSheet();
                const header = () => w.document.getElementById('overdue-sheet-count').textContent.trim();
                expect(header()).toBe('3 overdue');

                w.__test.setChildrenData(withBravoConfirmed());
                w.updateOverdueUI();

                // Three rows are on screen but only two are still overdue, and
                // the badge 40px above says so. The header must not disagree.
                expect(sheetRows(w).length).toBe(3);
                expect(header()).toBe('2 overdue');
                expect(w.document.getElementById('overdue-badge').textContent).toContain('2 overdue');
            });
        });

        // ---- failed poll ----
        // fetchChildrenData's catch empties childrenData. Retention is keyed off
        // childrenData, so a wifi blip used to empty the whole open drawer and
        // then rebuild it from scratch -- the exact reflow this feature exists
        // to prevent.
        describe('when a poll fails while the drawer is open', () => {
            const T0 = 1_700_000_000_000;
            const kids = () => [
                childAt('a', 20, T0, { first_name: 'Alpha' }),
                childAt('b', 15, T0, { first_name: 'Bravo' }),
                childAt('c', 10, T0, { first_name: 'Charlie' }),
            ];
            const sheetNames = (w) => sheetRows(w).map((r) => r.querySelector('.font-bold')?.textContent.trim());

            async function failTheNextPoll(w) {
                w.fetch = async () => { throw new Error('offline'); };
                await w.fetchChildrenData();
                expect(w.__test.getChildrenData()).toEqual([]);
            }

            it('leaves every row on screen', async () => {
                const { w } = await ready(kids(), T0);
                w.openOverdueSheet();
                expect(sheetNames(w)).toEqual(['Charlie Test', 'Bravo Test', 'Alpha Test']);

                await failTheNextPoll(w);
                w.updateOverdueUI();

                expect(sheetRows(w).length).toBe(3);
                expect(sheetNames(w)).toEqual(['Charlie Test', 'Bravo Test', 'Alpha Test']);
            });

            it('keeps the same DOM nodes, so nothing moves under the reader', async () => {
                const { w } = await ready(kids(), T0);
                w.openOverdueSheet();
                const rowB = sheetRowFor(w, 'b');

                await failTheNextPoll(w);
                w.updateOverdueUI();

                expect(sheetRowFor(w, 'b')).toBe(rowB);
            });

            it('does not rewrite the header count', async () => {
                const { w } = await ready(kids(), T0);
                w.openOverdueSheet();
                const header = w.document.getElementById('overdue-sheet-count');
                expect(header.textContent.trim()).toBe('3 overdue');

                await failTheNextPoll(w);
                w.updateOverdueUI();

                // We have no data, so the count is unknown, not zero. Freezing
                // it keeps the header consistent with the badge.
                expect(header.textContent.trim()).toBe('3 overdue');
            });
        });

        // ---- successful but empty poll ----
        // The failed-poll guard must key off whether the poll FAILED, not off
        // childrenData happening to be empty. The production entry links pin
        // checked_out_after=-31m, so a successful poll legitimately reports
        // nothing once every checkout has aged out of the window, and the two
        // cases must not be conflated.
        describe('when a successful poll reports no checkouts while the drawer is open', () => {
            const T0 = 1_700_000_000_000;
            const kids = () => [
                childAt('a', 20, T0, { first_name: 'Alpha' }),
                childAt('b', 15, T0, { first_name: 'Bravo' }),
                childAt('c', 10, T0, { first_name: 'Charlie' }),
            ];

            async function succeedWithNothing(w) {
                w.fetch = async () => ({ ok: true, json: async () => ({ checkins: [], manual_checkins: [] }) });
                await w.fetchChildrenData();
                expect(w.__test.getChildrenData()).toEqual([]);
            }

            // A successful empty payload means no row has payload backing any
            // more, so the drawer empties. That is the same outcome as the
            // failed-poll case, which is why the guard keys on lastPollFailed
            // rather than on emptiness: a blip must NOT empty the drawer, and
            // these three tests are the ones that keep the two apart.
            it('reports zero in the header rather than freezing the old count', async () => {
                const { w } = await ready(kids(), T0);
                w.openOverdueSheet();
                expect(w.document.getElementById('overdue-sheet-count').textContent.trim()).toBe('3 overdue');

                await succeedWithNothing(w);
                w.updateOverdueUI();

                expect(w.document.getElementById('overdue-sheet-count').textContent.trim()).toBe('No overdue');
            });

            it('reports zero on the badge rather than freezing the old count', async () => {
                const { w } = await ready(kids(), T0);
                w.openOverdueSheet();
                const badge = w.document.getElementById('overdue-badge');
                expect(badge.textContent).toContain('3 overdue');

                await succeedWithNothing(w);
                w.updateOverdueUI();

                expect(badge.textContent).not.toContain('3 overdue');
                expect(badge.getAttribute('aria-label')).not.toContain('3 overdue');
            });

            it('empties the drawer, since no row has payload backing any more', async () => {
                const { w } = await ready(kids(), T0);
                w.openOverdueSheet();
                expect(sheetRows(w).length).toBe(3);

                await succeedWithNothing(w);
                w.updateOverdueUI();

                expect(sheetList(w).textContent).toContain('No overdue checkouts');
            });

            it('leaves the drawer frozen but still recomputes the counts', async () => {
                const { w } = await ready(kids(), T0);
                w.openOverdueSheet();

                await succeedWithNothing(w);
                w.updateOverdueUI();
                expect(sheetList(w).textContent).toContain('No overdue checkouts');

                w.__test.setChildrenData([...kids(), childAt('d', 6, T0, { first_name: 'Delta' })]);
                w.updateOverdueUI();

                // Delta is not in the snapshot, so the row set does not change --
                // but the count surfaces are not frozen at zero, because that is
                // what told a blip apart from a real result in the first place.
                expect(sheetList(w).textContent).toContain('No overdue checkouts');
                expect(w.document.getElementById('overdue-sheet-count').textContent.trim()).toBe('4 overdue');
                expect(w.document.getElementById('overdue-badge').textContent).toContain('4 overdue');
            });
        });
    });

    it('refetches and reseeds the flash baseline after re-selecting a group', async () => {
        const w = loadWindow({
            html: '<!doctype html><html><body><div id="children-list"></div></body></html>',
            url: 'http://localhost/?location_group_id=',
            fetchImpl: async () => ({ ok: true, json: async () => [pcChild('a')] })
        });
        await w.fetchChildrenData();
        expect(w.__test.getFlashChildIds().size).toBe(0);

        w.history.replaceState(null, '', '?location_group_id=1');
        await w.fetchChildrenData();

        expect(Array.from(w.__test.getFlashChildIds())).toEqual([]);
        expect(w.document.querySelectorAll('.child-time').length).toBe(1);
    });

    it('unchecking all groups via the UI hides visible children but polling continues for overdue badge', async () => {
        let checkoutsFetchCount = 0;
        const w = locationGroupWindow();
        w.window.fetch = async (url) => {
            if (String(url).includes('/v1/checkins/checkouts')) checkoutsFetchCount++;
            return { ok: true, json: async () => [] };
        };
        w.document.dispatchEvent(new w.Event('DOMContentLoaded'));
        await new Promise(r=>setTimeout(r,10));

        expect(checkoutsFetchCount).toBe(1); // initial children fetch
        w.renderLocationGroupSettings([{ id: 1, name: 'A' }, { id: 2, name: 'B' }]);

        const checks = w.document.querySelectorAll('#location-group-checkboxes input');
        checks.forEach(c => { c.checked = false; c.dispatchEvent(new w.Event('change', {bubbles:true})); });
        await new Promise(r=>setTimeout(r,10));

        expect(w.getSelectedFromURL().isEmpty).toBe(true);
        expect(w.__test.getVisibleChildren().length).toBe(0);

        // While empty, scheduled polls still hit the API (so overdue badge sees all groups)
        const before = checkoutsFetchCount;
        await w.fetchChildrenData();
        await w.fetchChildrenData();
        expect(checkoutsFetchCount).toBe(before + 2);

        // Re-selecting a group shows children again
        w.document.querySelector('#location-group-checkboxes input[data-lg-id="1"]').checked = true;
        w.document.querySelector('#location-group-checkboxes input[data-lg-id="1"]').dispatchEvent(new w.Event('change', {bubbles:true}));
        await new Promise(r=>setTimeout(r,10));
        expect(w.getSelectedFromURL().isEmpty).toBe(false);
    });

    it('resolves location_group_name filters to ids once groups are known', () => {
        const w = loadWindow({
            html: '<!doctype html><html><body><div id="location-group-checkboxes"></div></body></html>',
            url: 'http://localhost/?location_group_name=Grace'
        });
        const sel = w.getSelectedFromURL();
        expect(sel.names.has('Grace')).toBe(true);
        expect(sel.ids.size).toBe(0);

        w.renderLocationGroupSettings([{ id: 1, name: 'Grace' }, { id: 2, name: 'Ada' }]);

        const resolved = w.getSelectedFromURL();
        expect(resolved.ids.has(1)).toBe(true);
        expect(resolved.ids.has(2)).toBe(false);
    });

    it('keeps name-filtered children visible when ids are also selected', () => {
        const w = loadWindow({
            html: '<!doctype html><html><body><div id="location-group-checkboxes"></div></body></html>',
            url: 'http://localhost/?location_group_id=2&location_group_name=Grace'
        });
        w.renderLocationGroupSettings([{ id: 1, name: 'Grace' }, { id: 2, name: 'Ada' }]);
        w.__test.setChildrenData([
            { source: 'planning_center', planning_center_id: '1', location_group_id: 1 },
            { source: 'planning_center', planning_center_id: '2', location_group_id: 2 }
        ]);
        const visible = w.__test.getVisibleChildren().map((c) => c.planning_center_id);
        expect(visible).toEqual(['1', '2']);
    });

    it('escapes location group ids in the settings markup', () => {
        const w = locationGroupWindow();
        w.renderLocationGroupSettings([{ id: '1" onmouseover="alert(1)', name: 'Sneaky' }]);
        const html = w.document.getElementById('location-group-checkboxes').innerHTML;
        expect(html).not.toContain('data-lg-id="1" onmouseover=');
        expect(html).toContain('&quot;');
    });

    it('keeps tied-row order stable across polls with varying API row order', async () => {
        const stamp = new Date().toISOString();
        const rows = () => [
            { ...pcChild('b'), checked_out_at: stamp },
            { ...pcChild('a'), checked_out_at: stamp }
        ];
        let flip = false;
        const w = loadWindow({
            html: '<!doctype html><html><body><div id="children-list"></div></body></html>',
            fetchImpl: async () => {
                flip = !flip;
                const r = rows();
                return { ok: true, json: async () => (flip ? r : [...r].reverse()) };
            }
        });
        await w.fetchChildrenData();
        const first = w.__test.getVisibleChildren().map((c) => c.planning_center_id);
        expect(first).toEqual(['a', 'b']);
        await w.fetchChildrenData();
        expect(w.__test.getVisibleChildren().map((c) => c.planning_center_id)).toEqual(first);
    });
});
