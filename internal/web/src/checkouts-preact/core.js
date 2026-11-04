export const OVERDUE_MINUTES = 5;
export const CONFIRM_OVERRIDE_TTL_MS = 15000;
export const FLASH_RESET_DELAY_MS = 4000;

const GRAY_UNASSIGNED = '#9CA3AF';
const PAUL_TOL_MUTED = [
  '#332288',
  '#117733',
  '#CC6677',
  '#44AA99',
  '#882255',
  '#88CCEE',
  '#DDCC77',
  '#AA4499'
];

const PILL_CLASSES = ['bg-gray-400', 'bg-green-500', 'bg-yellow-500', 'bg-red-500'];

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

export function getChildSignature(child) {
  if (!child) return 'empty';
  return JSON.stringify([
    child.source || '',
    child.planning_center_id || '',
    child.public_id || '',
    child.checked_out_at || '',
    child.checked_out_at_ms ?? '',
    child.checked_out_confirmed_at || '',
    child.first_name || '',
    child.last_name || '',
    child.security_code || '',
    child.location_group_id != null ? String(child.location_group_id) : ''
  ]);
}

export function compareByCheckoutId(a, b) {
  const aId = a._id || getChildId(a);
  const bId = b._id || getChildId(b);
  if (aId === bId) return 0;
  return aId < bId ? -1 : 1;
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
  if (Number.isNaN(parsed)) return 0;
  return parsed;
}

export function addCheckedOutTimestamps(children) {
  return children.map((child) => ({
    ...child,
    checked_out_at_ms: getCheckedOutTimestamp(child.checked_out_at)
  }));
}

export function sortChildren(children) {
  return children
    .filter((child) => child.checked_out_at_ms)
    .sort(
      (a, b) =>
        (b.checked_out_at_ms - a.checked_out_at_ms) || compareByCheckoutId(a, b)
    );
}

export function prepareChildren(data) {
  return sortChildren(addCheckedOutTimestamps(normalizeCheckoutsResponse(data)));
}

// Reuse row objects across polls when their display signature is unchanged.
// Stable identities let memoized rows skip work and keep focus/DOM untouched.
export function reconcileChildren(previous, next) {
  if (previous.length === 0) return next;

  const previousById = new Map();
  previous.forEach((child) => {
    const childId = getChildId(child);
    if (childId) previousById.set(childId, child);
  });

  let changed = previous.length !== next.length;
  const merged = next.map((child, index) => {
    const old = previousById.get(getChildId(child));
    const value =
      old && getChildSignature(old) === getChildSignature(child) ? old : child;
    if (value !== previous[index]) changed = true;
    return value;
  });
  return changed ? merged : previous;
}

export function getMinutesAgoFromTimestamp(checkedOutAtMs, nowMs) {
  if (!checkedOutAtMs) return 0;
  const now = typeof nowMs === 'number' ? nowMs : Date.now();
  return Math.max(
    0,
    Math.floor((now - checkedOutAtMs) / (1000 * 60))
  );
}

export function formatMinutesAgo(minutes) {
  return `${minutes} min ago`;
}

export function calculateMinutesAgoFromTimestamp(checkedOutAtMs, nowMs) {
  return formatMinutesAgo(getMinutesAgoFromTimestamp(checkedOutAtMs, nowMs));
}

export function getTimePillClassFromMinutes(
  checkedOutAtMs,
  confirmed,
  minutes
) {
  if (confirmed) return PILL_CLASSES[0];
  if (!checkedOutAtMs) return PILL_CLASSES[1];
  if (minutes >= 8) return 'bg-red-500';
  if (minutes >= 4) return 'bg-yellow-500';
  return 'bg-green-500';
}

export function getTimePillClass(checkedOutAtMs, confirmed, nowMs) {
  return getTimePillClassFromMinutes(
    checkedOutAtMs,
    confirmed,
    getMinutesAgoFromTimestamp(checkedOutAtMs, nowMs)
  );
}

export function getLocationGroupColor(locationGroupId) {
  if (locationGroupId == null) return GRAY_UNASSIGNED;
  const id = Number(locationGroupId);
  if (!Number.isFinite(id)) return GRAY_UNASSIGNED;
  const idx = Math.abs(id - 1) % PAUL_TOL_MUTED.length;
  return PAUL_TOL_MUTED[idx];
}

