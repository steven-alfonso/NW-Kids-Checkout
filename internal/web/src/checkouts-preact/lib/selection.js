// Location-group selection is the only filter persisted in the URL. The empty
// `location_group_id=` sentinel means "no groups selected" and renders an empty
// board, not "all groups".
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