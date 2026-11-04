import { describe, it, expect } from 'vitest';
import { JSDOM } from 'jsdom';
import { normalizeCheckoutsResponse } from '../api.js';
import { getLocationGroupColor, overdueGroupLabel, GRAY_UNASSIGNED } from '../groups.js';
import { compareByCheckoutId, getChildId } from '../identity.js';
import { readMenuHtml } from '../menu.js';
import {
    allGroupsSelected,
    readSelection,
    selectionHasFilter
} from '../selection.js';
import {
    CONFIRM_OVERRIDE_TTL_MS,
    OVERDUE_MINUTES,
    formatClock,
    getCheckedOutTimestamp,
    isOverdueAt,
    minutesAgoLabel,
    timePillClass
} from '../time.js';

const BASE = Date.parse('2026-01-15T12:00:00.000Z');

describe('api.normalizeCheckoutsResponse', () => {
    it('accepts a bare array', () => {
        expect(normalizeCheckoutsResponse([{ id: 1 }])).toEqual([{ id: 1 }]);
    });

    it('merges checkins and manual_checkins', () => {
        expect(
            normalizeCheckoutsResponse({
                checkins: [{ id: 'pc' }],
                manual_checkins: [{ id: 'manual' }]
            })
        ).toEqual([{ id: 'pc' }, { id: 'manual' }]);
    });

    it('treats missing or invalid lists as empty', () => {
        expect(normalizeCheckoutsResponse(null)).toEqual([]);
        expect(normalizeCheckoutsResponse({ checkins: null })).toEqual([]);
        expect(normalizeCheckoutsResponse({ checkins: {}, manual_checkins: 1 })).toEqual([]);
    });
});

describe('groups', () => {
    it('uses gray for null and unknown ids', () => {
        expect(getLocationGroupColor(null)).toBe(GRAY_UNASSIGNED);
        expect(getLocationGroupColor('nope')).toBe(GRAY_UNASSIGNED);
    });

    it('maps ids deterministically into the muted palette', () => {
        expect(getLocationGroupColor(1)).toBe('#332288');
        expect(getLocationGroupColor(9)).toBe('#332288');
        expect(getLocationGroupColor(2)).toBe('#117733');
    });

    it('labels overdue rows from loaded groups, with fallbacks', () => {
        const groups = [{ id: 1, name: 'Nursery' }];
        expect(overdueGroupLabel({ location_group_id: 1 }, groups)).toBe('Nursery');
        expect(overdueGroupLabel({ location_group_id: 2 }, groups)).toBe('Group 2');
        expect(overdueGroupLabel({ location_group_id: null }, groups)).toBe('Unassigned');
    });
});

describe('identity', () => {
    it('keys manual, Planning Center, and fallback rows', () => {
        expect(getChildId({ source: 'manual', public_id: 'm1' })).toBe('manual:m1');
        expect(getChildId({ source: 'manual' })).toBe('');
        expect(getChildId({ source: 'planning_center', planning_center_id: 'pc1' })).toBe(
            'pc:pc1'
        );
        expect(getChildId({ planning_center_id: 'pc2' })).toBe('pc:pc2');
        expect(getChildId({ public_id: 'p3' })).toBe('public:p3');
        expect(getChildId(null)).toBe('');
    });

    it('sorts equal timestamps by stable child id', () => {
        const a = { planning_center_id: 'a' };
        const b = { planning_center_id: 'b' };
        expect(compareByCheckoutId(a, b)).toBeLessThan(0);
        expect(compareByCheckoutId(b, a)).toBeGreaterThan(0);
        expect(compareByCheckoutId(a, a)).toBe(0);
    });
});

