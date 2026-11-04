<script>
    import ConfirmControl from './ConfirmControl.svelte';
    import TimePill from './TimePill.svelte';
    import { getLocationGroupColor } from '../lib/groups.js';

    let { child, nowMs, confirmed, flash = false, onRequestConfirm } = $props();

    const name = $derived(
        `${child.first_name || ''} ${child.last_name || ''}`.trim()
    );
    const code = $derived(
        child.source === 'manual' ? '---' : child.security_code || '----'
    );
    const barColor = $derived(getLocationGroupColor(child.location_group_id));
</script>

<div
    class="bg-white rounded-lg shadow-[0_0_10px_rgba(0,0,0,0.25)] flex"
    class:child-card-flash={flash}
>
    <div
        class="rounded-l-lg"
        style={`background-color:${barColor}; width:6px; flex-shrink:0`}
        aria-hidden="true"
    ></div>
    <div class="flex-1 py-2.5 px-4 flex flex-col justify-center">
        <div class="font-bold text-gray-800 text-2xl mb-0">
            {name}{#if child.source === 'manual'}<img
                    src="/static/img/star.svg"
                    alt="Manual checkin"
                    class="inline-block h-5 w-5 ml-2 relative -top-0.5"
                />{/if}
        </div>
        <div class="flex justify-between items-center">
            <div class="text-black text-xl">
                {code}
            </div>
            <div class="flex items-center gap-3">
                <TimePill {child} {nowMs} {confirmed} variant="main" />
                <ConfirmControl
                    {child}
                    {confirmed}
                    variant="main"
                    {onRequestConfirm}
                />
            </div>
        </div>
    </div>
</div>
