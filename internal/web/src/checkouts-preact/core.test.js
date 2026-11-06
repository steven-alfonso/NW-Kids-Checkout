import { describe, expect, it } from 'vitest';
import {
  TIME_PILL_BASE_CLASS,
  TIME_PILL_OVERDUE_BASE_CLASS,
  formatHeaderTime,
  formatMinutesAgo,
  getMinutesAgoFromTimestamp,
  getTimePillClass,
  getTimePillClassFromMinutes,
  calculateMinutesAgoFromTimestamp,
  getCheckedOutTimestamp
} from './lib/time.js';
import {
  getChildId,
  getChildSignature,
  reconcileChildren
} from './lib/identity.js';
import {
  normalizeCheckoutsResponse,
  prepareChildren,
  getOverdueChildren
} from './lib/api.js';
import {
  buildLocationGroupSearch,
  filterVisibleChildren,
  getSelectAllLabel,
  getSelectedFromURL,
  isGroupSelected
} from './lib/selection.js';
import {
  getOverdueGroupLabel
} from './lib/groups.js';

function child(overrides = {}) {
  return {
    source: 'planning_center',
    planning_center_id: 'pc-1',
    first_name: 'Ada',
    last_name: 'Lovelace',
    security_code: '1234',
    location_group_id: 1,
    checked_out_at: '2026-01-15T12:00:00Z',
    checked_out_confirmed_at: null,
    ...overrides
  };
}

