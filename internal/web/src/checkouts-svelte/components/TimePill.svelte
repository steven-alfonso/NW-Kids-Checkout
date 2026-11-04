<script>
    import { getChildId } from '../lib/identity.js';
    import { minutesAgoLabel, timePillClass, timePillClasses } from '../lib/time.js';

    let { child, nowMs, confirmed, variant = 'main' } = $props();

    const childId = $derived(getChildId(child));
    const checkedOutAtMs = $derived(
        child.checked_out_at_ms ?? 0
    );
    const label = $derived(minutesAgoLabel(checkedOutAtMs, nowMs));
    const pillClass = $derived(timePillClass(checkedOutAtMs, confirmed, nowMs));
    const classes = $derived(timePillClasses(variant, pillClass));
</script>

<span class={classes} data-child-id={childId}>{label}</span>
