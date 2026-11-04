// Location-group selection is the only filter persisted in the URL. The empty
// `location_group_id=` sentinel means "no groups selected" and renders an empty
// board, not "all groups".
export function readSelection(search, locationGroups) {
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

    params.getAll('location_group_name').forEach((value) => {
        value.split(',').forEach((part) => {
            const trimmed = part.trim();
            if (trimmed) names.add(trimmed);
        });
    });

    const includeParam = params.get('include_unassigned');
    const includeUnassigned = includeParam === '1' || includeParam === 'true';
    const hasLocationGroupParam =
        params.has('location_group_id') ||
        params.has('location_group_name') ||
        params.has('include_unassigned');
    const isEmpty =
        hasLocationGroupParam && ids.size === 0 && names.size === 0 && !includeUnassigned;

    // Resolve names against asynchronously loaded groups. Until they arrive the
    // page intentionally renders unfiltered, which is the vanilla cold-load
    // behavior the conformance suite documents.
    if (names.size > 0 && locationGroups.length > 0) {
        locationGroups.forEach((group) => {
            const id = Number(group.id);
            if (names.has(group.name) && Number.isFinite(id) && id > 0) ids.add(id);
        });
    }

    return { ids, includeUnassigned, names, isEmpty };
}

export function selectionHasFilter(selection) {
    return (
        selection.ids.size > 0 ||
        selection.names.size > 0 ||
        selection.includeUnassigned
    );
}
