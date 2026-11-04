import { describe, expect, it } from 'vitest';
import {
  calculateMinutesAgoFromTimestamp,
  filterVisibleChildren,
  getChildId,
  getOverdueChildren,
  getSelectedFromURL,
  getTimePillClass,
  prepareChildren
} from './core.js';

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
});
