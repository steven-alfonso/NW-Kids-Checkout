// Stable identity and ordering for a checkouts row. Mirrors the vanilla page:
// manual rows are keyed by public_id, Planning Center rows by
// planning_center_id, and anything else falls back to public_id.
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

// Signature of everything that affects a row's rendered output, used to reuse
// row objects across polls when nothing visually changed.
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

// Total ordering for checkouts: recency first, stable child id as tiebreaker.
// Ties are common (second-precision timestamps), so without the tiebreaker the
// order follows whatever row order the API happened to return.
export function compareByCheckoutId(a, b) {
  const aId = a._id || getChildId(a);
  const bId = b._id || getChildId(b);
  if (aId === bId) return 0;
  return aId < bId ? -1 : 1;
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