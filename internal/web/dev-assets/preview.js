// Dev-only asset (see internal/web/dev-assets/README.md for how this is
// served and how to add new dev tools).
//
// Dev helper: loadPreviewData() seeds demo checkouts so you can visually
// validate pill colors and the confirm checkbox without waiting for real time.
// Call from the browser console on the checkouts page: loadPreviewData()
// Use the ?debug param for extra console logging while previewing.
function loadPreviewData() {
    API_CALL_BLOCKS.fetchChildrenData = true;

    const ago = (minutes) => Date.now() - minutes * 60 * 1000;
    childrenData = [
        { first_name: 'Fresh', last_name: 'Kid', security_code: '1111', source: 'planning_center', planning_center_id: 'demo-0', location_group_id: 1, checked_out_at_ms: ago(0), checked_out_confirmed_at: null },
        { first_name: 'Edge', last_name: 'Kid', security_code: '2222', source: 'planning_center', planning_center_id: 'demo-4', location_group_id: 2, checked_out_at_ms: ago(3.9), checked_out_confirmed_at: null },
        { first_name: 'Late', last_name: 'Kid', security_code: '3333', source: 'planning_center', planning_center_id: 'demo-8', location_group_id: 1, checked_out_at_ms: ago(7.9), checked_out_confirmed_at: null },
        { first_name: 'Done', last_name: 'Kid', security_code: '4444', source: 'planning_center', planning_center_id: 'demo-c', location_group_id: null, checked_out_at_ms: ago(2), checked_out_confirmed_at: '2024-01-01T00:00:00Z' },
        { first_name: 'Manu', last_name: 'Checkin', source: 'manual', public_id: 'demo-m0', location_group_id: 1, checked_out_at_ms: ago(0), checked_out_confirmed_at: null },
        { first_name: 'Manual', last_name: 'Edge', source: 'manual', public_id: 'demo-m4', location_group_id: 2, checked_out_at_ms: ago(3.9), checked_out_confirmed_at: null },
        { first_name: 'Manual', last_name: 'Late', source: 'manual', public_id: 'demo-m8', location_group_id: 1, checked_out_at_ms: ago(7.9), checked_out_confirmed_at: null },
        { first_name: 'Manual', last_name: 'Done', source: 'manual', public_id: 'demo-mc', location_group_id: null, checked_out_at_ms: ago(2), checked_out_confirmed_at: '2024-01-01T00:00:00Z' }
    ];

    updateUI();
}