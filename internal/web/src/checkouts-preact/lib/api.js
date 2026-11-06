import { OVERDUE_MINUTES, getCheckedOutTimestamp } from './time.js';
import { compareByCheckoutId } from './identity.js';

// API response normalization and core data transformations.
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