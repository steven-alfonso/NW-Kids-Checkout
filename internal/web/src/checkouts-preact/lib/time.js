// Constants and time helpers for the checkouts page.
// Mirrors the Svelte PR 38 time module layout.

export const OVERDUE_MINUTES = 5;
export const CONFIRM_OVERRIDE_TTL_MS = 15000;
export const FLASH_RESET_DELAY_MS = 4000;

const PILL_CLASSES = ['bg-gray-400', 'bg-green-500', 'bg-yellow-500', 'bg-red-500'];

export const TIME_PILL_BASE_CLASS =
  'text-white transition-colors duration-1000 px-1.5 py-0 rounded-md text-base child-time';
export const TIME_PILL_OVERDUE_BASE_CLASS =
  'ml-auto text-white transition-colors duration-1000 text-xs px-2.5 py-1 rounded-md child-time';

export function getCheckedOutTimestamp(value) {
  if (!value) return 0;
  const parsed = Date.parse(value);
  if (Number.isNaN(parsed)) return 0;
  return parsed;
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

export function formatHeaderTime(nowMs) {
  return new Date(nowMs).toLocaleTimeString('en-US', {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false
  });
}