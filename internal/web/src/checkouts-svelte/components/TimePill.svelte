<script>
    import { getChildId } from '../lib/identity.js';
    import { minutesAgoLabel, timePillClass } from '../lib/time.js';

    let { child, nowMs, confirmed, variant = 'main' } = $props();

    const childId = $derived(getChildId(child));
    const checkedOutAtMs = $derived(
        child.checked_out_at_ms ?? 0
    );
    const label = $derived(minutesAgoLabel(checkedOutAtMs, nowMs));
    const pillClass = $derived(timePillClass(checkedOutAtMs, confirmed, nowMs));
    const classes = $derived(
        variant === 'overdue'
            ? `ml-auto text-white text-xs px-2.5 py-1 rounded-md child-time ${pillClass}`
            : `text-white transition-colors duration-1000 px-1.5 py-0 rounded-md text-base child-time ${pillClass}`
    );
</script>

<span class={classes} data-child-id={childId}>{label}</span>
