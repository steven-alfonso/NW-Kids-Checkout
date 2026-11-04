// Stable identity and ordering for a checkouts row. Mirrors the vanilla page:
// manual rows are keyed by public_id, Planning Center rows by
// planning_center_id, and anything else falls back to public_id.
export function getChildId(child) {
    if (!child) return '';
    if (child.source === 'manual') return child.public_id ? `manual:${child.public_id}` : '';
    if (child.source === 'planning_center') {
        return child.planning_center_id ? `pc:${child.planning_center_id}` : '';
    }
    if (child.planning_center_id) return `pc:${child.planning_center_id}`;
    if (child.public_id) return `public:${child.public_id}`;
    return '';
}

// Total ordering for checkouts: recency first, stable child id as tiebreaker.
// Ties are common (second-precision timestamps), so without the tiebreaker the
// order follows whatever row order the API happened to return.
export function compareByCheckoutId(a, b) {
    const aId = a._id || getChildId(a);
    const bId = b._id || getChildId(b);
    if (aId === bId) return 0;
    return aId < bId ? -1 : 1;
}
