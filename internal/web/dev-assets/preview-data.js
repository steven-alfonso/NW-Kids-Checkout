// Demo rows for internal/web/dev-assets/preview.js. Kept in the Svelte source
// tree so the preview helper and its unit test share one definition.
export function createPreviewChildren(nowMs = Date.now()) {
    const ago = (minutes) => nowMs - minutes * 60 * 1000;
    const row = (overrides, minutes) => ({
        location_group_id: null,
        checked_out_confirmed_at: null,
        ...overrides,
        checked_out_at_ms: ago(minutes),
        checked_out_at: new Date(ago(minutes)).toISOString()
    });

    return [
        row(
            {
                first_name: 'Fresh',
                last_name: 'Kid',
                security_code: '1111',
                source: 'planning_center',
                planning_center_id: 'demo-0',
                location_group_id: 1
            },
            0
        ),
        row(
            {
                first_name: 'Edge',
                last_name: 'Kid',
                security_code: '2222',
                source: 'planning_center',
                planning_center_id: 'demo-4',
                location_group_id: 2
            },
            3.9
        ),
        row(
            {
                first_name: 'Overdue',
                last_name: 'Kid',
                security_code: '5555',
                source: 'planning_center',
                planning_center_id: 'demo-5',
                location_group_id: 2
            },
            6
        ),
        row(
            {
                first_name: 'Late',
                last_name: 'Kid',
                security_code: '3333',
                source: 'planning_center',
                planning_center_id: 'demo-8',
                location_group_id: 1
            },
            7.9
        ),
        row(
            {
                first_name: 'Done',
                last_name: 'Kid',
                security_code: '4444',
                source: 'planning_center',
                planning_center_id: 'demo-c',
                checked_out_confirmed_at: '2024-01-01T00:00:00Z'
            },
            2
        ),
        row(
            {
                first_name: 'Manu',
                last_name: 'Checkin',
                source: 'manual',
                public_id: 'demo-m0'
            },
            0
        ),
        row(
            {
                first_name: 'Manual',
                last_name: 'Edge',
                source: 'manual',
                public_id: 'demo-m4'
            },
            3.9
        ),
        row(
            {
                first_name: 'Manual',
                last_name: 'Overdue',
                source: 'manual',
                public_id: 'demo-m5'
            },
            6
        ),
        row(
            {
                first_name: 'Manual',
                last_name: 'Late',
                source: 'manual',
                public_id: 'demo-m8'
            },
            7.9
        ),
        row(
            {
                first_name: 'Manual',
                last_name: 'Done',
                source: 'manual',
                public_id: 'demo-mc',
                checked_out_confirmed_at: '2024-01-01T00:00:00Z'
            },
            2
        )
    ];
}
