// Colors come from the Paul Tol muted palette, the same set the vanilla page
// exported on window. Kept as a pure function so both cards and group chips can
// derive the same swatch from a location_group_id.
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

export function getLocationGroupColor(locationGroupId) {
    if (locationGroupId == null) return GRAY_UNASSIGNED;
    const id = Number(locationGroupId);
    if (!Number.isFinite(id)) return GRAY_UNASSIGNED;
    const idx = Math.abs(id - 1) % PAUL_TOL_MUTED.length;
    return PAUL_TOL_MUTED[idx];
}

export function overdueGroupLabel(child, locationGroups) {
    if (child.location_group_id == null) return 'Unassigned';
    const group = locationGroups.find(
        (candidate) => Number(candidate.id) === Number(child.location_group_id)
    );
    return group ? group.name : `Group ${child.location_group_id}`;
}