describe('selection', () => {
    it('reads repeated and comma-separated ids', () => {
        const selection = readSelection('?location_group_id=1,2&location_group_id=3', []);
        expect([...selection.ids]).toEqual([1, 2, 3]);
        expect(selection.isEmpty).toBe(false);
        expect(selectionHasFilter(selection)).toBe(true);
    });

    it('reads include_unassigned in both accepted forms', () => {
        expect(readSelection('?include_unassigned=1', []).includeUnassigned).toBe(true);
        expect(readSelection('?include_unassigned=true', []).includeUnassigned).toBe(true);
        expect(readSelection('?include_unassigned=0', []).includeUnassigned).toBe(false);
    });

    it('treats the empty location_group_id sentinel as empty', () => {
        const selection = readSelection('?location_group_id=', []);
        expect(selection.isEmpty).toBe(true);
        expect(selectionHasFilter(selection)).toBe(false);
    });

    it('resolves names once groups are loaded', () => {
        const groups = [{ id: 2, name: 'Elementary' }];
        expect([...readSelection('?location_group_name=Elementary', []).ids]).toEqual([]);
        expect([...readSelection('?location_group_name=Elementary', groups).ids]).toEqual([2]);
    });

    it('only reports all-selected when groups have loaded', () => {
        const groups = [{ id: 1, name: 'Nursery' }];
        const noFilter = readSelection('', groups);
        expect(allGroupsSelected(noFilter, groups, false)).toBe(false);
        expect(allGroupsSelected(noFilter, groups, true)).toBe(true);

        const filtered = readSelection('?location_group_id=1&include_unassigned=1', groups);
        expect(allGroupsSelected(filtered, groups, true)).toBe(true);
        expect(allGroupsSelected(readSelection('?location_group_id=1', groups), groups, true)).toBe(
            false
        );
        expect(allGroupsSelected(readSelection('?location_group_id=', []), [], true)).toBe(false);
    });
});

describe('time', () => {
    it('parses timestamps and rejects missing or invalid values', () => {
        expect(getCheckedOutTimestamp('2026-01-15T11:59:00.000Z')).toBe(BASE - 60_000);
        expect(getCheckedOutTimestamp(null)).toBe(0);
        expect(getCheckedOutTimestamp('not-a-date')).toBe(0);
    });

    it('formats whole minutes, clamping future timestamps', () => {
        expect(minutesAgoLabel(BASE - 59_000, BASE)).toBe('0 min ago');
        expect(minutesAgoLabel(BASE - 60_000, BASE)).toBe('1 min ago');
        expect(minutesAgoLabel(BASE - 3.9 * 60_000, BASE)).toBe('3 min ago');
        expect(minutesAgoLabel(BASE + 60_000, BASE)).toBe('0 min ago');
        expect(minutesAgoLabel(0, BASE)).toBe('0 min ago');
    });

    it('uses the 4/8 minute pill thresholds and gray when confirmed', () => {
        expect(timePillClass(BASE - 3.99 * 60_000, false, BASE)).toBe('bg-green-500');
        expect(timePillClass(BASE - 4 * 60_000, false, BASE)).toBe('bg-yellow-500');
        expect(timePillClass(BASE - 7.99 * 60_000, false, BASE)).toBe('bg-yellow-500');
        expect(timePillClass(BASE - 8 * 60_000, false, BASE)).toBe('bg-red-500');
        expect(timePillClass(BASE - 60_000, true, BASE)).toBe('bg-gray-400');
        expect(timePillClass(0, false, BASE)).toBe('bg-green-500');
    });

    it('uses an inclusive five-minute overdue boundary', () => {
        expect(OVERDUE_MINUTES).toBe(5);
        expect(isOverdueAt(BASE - (5 * 60_000 - 1), BASE)).toBe(false);
        expect(isOverdueAt(BASE - 5 * 60_000, BASE)).toBe(true);
        expect(isOverdueAt(0, BASE)).toBe(false);
    });

    it('formats the clock as HH:MM', () => {
        expect(formatClock(BASE)).toMatch(/^\d{2}:\d{2}$/);
        expect(CONFIRM_OVERRIDE_TTL_MS).toBe(15_000);
    });
});

describe('menu handoff', () => {
    it('reads the server menu markup and removes the source node', () => {
        const dom = new JSDOM(
            '<div id="checkouts-menu-source"><a id="login-link">Log In</a></div>'
        );
        const html = readMenuHtml(dom.window.document);
        expect(html).toContain('login-link');
        expect(dom.window.document.getElementById('checkouts-menu-source')).toBeNull();
    });

    it('returns an empty string when the source is absent', () => {
        const dom = new JSDOM('<div></div>');
        expect(readMenuHtml(dom.window.document)).toBe('');
    });
});