describe('checkouts-preact core', () => {
  it('builds child ids by source', () => {
    expect(getChildId({ source: 'manual', public_id: 'm1' })).toBe('manual:m1');
    expect(getChildId({ source: 'manual' })).toBe('');
    expect(getChildId({ source: 'planning_center', planning_center_id: 'p1' })).toBe('pc:p1');
    expect(getChildId({ planning_center_id: 'p2' })).toBe('pc:p2');
    expect(getChildId({ public_id: 'pub' })).toBe('public:pub');
  });

  it('normalizes, timestamps, and stably sorts checkouts', () => {
    const tied = '2026-01-15T12:00:00Z';
    const prepared = prepareChildren({
      checkins: [
        child({ planning_center_id: 'b', checked_out_at: tied }),
        child({ planning_center_id: 'a', checked_out_at: tied }),
        child({ planning_center_id: 'none', checked_out_at: null })
      ],
      manual_checkins: {
        checkins: [child({ source: 'manual', public_id: 'm', checked_out_at: '2026-01-15T12:01:00Z' })]
      }
    });

    expect(prepared.map(getChildId)).toEqual(['manual:m', 'pc:a', 'pc:b']);
    expect(prepared[0].checked_out_at_ms).toBe(Date.parse('2026-01-15T12:01:00Z'));
  });

  it('formats clock labels and pill colors', () => {
    const base = Date.parse('2026-01-15T12:00:00Z');
    expect(calculateMinutesAgoFromTimestamp(base, base + 3 * 60_000)).toBe('3 min ago');
    expect(calculateMinutesAgoFromTimestamp(0, base)).toBe('0 min ago');
    expect(getTimePillClass(base, false, base + 3 * 60_000)).toBe('bg-green-500');
    expect(getTimePillClass(base, false, base + 4 * 60_000)).toBe('bg-yellow-500');
    expect(getTimePillClass(base, false, base + 8 * 60_000)).toBe('bg-red-500');
    expect(getTimePillClass(base, true, base + 20 * 60_000)).toBe('bg-gray-400');
  });

  it('fades the time pill in both variants', () => {
    for (const baseClass of [TIME_PILL_BASE_CLASS, TIME_PILL_OVERDUE_BASE_CLASS]) {
      expect(baseClass).toContain('transition-colors');
      expect(baseClass).toContain('duration-1000');
    }
  });

  it('derives integer minutes and pill colors from the same value', () => {
    const base = Date.parse('2026-01-15T12:00:00Z');
    expect(getMinutesAgoFromTimestamp(base, base + 3 * 60_000 + 59_999)).toBe(3);
    expect(getMinutesAgoFromTimestamp(base, base - 60_000)).toBe(0);
    expect(formatMinutesAgo(3)).toBe('3 min ago');
    expect(getTimePillClassFromMinutes(base, false, 3)).toBe('bg-green-500');
    expect(getTimePillClassFromMinutes(base, false, 4)).toBe('bg-yellow-500');
    expect(getTimePillClassFromMinutes(base, false, 8)).toBe('bg-red-500');
    expect(getTimePillClassFromMinutes(base, true, 20)).toBe('bg-gray-400');
  });

  it('reuses unchanged child rows across polls', () => {
    const children = prepareChildren([
      child({ planning_center_id: 'one' }),
      child({ planning_center_id: 'two', checked_out_at: '2026-01-15T11:59:00Z' })
    ]);
    const same = prepareChildren([
      child({ planning_center_id: 'one' }),
      child({ planning_center_id: 'two', checked_out_at: '2026-01-15T11:59:00Z' })
    ]);

    const reconciled = reconcileChildren(children, same);
    expect(reconciled).toBe(children);
    expect(reconciled[0]).toBe(children[0]);

    same[0] = { ...same[0], first_name: 'Grace' };
    const changed = reconcileChildren(children, same);
    expect(changed).not.toBe(children);
    expect(changed[0]).toBe(same[0]);
    expect(changed[1]).toBe(children[1]);
  });

  it('parses group filters from the URL and resolves names', () => {
    const groups = [{ id: 1, name: 'Nursery' }, { id: 2, name: 'Elementary' }];
    const parsed = getSelectedFromURL('?location_group_id=2&location_group_name=Elementary&include_unassigned=1', groups);

    expect([...parsed.ids]).toEqual([2]);
    expect(parsed.names.has('Elementary')).toBe(true);
    expect(parsed.includeUnassigned).toBe(true);
    expect(parsed.isEmpty).toBe(false);
    expect(getSelectedFromURL('?location_group_id=', groups).isEmpty).toBe(true);
  });

  it('filters the board while keeping manual rows and overdue unfiltered', () => {
    const selection = getSelectedFromURL('?location_group_id=1', []);
    const children = prepareChildren([
      child({ planning_center_id: 'one', location_group_id: 1, checked_out_at: '2026-01-15T11:54:00Z' }),
      child({ planning_center_id: 'two', location_group_id: 2, checked_out_at: '2026-01-15T11:55:00Z' }),
      child({ source: 'manual', public_id: 'm', location_group_id: null, checked_out_at: '2026-01-15T11:56:00Z' }),
      child({ planning_center_id: 'unassigned', location_group_id: null, checked_out_at: '2026-01-15T11:57:00Z' })
    ]);

    expect(filterVisibleChildren(children, { selection }).map(getChildId)).toEqual([
      'manual:m',
      'pc:one'
    ]);

    const now = Date.parse('2026-01-15T12:00:00Z');
    expect(getOverdueChildren(children, () => false, now).map(getChildId)).toEqual([
      'pc:two',
      'pc:one'
    ]);
  });

  it('treats exactly five minutes as overdue', () => {
    const now = Date.parse('2026-01-15T12:00:00Z');
    const children = prepareChildren([
      child({ planning_center_id: 'five', checked_out_at: '2026-01-15T11:55:00Z' }),
      child({ planning_center_id: 'almost', checked_out_at: '2026-01-15T11:55:01Z' })
    ]);

    expect(getOverdueChildren(children, () => false, now).map(getChildId)).toEqual(['pc:five']);
  });

  it('normalizes missing/invalid payloads to an empty list', () => {
    expect(normalizeCheckoutsResponse(null)).toEqual([]);
    expect(normalizeCheckoutsResponse({ checkins: null, manual_checkins: 1 })).toEqual([]);
  });

  it('changes the signature when identity or display fields change', () => {
    const base = child();
    expect(getChildSignature(base)).toBe(getChildSignature({ ...base }));
    expect(getChildSignature(base)).not.toBe(
      getChildSignature({ ...base, first_name: 'Grace' })
    );
    expect(getChildSignature(base)).not.toBe(
      getChildSignature({ ...base, planning_center_id: 'pc-2' })
    );
  });

  it('builds group-filter query strings without dropping other params', () => {
    expect(
      buildLocationGroupSearch('?checked_out_after=-31m', {
        ids: [1, 2],
        includeUnassigned: true
      })
    ).toBe('checked_out_after=-31m&location_group_id=1&location_group_id=2&include_unassigned=1');
    expect(buildLocationGroupSearch('?location_group_id=1', { empty: true })).toBe(
      'location_group_id='
    );
  });

  it('derives select-all state and group checkbox state', () => {
    const groups = [{ id: 1, name: 'Nursery' }, { id: 2, name: 'Elementary' }];
    const none = getSelectedFromURL('', groups);
    expect(getSelectAllLabel(none, groups, false)).toBe('Select all');
    expect(getSelectAllLabel(none, groups, true)).toBe('Deselect all');
    expect(isGroupSelected(none, 1)).toBe(true);
    expect(isGroupSelected(none, 'unassigned')).toBe(true);

    const empty = getSelectedFromURL('?location_group_id=', groups);
    expect(getSelectAllLabel(empty, groups, true)).toBe('Select all');
    expect(isGroupSelected(empty, 1)).toBe(false);

    const partial = getSelectedFromURL('?location_group_id=1', groups);
    expect(getSelectAllLabel(partial, groups, true)).toBe('Select all');
    expect(isGroupSelected(partial, 1)).toBe(true);
    expect(isGroupSelected(partial, 2)).toBe(false);

    const all = getSelectedFromURL('?location_group_id=1&location_group_id=2&include_unassigned=1', groups);
    expect(getSelectAllLabel(all, groups, true)).toBe('Deselect all');
    expect(isGroupSelected(all, 'unassigned')).toBe(true);
  });

  it('labels overdue groups and formats the header time', () => {
    const groups = [{ id: 1, name: 'Nursery' }];
    expect(getOverdueGroupLabel({ location_group_id: 1 }, groups)).toBe('Nursery');
    expect(getOverdueGroupLabel({ location_group_id: 2 }, groups)).toBe('Group 2');
    expect(getOverdueGroupLabel({ location_group_id: null }, groups)).toBe('Unassigned');
    expect(formatHeaderTime(Date.parse('2026-01-15T12:00:00Z'))).toMatch(/^\d{2}:\d{2}$/);
  });
});
