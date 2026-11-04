// Location-group selection lives in the query string. Only this filter is
// persisted there; search and hide-confirmed stay in memory, matching the
// current page.

function valuesOf(params, key) {
  const values = new Set();
  params.getAll(key).forEach((raw) => {
    raw.split(',').forEach((part) => {
      const trimmed = part.trim();
      if (trimmed) values.add(trimmed);
    });
  });
  return values;
}

export function getSelectedFromURL(search, locationGroups = []) {
  const params = new URLSearchParams(search || '');
  const ids = new Set();

  valuesOf(params, 'location_group_id').forEach((value) => {
    const id = Number(value);
    if (Number.isFinite(id) && id > 0) ids.add(id);
  });

  const names = valuesOf(params, 'location_group_name');
  const includeUnassigned = ['1', 'true'].includes(
    params.get('include_unassigned')
  );
  const hasGroupParam =
    params.has('location_group_id') ||
    params.has('location_group_name') ||
    params.has('include_unassigned');
  const isEmpty =
    hasGroupParam && ids.size === 0 && names.size === 0 && !includeUnassigned;

  if (names.size > 0 && locationGroups.length > 0) {
    locationGroups.forEach((group) => {
      const id = Number(group.id);
      if (names.has(group.name) && Number.isFinite(id) && id > 0) ids.add(id);
    });
  }

  return { ids, names, includeUnassigned, isEmpty, hasGroupParam };
}

export function selectionHasFilter(selection) {
  return (
    selection.ids.size > 0 ||
    selection.names.size > 0 ||
    selection.includeUnassigned
  );
}

export function replaceSelectionInURL(search, ids, includeUnassigned) {
  const params = new URLSearchParams(search || '');
  params.delete('location_group_id');
  params.delete('location_group_name');
  params.delete('include_unassigned');
  if (ids && ids.size) {
    [...ids].forEach((id) => params.append('location_group_id', String(id)));
  }
  if (includeUnassigned) params.append('include_unassigned', '1');
  return `${params.toString() ? '?' : ''}${params.toString()}`;
}

export function replaceWithEmptySelectionInURL(search) {
  const params = new URLSearchParams(search || '');
  params.delete('location_group_id');
  params.delete('location_group_name');
  params.delete('include_unassigned');
  params.append('location_group_id', '');
  return `${params.toString() ? '?' : ''}${params.toString()}`;
}

export function replaceWithAllSelectionInURL(search) {
  const params = new URLSearchParams(search || '');
  params.delete('location_group_id');
  params.delete('location_group_name');
  params.delete('include_unassigned');
  return `${params.toString() ? '?' : ''}${params.toString()}`;
}
