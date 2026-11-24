<script>
    import { getChildId } from '../lib/identity.js';

    let { child, confirmed, variant = 'main', onRequestConfirm } = $props();

    // `local` is the checkbox's own state. It is seeded from the parent's
    // effective confirmation and re-seeded whenever that changes (a poll, an
    // override expiring, or a failed PATCH). Binding it keeps Svelte's DOM
    // bookkeeping honest when the browser toggles the input natively.
    // svelte-ignore state_referenced_locally
    let local = $state(confirmed);
    $effect(() => {
        local = confirmed;
    });

    const childId = $derived(getChildId(child));
    const name = $derived(
        `${child.first_name || ''} ${child.last_name || ''}`.trim()
    );
    const iconClass = $derived(
        variant === 'overdue' ? 'h-9 w-9 block' : 'h-10 w-10 block'
    );

    function handleChange() {
        const accepted = onRequestConfirm(child, local);
        if (!accepted) local = confirmed;
    }
</script>

<label
    class="relative flex items-center cursor-pointer leading-none"
    data-confirmed-label
    data-confirmed-state={confirmed ? 'confirmed' : 'unconfirmed'}
>
    <input
        type="checkbox"
        class="sr-only child-confirmed-checkbox"
        aria-label={`Mark ${name} as confirmed`}
        data-child-id={childId}
        data-planning-center-id={child.planning_center_id || ''}
        data-public-id={child.public_id || ''}
        data-source={child.source || ''}
        bind:checked={local}
        onchange={handleChange}
    />
    <img
        src="/static/img/confirmed-checkbox.svg"
        alt=""
        class={iconClass}
        data-confirmed-icon
    />
</label>
