// Fixtures for the checkouts conformance suite.
//
// Times are absolute and paired with a fixed page.clock, so every "N min ago"
// label is deterministic. Keep the default set stable: the boot/order/overdue
// tests name these children explicitly.
export const BASE_TIME = new Date('2026-01-15T12:00:00.000Z');

export const GROUPS = [
  { id: 1, name: 'Nursery' },
  { id: 2, name: 'Elementary' }
];

export function minutesAgo(minutes) {
  return new Date(BASE_TIME.getTime() - minutes * 60_000).toISOString();
}

export function minutesFromNow(minutes) {
  return new Date(BASE_TIME.getTime() + minutes * 60_000).toISOString();
}

export function makeChild(overrides = {}) {
  return {
    source: 'planning_center',
    planning_center_id: 'pc-child',
    public_id: '',
    first_name: 'Test',
    last_name: 'Child',
    security_code: '0000',
    location_id: 10,
    location_group_id: 1,
    checked_out_at: minutesAgo(1),
    checked_out_confirmed_at: null,
    ...overrides
  };
}

export function makeManualChild(overrides = {}) {
  return makeChild({
    source: 'manual',
    planning_center_id: '',
    public_id: 'manual-child',
    first_name: 'Manual',
    last_name: 'Child',
    security_code: '',
    location_group_id: null,
    ...overrides
  });
}

export function defaultChildren() {
  return [
    makeChild({
      planning_center_id: 'fresh',
      first_name: 'Fresh',
      last_name: 'Kid',
      security_code: '1111',
      location_group_id: 1,
      checked_out_at: minutesAgo(0.1)
    }),
    makeManualChild({
      public_id: 'manu',
      first_name: 'Manu',
      last_name: 'Checkin',
      checked_out_at: minutesAgo(0.2)
    }),
    makeChild({
      planning_center_id: 'unassigned',
      first_name: 'Unassigned',
      last_name: 'Kid',
      security_code: '1212',
      location_group_id: null,
      checked_out_at: minutesAgo(1)
    }),
    makeChild({
      planning_center_id: 'done',
      first_name: 'Done',
      last_name: 'Kid',
      security_code: '4444',
      location_group_id: 1,
      checked_out_at: minutesAgo(2),
      checked_out_confirmed_at: minutesAgo(1)
    }),
    makeChild({
      planning_center_id: 'edge',
      first_name: 'Edge',
      last_name: 'Kid',
      security_code: '2222',
      location_group_id: 2,
      checked_out_at: minutesAgo(3.9)
    }),
    makeChild({
      planning_center_id: 'overdue',
      first_name: 'Overdue',
      last_name: 'Kid',
      security_code: '5555',
      location_group_id: 2,
      checked_out_at: minutesAgo(6)
    }),
    makeChild({
      planning_center_id: 'late',
      first_name: 'Late',
      last_name: 'Kid',
      security_code: '3333',
      location_group_id: 1,
      checked_out_at: minutesAgo(7.9)
    })
  ];
}

export function childKey(child) {
  if (!child) return '';
  if (child.source === 'manual') {
    return child.public_id ? `manual:${child.public_id}` : '';
  }
  if (child.planning_center_id) return `pc:${child.planning_center_id}`;
  if (child.public_id) return `public:${child.public_id}`;
  return '';
}