export function getSelectedFromURL(search, groups = []) {
  const params = new URLSearchParams(search);
  const ids = new Set();
  const names = new Set();

  params.getAll('location_group_id').forEach((value) => {
    value.split(',').forEach((part) => {
      const trimmed = part.trim();
      if (!trimmed) return;
      const id = Number(trimmed);
      if (Number.isFinite(id) && id > 0) ids.add(id);
    });
  });

  const includeUnassigned = ['1', 'true'].includes(
    params.get('include_unassigned')
  );

  params.getAll('location_group_name').forEach((value) => {
    value.split(',').forEach((part) => {
      const trimmed = part.trim();
      if (trimmed) names.add(trimmed);
    });
  });

  const hasLocationGroupParam =
    params.has('location_group_id') ||
    params.has('location_group_name') ||
    params.has('include_unassigned');
  const isEmpty =
    hasLocationGroupParam &&
    ids.size === 0 &&
    names.size === 0 &&
    !includeUnassigned;

  if (names.size > 0 && groups.length > 0) {
    groups.forEach((group) => {
      const id = Number(group.id);
      if (names.has(group.name) && Number.isFinite(id) && id > 0) {
        ids.add(id);
      }
    });
  }

  return { ids, includeUnassigned, names, isEmpty };
}

export function hasSelectionFilter(selection) {
  return (
    selection.ids.size > 0 ||
    selection.names.size > 0 ||
    selection.includeUnassigned
  );
}

export function buildLocationGroupSearch(
  search,
  { ids = [], includeUnassigned = false, empty = false } = {}
) {
  const params = new URLSearchParams(search);
  params.delete('location_group_id');
  params.delete('location_group_name');
  params.delete('include_unassigned');

  if (empty) {
    params.append('location_group_id', '');
    return params.toString();
  }

  ids.forEach((id) => params.append('location_group_id', String(id)));
  if (includeUnassigned) params.append('include_unassigned', '1');
  return params.toString();
}

export function isGroupSelected(selection, value) {
  if (selection.isEmpty) return false;
  if (!hasSelectionFilter(selection)) return true;
  if (value === 'unassigned') return selection.includeUnassigned;
  const id = Number(value);
  return Number.isFinite(id) && selection.ids.has(id);
}

export function getSelectAllLabel(selection, groups, groupsLoaded) {
  if (!groupsLoaded) return 'Select all';
  if (selection.isEmpty) return 'Select all';
  if (!hasSelectionFilter(selection)) return 'Deselect all';

  const allChecked =
    selection.includeUnassigned &&
    groups.every((group) => selection.ids.has(Number(group.id)));
  return allChecked ? 'Deselect all' : 'Select all';
}

export function matchesGroupSelection(child, selection) {
  if (selection.isEmpty) return false;
  if (selection.ids.size === 0 && !selection.includeUnassigned) return true;
  if (child.source === 'manual') return true;
  if (child.location_group_id == null) return selection.includeUnassigned;
  return selection.ids.has(Number(child.location_group_id));
}

export function filterVisibleChildren(
  children,
  { searchQuery = '', hideConfirmed = false, selection }
) {
  let visible = children;
  if (hideConfirmed) {
    visible = visible.filter((child) => !child.checked_out_confirmed_at);
  }

  const query = searchQuery.trim().toLowerCase();
  if (query) {
    visible = visible.filter((child) => {
      const name =
        `${child.first_name || ''} ${child.last_name || ''}`.toLowerCase();
      const code = (child.security_code || '').toLowerCase();
      return name.includes(query) || code.includes(query);
    });
  }

  return visible.filter((child) => matchesGroupSelection(child, selection));
}

export function getOverdueChildren(children, isConfirmed, nowMs) {
  const now = typeof nowMs === 'number' ? nowMs : Date.now();
  const cutoff = OVERDUE_MINUTES * 60 * 1000;
  return children
    .filter((child) => {
      if (!child.checked_out_at_ms) return false;
      if (isConfirmed(child)) return false;
      return now - child.checked_out_at_ms >= cutoff;
    })
    .sort(
      (a, b) =>
        b.checked_out_at_ms - a.checked_out_at_ms || compareByCheckoutId(a, b)
    );
}

export function getOverdueGroupLabel(child, groups) {
  if (child.location_group_id == null) return 'Unassigned';
  const group = groups.find(
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
