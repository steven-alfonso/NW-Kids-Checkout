import { describe, it, expect } from 'vitest';
import {
  OVERDUE_MINUTES,
  CONFIRM_OVERRIDE_TTL_MS,
  calculateMinutesAgoFromTimestamp,
  compareByCheckoutId,
  formatHeaderTime,
  getCheckedOutTimestamp,
  getChildId,
  getLocationGroupColor,
  getTimePillClass,
  normalizeCheckoutsResponse,
  overdueGroupLabel,
  sortCheckouts
} from '../checkouts.js';
import {
  getSelectedFromURL,
  replaceSelectionInURL,
  replaceWithAllSelectionInURL,
  replaceWithEmptySelectionInURL,
  selectionHasFilter
} from '../urlFilters.js';

const BASE = Date.parse('2026-01-15T12:00:00.000Z');

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

describe('checkouts Vue helpers', () => {
  it('builds ids by source and stable ordering', () => {
    expect(getChildId({ source: 'manual', public_id: 'm1' })).toBe('manual:m1');
    expect(getChildId({ source: 'manual' })).toBe('');
    expect(getChildId({ source: 'planning_center', planning_center_id: 'p1' })).toBe('pc:p1');
    expect(getChildId({ planning_center_id: 'p2' })).toBe('pc:p2');
    expect(getChildId({ public_id: 'pub' })).toBe('public:pub');
    expect(compareByCheckoutId({ planning_center_id: 'a' }, { planning_center_id: 'b' })).toBeLessThan(0);
    expect(compareByCheckoutId({ planning_center_id: 'b' }, { planning_center_id: 'a' })).toBeGreaterThan(0);
  });

  it('normalizes payloads and drops children without usable timestamps', () => {
    expect(normalizeCheckoutsResponse(null)).toEqual([]);
    expect(normalizeCheckoutsResponse({ checkins: null, manual_checkins: 1 })).toEqual([]);
    expect(
      normalizeCheckoutsResponse({
        checkins: [child({ planning_center_id: 'pc' })],
        manual_checkins: [child({ source: 'manual', public_id: 'm' })]
      }).map(getChildId)
    ).toEqual(['pc:pc', 'manual:m']);

    const sorted = sortCheckouts([
      { ...child({ planning_center_id: 'none' }), checked_out_at_ms: 0 },
      { ...child({ planning_center_id: 'old' }), checked_out_at_ms: BASE - 1 },
      { ...child({ planning_center_id: 'new' }), checked_out_at_ms: BASE }
    ]);
    expect(sorted.map(getChildId)).toEqual(['pc:new', 'pc:old']);
  });

  it('parses times, formats labels, and applies pill thresholds', () => {
    expect(getCheckedOutTimestamp('2026-01-15T11:59:00.000Z')).toBe(BASE - 60_000);
    expect(getCheckedOutTimestamp(null)).toBe(0);
    expect(getCheckedOutTimestamp('not-a-date')).toBe(0);
    expect(calculateMinutesAgoFromTimestamp(BASE - 59_000, BASE)).toBe('0 min ago');
    expect(calculateMinutesAgoFromTimestamp(BASE - 3.9 * 60_000, BASE)).toBe('3 min ago');
    expect(calculateMinutesAgoFromTimestamp(BASE + 60_000, BASE)).toBe('0 min ago');
    expect(getTimePillClass(BASE - 3.99 * 60_000, false, BASE)).toBe('bg-green-500');
    expect(getTimePillClass(BASE - 4 * 60_000, false, BASE)).toBe('bg-yellow-500');
    expect(getTimePillClass(BASE - 7.99 * 60_000, false, BASE)).toBe('bg-yellow-500');
    expect(getTimePillClass(BASE - 8 * 60_000, false, BASE)).toBe('bg-red-500');
    expect(getTimePillClass(BASE - 60_000, true, BASE)).toBe('bg-gray-400');
    expect(OVERDUE_MINUTES).toBe(5);
    expect(CONFIRM_OVERRIDE_TTL_MS).toBe(15_000);
  });

  it('maps group colours and labels with fallbacks', () => {
    expect(getLocationGroupColor(null)).toBe('#9CA3AF');
    expect(getLocationGroupColor('nope')).toBe('#9CA3AF');
    expect(getLocationGroupColor(1)).toBe('#332288');
    expect(getLocationGroupColor(9)).toBe('#332288');
    expect(overdueGroupLabel({ location_group_id: 1 }, [{ id: 1, name: 'Nursery' }])).toBe('Nursery');
    expect(overdueGroupLabel({ location_group_id: 2 }, [])).toBe('Group 2');
    expect(overdueGroupLabel({ location_group_id: null }, [])).toBe('Unassigned');
  });

  it('formats the header time as HH:MM', () => {
    expect(formatHeaderTime(BASE)).toMatch(/^\d{2}:\d{2}$/);
  });
});

describe('URL filter helpers', () => {
  it('reads ids, names, unassigned, and the empty sentinel', () => {
    const groups = [{ id: 2, name: 'Elementary' }];
    const parsed = getSelectedFromURL('?location_group_id=1,2&location_group_name=Elementary&include_unassigned=1', groups);
    expect([...parsed.ids]).toEqual([1, 2]);
    expect(parsed.names.has('Elementary')).toBe(true);
    expect(parsed.includeUnassigned).toBe(true);
    expect(parsed.isEmpty).toBe(false);
    expect(selectionHasFilter(parsed)).toBe(true);

    expect(getSelectedFromURL('?location_group_id=', groups).isEmpty).toBe(true);
    expect(getSelectedFromURL('?location_group_name=Elementary', []).ids.size).toBe(0);
    expect([...getSelectedFromURL('?location_group_name=Elementary', groups).ids]).toEqual([2]);
  });

  it('replaces selection while preserving unrelated params', () => {
    expect(replaceSelectionInURL('?checked_out_after=-31m&location_group_id=9', new Set([1, 2]), true)).toBe(
      '?checked_out_after=-31m&location_group_id=1&location_group_id=2&include_unassigned=1'
    );
    expect(replaceWithEmptySelectionInURL('?checked_out_after=-31m')).toBe(
      '?checked_out_after=-31m&location_group_id='
    );
    expect(replaceWithAllSelectionInURL('?checked_out_after=-31m&location_group_id=1&include_unassigned=1')).toBe(
      '?checked_out_after=-31m'
    );
  });
});
