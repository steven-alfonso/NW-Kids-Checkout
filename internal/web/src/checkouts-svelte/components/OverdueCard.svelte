<script>
    import ConfirmControl from './ConfirmControl.svelte';
    import TimePill from './TimePill.svelte';
    import { getChildId } from '../lib/identity.js';
    import { getLocationGroupColor, overdueGroupLabel } from '../lib/groups.js';

    let { child, nowMs, confirmed, locationGroups, onRequestConfirm } = $props();

    const childId = $derived(getChildId(child));
    const name = $derived(
        `${child.first_name || ''} ${child.last_name || ''}`.trim()
    );
    const code = $derived(
        child.source === 'manual' ? '---' : child.security_code || '----'
    );
    const barColor = $derived(getLocationGroupColor(child.location_group_id));
    const groupLabel = $derived(overdueGroupLabel(child, locationGroups));
</script>

<div class="bg-white rounded-lg shadow flex overflow-hidden">
    <div
        style={`background-color:${barColor}; width:6px; flex-shrink:0`}
        aria-hidden="true"
    ></div>
    <div class="flex-1 py-2 px-3 flex flex-col gap-1">
        <div class="flex items-center gap-2">
            <span
                class="inline-block h-2.5 w-2.5 rounded-sm border border-black/10 shrink-0"
                style={`background-color:${barColor}`}
            ></span>
            <span class="text-xs font-medium text-slate-600">{groupLabel}</span>
            <TimePill {child} {nowMs} {confirmed} variant="overdue" />
        </div>
        <div class="font-bold text-gray-800 text-lg leading-tight">
            {name}{#if child.source === 'manual'}<img
                    src="/static/img/star.svg"
                    alt="Manual checkin"
                    class="inline-block h-5 w-5 ml-2 relative -top-0.5"
                />{/if}
        </div>
        <div class="flex justify-between items-center">
            <div class="text-black text-base">{code}</div>
            <ConfirmControl
                {child}
                {confirmed}
                variant="overdue"
                {onRequestConfirm}
            />
        </div>
    </div>
</div>
