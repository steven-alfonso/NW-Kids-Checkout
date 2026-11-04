// Pure helpers for the checkouts board. Kept free of Vue so the behaviour the
// conformance suite pins (ordering, labels, colours) is easy to read and test.

export const OVERDUE_MINUTES = 5;
export const CONFIRM_OVERRIDE_TTL_MS = 15000;
export const FLASH_RESET_DELAY_MS = 4000;

export const GRAY_UNASSIGNED = '#9CA3AF';
export const PAUL_TOL_MUTED = [
  '#332288',
  '#117733',
  '#CC6677',
  '#44AA99',
  '#882255',
  '#88CCEE',
  '#DDCC77',
  '#AA4499'
];

export function getChildId(child) {
  if (!child) return '';
  if (child.source === 'manual') {
    return child.public_id ? `manual:${child.public_id}` : '';
  }
  if (child.source === 'planning_center') {
    return child.planning_center_id ? `pc:${child.planning_center_id}` : '';
  }
  if (child.planning_center_id) return `pc:${child.planning_center_id}`;
  if (child.public_id) return `public:${child.public_id}`;
  return '';
}

export function normalizeCheckoutsResponse(data) {
  if (Array.isArray(data)) return data;

  const normalizeList = (value) => {
    if (Array.isArray(value)) return value;
    if (Array.isArray(value?.checkins)) return value.checkins;
    return [];
  };

  return [
    ...normalizeList(data?.checkins),
    ...normalizeList(data?.manual_checkins)
  ];
}

export function getCheckedOutTimestamp(value) {
  if (!value) return 0;
  const parsed = Date.parse(value);
  return Number.isNaN(parsed) ? 0 : parsed;
}

export function calculateMinutesAgoFromTimestamp(checkedOutAtMs, nowMs) {
  if (!checkedOutAtMs) return '0 min ago';
  const now = typeof nowMs === 'number' ? nowMs : Date.now();
  const diffInMinutes = Math.max(
    0,
    Math.floor((now - checkedOutAtMs) / (1000 * 60))
  );
  return `${diffInMinutes} min ago`;
}

export function compareByCheckoutId(a, b) {
  const aId = getChildId(a);
  const bId = getChildId(b);
  if (aId === bId) return 0;
  return aId < bId ? -1 : 1;
}

export function sortCheckouts(children) {
  return children
    .filter((child) => child.checked_out_at_ms)
    .sort(
      (a, b) =>
        b.checked_out_at_ms - a.checked_out_at_ms ||
        compareByCheckoutId(a, b)
    );
}

export function getTimePillClass(checkedOutAtMs, confirmed, nowMs) {
  if (confirmed) return 'bg-gray-400';
  if (!checkedOutAtMs) return 'bg-green-500';

  const now = typeof nowMs === 'number' ? nowMs : Date.now();
  const diffInMinutes = Math.max(0, (now - checkedOutAtMs) / (1000 * 60));

  if (diffInMinutes >= 8) return 'bg-red-500';
  if (diffInMinutes >= 4) return 'bg-yellow-500';
  return 'bg-green-500';
}

export function getLocationGroupColor(locationGroupId) {
  if (locationGroupId == null) return GRAY_UNASSIGNED;
  const id = Number(locationGroupId);
  if (!Number.isFinite(id)) return GRAY_UNASSIGNED;
  const index = Math.abs(id - 1) % PAUL_TOL_MUTED.length;
  return PAUL_TOL_MUTED[index];
}

export function overdueGroupLabel(child, locationGroups) {
  if (child.location_group_id == null) return 'Unassigned';
  const group = locationGroups.find(
    (candidate) => Number(candidate.id) === Number(child.location_group_id)
  );
  return group ? group.name : `Group ${child.location_group_id}`;
}

export function formatHeaderTime(nowMs) {
  return new Date(nowMs).toLocaleTimeString('en-US', {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false
  });
}
