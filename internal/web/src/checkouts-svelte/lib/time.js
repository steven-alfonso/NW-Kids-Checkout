export const OVERDUE_MINUTES = 5;
export const CONFIRM_OVERRIDE_TTL_MS = 15000;

const PILL_BG_CLASSES = ['bg-gray-400', 'bg-green-500', 'bg-yellow-500', 'bg-red-500'];

export const TIME_PILL_BASE_CLASS =
    'text-white transition-colors duration-1000 px-1.5 py-0 rounded-md text-base child-time';
export const TIME_PILL_OVERDUE_BASE_CLASS =
    'ml-auto text-white transition-colors duration-1000 text-xs px-2.5 py-1 rounded-md child-time';

export function timePillClasses(variant, pillClass) {
    const base =
        variant === 'overdue' ? TIME_PILL_OVERDUE_BASE_CLASS : TIME_PILL_BASE_CLASS;
    return `${base} ${pillClass}`;
}

export function getCheckedOutTimestamp(value) {
    if (!value) return 0;
    const parsed = Date.parse(value);
    if (Number.isNaN(parsed)) return 0;
    return parsed;
}

export function minutesAgoLabel(checkedOutAtMs, nowMs) {
    if (!checkedOutAtMs) return '0 min ago';
    const now = typeof nowMs === 'number' ? nowMs : Date.now();
    const diffInMinutes = Math.max(0, Math.floor((now - checkedOutAtMs) / (1000 * 60)));
    return `${diffInMinutes} min ago`;
}

export function timePillClass(checkedOutAtMs, confirmed, nowMs) {
    if (confirmed) return PILL_BG_CLASSES[0];
    if (!checkedOutAtMs) return PILL_BG_CLASSES[1];

    const now = typeof nowMs === 'number' ? nowMs : Date.now();
    const diffInMinutes = Math.max(0, (now - checkedOutAtMs) / (1000 * 60));

    if (diffInMinutes >= 8) return PILL_BG_CLASSES[3];
    if (diffInMinutes >= 4) return PILL_BG_CLASSES[2];
    return PILL_BG_CLASSES[1];
}

export function formatClock(nowMs) {
    const date = new Date(nowMs);
    const hours = String(date.getHours()).padStart(2, '0');
    const minutes = String(date.getMinutes()).padStart(2, '0');
    return `${hours}:${minutes}`;
}

export function isOverdueAt(checkedOutAtMs, nowMs) {
    if (!checkedOutAtMs) return false;
    return nowMs - checkedOutAtMs >= OVERDUE_MINUTES * 60 * 1000;
}
