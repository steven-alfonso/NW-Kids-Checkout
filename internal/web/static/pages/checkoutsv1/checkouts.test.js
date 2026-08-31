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
});
