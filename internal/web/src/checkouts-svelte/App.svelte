<script>
    import { onMount, tick } from 'svelte';
    import ChildCard from './components/ChildCard.svelte';
    import OverdueCard from './components/OverdueCard.svelte';
    import { searchPanel } from './actions/search-panel.js';
    import { normalizeCheckoutsResponse } from './lib/api.js';
    import { compareByCheckoutId, getChildId } from './lib/identity.js';
    import { getLocationGroupColor } from './lib/groups.js';
    import { readSelection, selectionHasFilter, allGroupsSelected } from './lib/selection.js';
    import {
        CONFIRM_OVERRIDE_TTL_MS,
        formatClock,
        getCheckedOutTimestamp,
        isOverdueAt
    } from './lib/time.js';

    let { menuHtml = '' } = $props();

    const API_URL = '';
    const DEBUG = new URLSearchParams(window.location.search).has('debug');
    const FLASH_RESET_DELAY_MS = 4000;

    let childrenData = $state([]);
    let locationGroups = $state([]);
    let groupsLoaded = $state(false);
    let loaded = $state(false);
    let pollFailed = $state(false);
    let nowMs = $state(Date.now());
    let searchQuery = $state('');
    let hideConfirmed = $state(false);
    let searchOpen = $state(false);
    let urlSearch = $state(window.location.search);
    let overrides = $state({});
    let flashIds = $state({});
    let sheetOpen = $state(false);
    let sheetItems = $state([]);
    let sheetCount = $state(0);
    let badgeJiggle = $state(false);

    let childrenListEl;
    let sheetEl;
    let backdropEl;

    const confirmingIds = new Set();
    let knownChildIds = new Set();
    let lastFetchParams = null;
    let flashTimeoutId = null;
    let fetchController = null;
    let requestSeq = 0;
    let previewBlocked = false;
    let sheetSnapshotIds = new Set();
    let returnFocus = null;
    let bodyScrollLock = null;
    let lastOverdueCount = 0;

    const selection = $derived(readSelection(urlSearch, locationGroups));
    const hasGroupFilter = $derived(selectionHasFilter(selection));

    const liveOverdue = $derived.by(() => {
        const now = nowMs;
        return childrenData
            .filter(
                (child) =>
                    child.checked_out_at_ms &&
                    !isChildConfirmed(child) &&
                    isOverdueAt(child.checked_out_at_ms, now)
            )
            .sort(
                (a, b) =>
                    b.checked_out_at_ms - a.checked_out_at_ms ||
                    compareByCheckoutId(a, b)
            );
    });
    // A failed poll clears childrenData, so this reaches zero immediately.
    // Vanilla hid the badge on the next 1s tick instead; the difference is
    // under one second and is not covered by the conformance contract.
    const overdueCount = $derived(liveOverdue.length);

    const visibleChildren = $derived.by(() => {
        let children = childrenData;
        if (hideConfirmed) {
            children = children.filter((child) => !child.checked_out_confirmed_at);
        }
        const query = searchQuery.trim().toLowerCase();
        if (query) {
            children = children.filter((child) => {
                const name =
                    `${child.first_name || ''} ${child.last_name || ''}`.toLowerCase();
                const code = (child.security_code || '').toLowerCase();
                return name.includes(query) || code.includes(query);
            });
        }
        if (selection.isEmpty) return [];
        if (selection.ids.size > 0 || selection.includeUnassigned) {
            children = children.filter((child) => {
                if (child.source === 'manual') return true;
                if (child.location_group_id == null) return selection.includeUnassigned;
                return selection.ids.has(Number(child.location_group_id));
            });
        }
        return children.slice(0, 100);
    });

    const searchActive = $derived(searchQuery.trim().length > 0);
    const emptyMessage = $derived(
        searchActive
            ? 'No matching children'
            : hideConfirmed
              ? 'No unconfirmed children'
              : 'No children called yet'
    );

    const everyGroupChecked = $derived(
        allGroupsSelected(selection, locationGroups, groupsLoaded)
    );
    const selectAllLabel = $derived(everyGroupChecked ? 'Deselect all' : 'Select all');

    function isGroupChecked(id) {
        if (selection.isEmpty) return false;
        if (!hasGroupFilter) return true;
        if (id === 'unassigned') return selection.includeUnassigned;
        return selection.ids.has(Number(id));
    }

    function isChildConfirmed(child) {
        const childId = getChildId(child);
        if (!childId) return Boolean(child.checked_out_confirmed_at);
        const entry = overrides[childId];
        if (entry && nowMs - entry.timestamp <= CONFIRM_OVERRIDE_TTL_MS) {
            return Boolean(entry.confirmed);
        }
        return Boolean(child.checked_out_confirmed_at);
    }

    function setOverride(childId, confirmed) {
        if (!childId) return;
        overrides = {
            ...overrides,
            [childId]: { confirmed: Boolean(confirmed), timestamp: Date.now() }
        };
    }

    function clearOverride(childId) {
        if (!childId || !(childId in overrides)) return;
        const next = { ...overrides };
        delete next[childId];
        overrides = next;
    }

    function reconcileOverrides() {
        const confirmedById = new Map();
        childrenData.forEach((child) => {
            const childId = getChildId(child);
            if (!childId) return;
            confirmedById.set(childId, Boolean(child.checked_out_confirmed_at));
        });
        let next = overrides;
        let changed = false;
        Object.entries(overrides).forEach(([childId, entry]) => {
            const current = confirmedById.get(childId);
            if (typeof current === 'boolean' && current === entry.confirmed) {
                if (!changed) {
                    next = { ...overrides };
                    changed = true;
                }
                delete next[childId];
            }
        });
        if (changed) overrides = next;
    }

    function computeNewChildIds(children) {
        const currentIds = new Set(children.map(getChildId).filter(Boolean));
        const newlyAppeared = new Set();
        if (knownChildIds.size > 0) {
            currentIds.forEach((childId) => {
                if (!knownChildIds.has(childId)) newlyAppeared.add(childId);
            });
        }
        knownChildIds = currentIds;
        return newlyAppeared;
    }

    function clearFlashStyles() {
        flashIds = {};
        if (flashTimeoutId) {
            clearTimeout(flashTimeoutId);
            flashTimeoutId = null;
        }
    }

    function flashNewChildren(ids) {
        flashIds = Object.fromEntries([...ids].map((childId) => [childId, true]));
        if (flashTimeoutId) clearTimeout(flashTimeoutId);
        flashTimeoutId = setTimeout(clearFlashStyles, FLASH_RESET_DELAY_MS);
    }

    function updateSheetCount() {
        if (sheetOpen && !pollFailed) sheetCount = liveOverdue.length;
    }

    function syncSheetFromChildren() {
        if (!sheetOpen || pollFailed) return;
        sheetItems = childrenData
            .filter((child) => sheetSnapshotIds.has(getChildId(child)))
            .sort(
                (a, b) =>
                    (b.checked_out_at_ms ?? 0) - (a.checked_out_at_ms ?? 0) ||
                    compareByCheckoutId(a, b)
            );
        sheetCount = liveOverdue.length;
    }

    async function fetchChildrenData() {
        if (previewBlocked) return;
        if (fetchController) fetchController.abort();
        const controller = new AbortController();
        fetchController = controller;
        const seq = ++requestSeq;

        const params = new URLSearchParams(window.location.search);
        const outParams = new URLSearchParams();
        outParams.append('limit', params.get('limit') || '100');
        const checkedOutAfter = params.get('checked_out_after');
        if (checkedOutAfter) outParams.append('checked_out_after', checkedOutAfter);

        const fetchSignature = window.location.search;
        const filterChanged = fetchSignature !== lastFetchParams;

        try {
            const response = await fetch(
                `${API_URL}/v1/checkins/checkouts/?${outParams.toString()}`,
                { signal: controller.signal }
            );
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            const data = await response.json();
            if (seq !== requestSeq) return;

            const combined = normalizeCheckoutsResponse(data)
                .map((child) => ({
                    ...child,
                    checked_out_at_ms: getCheckedOutTimestamp(child.checked_out_at)
                }))
                .filter((child) => child.checked_out_at_ms)
                .sort(
                    (a, b) =>
                        b.checked_out_at_ms - a.checked_out_at_ms ||
                        compareByCheckoutId(a, b)
                );

            childrenData = combined;
            pollFailed = false;
            loaded = true;

            if (filterChanged) {
                knownChildIds = new Set(childrenData.map(getChildId).filter(Boolean));
                clearFlashStyles();
                lastFetchParams = fetchSignature;
            } else {
                const newIds = computeNewChildIds(childrenData);
                if (newIds.size > 0) flashNewChildren(newIds);
            }

            reconcileOverrides();
            syncSheetFromChildren();
            updateSheetCount();

            if (DEBUG) console.log(`Fetched ${combined.length} children`);
        } catch (error) {
            if (error?.name === 'AbortError') return;
            if (seq !== requestSeq) return;
            console.error('Error fetching children data:', error);
            childrenData = [];
            pollFailed = true;
            loaded = true;
            // The open drawer deliberately keeps its rows on a failed poll.
        } finally {
            if (fetchController === controller) fetchController = null;
        }
    }

    async function fetchLocationGroups() {
        try {
            const response = await fetch(`${API_URL}/v1/location_groups`, {
                credentials: 'same-origin'
            });
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            const data = await response.json();
            if (Array.isArray(data)) locationGroups = data;
            else if (Array.isArray(data?.location_groups)) locationGroups = data.location_groups;
            else locationGroups = [];
            groupsLoaded = true;
        } catch (error) {
            console.error('Error fetching location groups:', error);
        }
    }

    function requestConfirm(child, confirmed) {
        const childId = getChildId(child);
        if (confirmingIds.has(childId)) return false;

        setOverride(childId, confirmed);

        let endpoint = '';
        if (child.source === 'manual') {
            if (!child.public_id) {
                console.error('Missing public_id for manual confirmation');
                clearOverride(childId);
                return false;
            }
            endpoint = `${API_URL}/v1/checkins/manual-checkins/${encodeURIComponent(child.public_id)}/checked_out_confirmed`;
        } else {
            if (child.source && child.source !== 'planning_center') {
                console.warn(`Skipping confirmation for source: ${child.source}`);
                clearOverride(childId);
                return false;
            }
            if (!child.planning_center_id) {
                console.error('Missing planning_center_id for confirmation');
                clearOverride(childId);
                return false;
            }
            endpoint = `${API_URL}/v1/checkins/${encodeURIComponent(child.planning_center_id)}/checked_out_confirmed`;
        }

        confirmingIds.add(childId);
        updateSheetCount();

        (async () => {
            try {
                const response = await fetch(endpoint, {
                    method: 'PATCH',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ confirmed })
                });
                if (!response.ok) {
                    throw new Error(`HTTP error! status: ${response.status}`);
                }
                updateSheetCount();
            } catch (error) {
                console.error('Error confirming checkout:', error);
                clearOverride(childId);
                updateSheetCount();
            } finally {
                confirmingIds.delete(childId);
            }
        })();

        return true;
    }

    function writeGroupParams(ids, includeUnassigned, empty) {
        const params = new URLSearchParams(window.location.search);
        params.delete('location_group_id');
        params.delete('location_group_name');
        params.delete('include_unassigned');
        if (empty) {
            params.append('location_group_id', '');
        } else {
            ids.forEach((id) => params.append('location_group_id', String(id)));
            if (includeUnassigned) params.append('include_unassigned', '1');
        }
        const newSearch = params.toString();
        const newUrl = newSearch ? `?${newSearch}` : window.location.pathname;
        history.replaceState(null, '', newUrl);
        urlSearch = window.location.search;
    }

    function onGroupToggle(id, checked) {
        const allSelected = !selection.isEmpty && !hasGroupFilter;
        const ids = new Set(
            allSelected ? locationGroups.map((group) => Number(group.id)) : selection.ids
        );
        let includeUnassigned = allSelected ? true : selection.includeUnassigned;

        if (id === 'unassigned') {
            includeUnassigned = checked;
        } else if (checked) {
            ids.add(Number(id));
        } else {
            ids.delete(Number(id));
        }

        writeGroupParams(ids, includeUnassigned, ids.size === 0 && !includeUnassigned);
        fetchChildrenData();
    }

    function onSelectAll() {
        const params = new URLSearchParams(window.location.search);
        params.delete('location_group_id');
        params.delete('location_group_name');
        params.delete('include_unassigned');
        if (everyGroupChecked) params.append('location_group_id', '');
        const newSearch = params.toString();
        const newUrl = newSearch ? `?${newSearch}` : window.location.pathname;
        history.replaceState(null, '', newUrl);
        urlSearch = window.location.search;
        fetchChildrenData();
    }

    async function toggleSearch() {
        searchOpen = !searchOpen;
        if (searchOpen) {
            await tick();
            document.getElementById('search-input')?.focus();
        }
    }

    function lockBodyScroll() {
        if (bodyScrollLock || !document.body) return;
        bodyScrollLock = { scrollY: window.scrollY || 0 };
        const style = document.body.style;
        style.position = 'fixed';
        style.top = `-${bodyScrollLock.scrollY}px`;
        style.left = '0';
        style.right = '0';
        style.width = '100%';
        style.overflow = 'hidden';
    }

    function unlockBodyScroll() {
        if (!bodyScrollLock || !document.body) return;
        const scrollY = bodyScrollLock.scrollY;
        bodyScrollLock = null;
        const style = document.body.style;
        style.position = '';
        style.top = '';
        style.left = '';
        style.right = '';
        style.width = '';
        style.overflow = '';
        if (typeof window.scrollTo === 'function') {
            try {
                window.scrollTo(0, scrollY);
            } catch {
                // jsdom and some embedders do not implement scrollTo.
            }
        }
    }

    function syncSheetViewport({ lift = true } = {}) {
        if (!sheetEl) return;
        const viewport = window.visualViewport;
        if (viewport && viewport.scale !== 1) return;
        const viewportHeight = (viewport && viewport.height) || window.innerHeight || 0;
        if (viewportHeight <= 0) return;

        sheetEl.style.maxHeight = `${Math.floor(viewportHeight * 0.8)}px`;
        if (!lift) return;

        const overlap =
            document.documentElement.clientHeight -
            (viewport ? viewport.offsetTop : 0) -
            viewportHeight;
        sheetEl.style.bottom = overlap > 0 ? `${Math.floor(overlap)}px` : '';
    }

    function clampChildrenListScroll() {
        if (!childrenListEl) return;
        const maxScrollTop = Math.max(
            0,
            childrenListEl.scrollHeight - childrenListEl.clientHeight
        );
        if (childrenListEl.scrollTop > maxScrollTop) {
            childrenListEl.scrollTop = maxScrollTop;
        }
    }

    async function openSheet() {
        if (!sheetEl || !backdropEl) return;
        returnFocus = document.activeElement;
        sheetSnapshotIds = new Set(liveOverdue.map(getChildId).filter(Boolean));
        sheetItems = [...liveOverdue];
        sheetCount = liveOverdue.length;
        sheetOpen = true;
        syncSheetViewport();
        lockBodyScroll();
        await tick();
        sheetEl.focus({ preventScroll: true });
    }

    function closeSheet() {
        if (!sheetOpen) return;
        sheetOpen = false;
        sheetSnapshotIds = new Set();
        if (sheetEl) sheetEl.style.bottom = '';
        unlockBodyScroll();
        const target = returnFocus;
        returnFocus = null;
        if (
            target &&
            target !== document.body &&
            target.isConnected &&
            typeof target.focus === 'function'
        ) {
            target.focus({ preventScroll: true });
        }
    }

    function onKeydown(event) {
        if (event.key === 'Escape' && sheetOpen) closeSheet();
    }

    async function loadPreviewData() {
        let createPreviewChildren;
        try {
            // A variable keeps Vite from trying to bundle this dev-only module.
            const previewDataUrl = '/static/dev/preview-data.js';
            ({ createPreviewChildren } = await import(
                /* @vite-ignore */ previewDataUrl
            ));
        } catch (error) {
            console.warn('Checkouts preview fixture is unavailable outside dev.', error);
            return;
        }

        previewBlocked = true;
        requestSeq += 1;
        if (fetchController) {
            fetchController.abort();
            fetchController = null;
        }
        clearFlashStyles();
        overrides = {};
        childrenData = createPreviewChildren();
        loaded = true;
        pollFailed = false;
        knownChildIds = new Set(childrenData.map(getChildId).filter(Boolean));
        if (sheetOpen) {
            sheetSnapshotIds = new Set(liveOverdue.map(getChildId).filter(Boolean));
            sheetItems = [...liveOverdue];
            sheetCount = liveOverdue.length;
        }
    }

    $effect(() => {
        const count = overdueCount;
        if (count > lastOverdueCount) {
            badgeJiggle = true;
            const timer = setTimeout(() => {
                badgeJiggle = false;
            }, 1000);
            lastOverdueCount = count;
            return () => {
                clearTimeout(timer);
                badgeJiggle = false;
            };
        }
        badgeJiggle = false;
        lastOverdueCount = count;
    });

    onMount(() => {
        syncSheetViewport({ lift: false });

        const pollTimer = setInterval(fetchChildrenData, 3000);
        const clockTimer = setInterval(() => {
            nowMs = Date.now();
            updateSheetCount();
        }, 1000);

        const onResize = () => {
            requestAnimationFrame(clampChildrenListScroll);
            requestAnimationFrame(() => {
                if (sheetOpen) syncSheetViewport();
            });
        };
        window.addEventListener('resize', onResize);

        const visualViewport = window.visualViewport;
        const onVisualViewportResize = () => {
            requestAnimationFrame(() => {
                if (sheetOpen) syncSheetViewport();
            });
        };
        visualViewport?.addEventListener('resize', onVisualViewportResize);

        document.addEventListener('keydown', onKeydown);

        if (typeof window.initKebabMenu === 'function') {
            window.initKebabMenu();
        } else if (
            window.NWKidsKebabMenu &&
            typeof window.NWKidsKebabMenu.initKebabMenu === 'function'
        ) {
            window.NWKidsKebabMenu.initKebabMenu();
        }

        window.__checkoutsPreview = { loadPreviewData };

        fetchLocationGroups();
        fetchChildrenData();

        return () => {
            clearInterval(pollTimer);
            clearInterval(clockTimer);
            window.removeEventListener('resize', onResize);
            visualViewport?.removeEventListener('resize', onVisualViewportResize);
            document.removeEventListener('keydown', onKeydown);
            if (fetchController) fetchController.abort();
            if (flashTimeoutId) clearTimeout(flashTimeoutId);
            if (window.__checkoutsPreview?.loadPreviewData === loadPreviewData) {
                delete window.__checkoutsPreview;
            }
        };
    });
</script>

<!-- Background images -->
<div class="fixed inset-0 z-0 overflow-hidden pointer-events-none">
    <img
        src="/static/img/bg-left.svg"
        alt=""
        class="absolute left-0 bottom-0 h-[300px] w-auto m:h-auto object-cover object-bottom lg:h-auto lg:w-[100%] z-10"
    />
    <img
        src="/static/img/bg-right.svg"
        alt=""
        class="absolute right-0 bottom-0 h-[200px] w-auto m:h-auto object-cover object-bottom lg:h-auto lg:w-[40%] z-0"
    />
</div>

<header class="relative flex justify-between items-center w-full z-20 px-6 pt-4 lg:fixed">
    <div class="flex justify-between items-center">
        <a href="/">
            <picture>
                <source type="image/avif" srcset="/static/img/NWKids-logo-320.avif" />
                <source type="image/webp" srcset="/static/img/NWKids-logo-320.webp" />
                <img
                    src="/static/img/NWKids-logo-320.png"
                    alt="NWKids Logo"
                    class="h-16 md:h-24 w-auto"
                />
            </picture>
        </a>
    </div>

    <div
        class="absolute left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 text-4xl md:text-6xl font-bold text-gray-800"
    >
        <span id="current-time">{formatClock(nowMs)}</span>
    </div>

    <div class="hidden lg:block w-16 md:w-24"></div>

    <div class="absolute top-6 right-6 flex items-center gap-2">
        <button
            id="search-toggle-button"
            type="button"
            aria-expanded={searchOpen}
            aria-controls="search-controls"
            aria-label="Toggle search"
            class="cursor-pointer rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-700 shadow-sm hover:bg-slate-50"
            onclick={toggleSearch}
        >
            <svg
                xmlns="http://www.w3.org/2000/svg"
                fill="none"
                viewBox="0 0 24 24"
                stroke-width="2"
                stroke="currentColor"
                class="h-5 w-5 transition-transform duration-300"
                class:rotate-180={searchOpen}
                data-search-toggle-icon
            >
                <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    d="M9.594 3.94c.09-.542.56-.94 1.11-.94h2.593c.55 0 1.02.398 1.11.94l.213 1.281c.063.374.313.686.645.87.074.04.147.083.22.127.325.196.72.257 1.075.124l1.217-.456a1.125 1.125 0 0 1 1.37.49l1.296 2.247a1.125 1.125 0 0 1-.26 1.431l-1.003.827c-.293.241-.438.613-.43.992a7.723 7.723 0 0 1 0 .255c-.008.378.137.75.43.991l1.004.827c.424.35.534.955.26 1.43l-1.298 2.247a1.125 1.125 0 0 1-1.369.491l-1.217-.456c-.355-.133-.75-.072-1.076.124a6.47 6.47 0 0 1-.22.128c-.331.183-.581.495-.644.869l-.213 1.281c-.09.543-.56.94-1.11.94h-2.594c-.55 0-1.019-.398-1.111-.94l-.213-1.281c-.062-.374-.312-.686-.644-.87a6.52 6.52 0 0 1-.22-.127c-.325-.196-.72-.257-1.076-.124l-1.217.456a1.125 1.125 0 0 1-1.369-.49l-1.297-2.247a1.125 1.125 0 0 1 .26-1.431l1.004-.827c.292-.24.437-.613.43-.991a6.932 6.932 0 0 1 0-.255c.007-.38-.138-.751-.43-.992l-1.004-.827a1.125 1.125 0 0 1-.26-1.43l1.297-2.247a1.125 1.125 0 0 1 1.37-.491l1.216.456c.356.133.751.072 1.076-.124.072-.044.146-.086.22-.128.332-.183.582-.495.644-.869l.214-1.28Z"
                />
                <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    d="M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z"
                />
            </svg>
        </button>

        <div class="relative z-50">
            <button
                id="menu-button"
                type="button"
                aria-haspopup="true"
                aria-expanded="false"
                aria-controls="kebab-menu"
                class="inline-flex h-11 w-11 cursor-pointer items-center justify-center rounded-full border border-slate-200 bg-white text-slate-700 shadow-sm transition hover:border-slate-300 hover:text-slate-900"
            >
                <span class="sr-only">Open menu</span>
                <svg aria-hidden="true" class="h-5 w-5" viewBox="0 0 20 20" fill="currentColor">
                    <rect x="3" y="4" width="14" height="2" rx="1"></rect>
                    <rect x="3" y="9" width="14" height="2" rx="1"></rect>
                    <rect x="3" y="14" width="14" height="2" rx="1"></rect>
                </svg>
            </button>
            <div
                id="kebab-menu"
                class="absolute right-0 mt-2 hidden w-56 rounded-md border border-slate-200 bg-white py-2 text-sm text-slate-700 shadow-lg z-50"
            >
                {@html menuHtml}
            </div>
        </div>
    </div>
</header>

<!-- Main Content -->
<div class="relative z-10 min-h-screen flex flex-col box-border lg:h-screen lg:pt-28 pb-20">
    <div
        class="relative z-10 flex flex-col items-center gap-2 px-4 md:flex-row md:justify-center md:gap-3"
    >
        <div
            id="search-controls"
            class="w-full max-w-md md:flex-1 md:max-w-2xl"
            use:searchPanel={searchOpen}
        >
            <div class="overflow-hidden search-controls-panel pb-2 md:pt-4">
                <div
                    class="flex flex-col items-center gap-2 md:flex-row md:items-center md:gap-3"
                >
                    <input
                        id="search-input"
                        type="search"
                        autocomplete="off"
                        placeholder="Search by name or code"
                        class="w-full rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-base shadow-sm focus:border-slate-500 focus:outline-none md:flex-1 md:min-w-0"
                        value={searchQuery}
                        oninput={(event) => (searchQuery = event.currentTarget.value)}
                    />
                    <label
                        class="inline-flex cursor-pointer select-none items-center gap-2 text-sm font-medium text-slate-700"
                    >
                        Hide confirmed
                        <span class="relative inline-flex h-5 w-9 shrink-0 items-center">
                            <input
                                id="hide-confirmed-toggle"
                                type="checkbox"
                                role="switch"
                                aria-label="Hide confirmed children"
                                class="peer absolute h-0 w-0 opacity-0"
                                checked={hideConfirmed}
                                onchange={(event) =>
                                    (hideConfirmed = event.currentTarget.checked)}
                            />
                            <span
                                class="pointer-events-none absolute inset-0 rounded-full bg-slate-200 transition-colors duration-200 peer-checked:bg-emerald-500"
                            ></span>
                            <span
                                class="pointer-events-none absolute left-0.5 top-0.5 h-4 w-4 rounded-full bg-white shadow transition-transform duration-200 peer-checked:translate-x-4"
                            ></span>
                        </span>
                    </label>
                </div>
                <div id="location-group-filter" class="mt-3 border-t border-slate-200 pt-3">
                    <div class="flex items-center justify-between mb-2">
                        <span class="text-sm font-medium text-slate-700">Location groups</span>
                        <button
                            id="location-group-select-all"
                            type="button"
                            class="cursor-pointer text-xs font-semibold text-blue-600 hover:text-blue-700"
                            onclick={onSelectAll}
                        >
                            {selectAllLabel}
                        </button>
                    </div>
                    <div id="location-group-checkboxes" class="flex flex-wrap gap-2">
                        {#if groupsLoaded}
                            {#each locationGroups as group (group.id)}
                                <label
                                    class="inline-flex items-center gap-1.5 text-sm cursor-pointer"
                                >
                                    <input
                                        type="checkbox"
                                        data-lg-id={String(group.id)}
                                        class="h-4 w-4 rounded border-slate-300 cursor-pointer"
                                        checked={isGroupChecked(group.id)}
                                        onchange={(event) =>
                                            onGroupToggle(
                                                group.id,
                                                event.currentTarget.checked
                                            )}
                                    />
                                    <span
                                        style={`background-color:${getLocationGroupColor(group.id)}`}
                                        class="inline-block h-3 w-3 rounded-sm border border-black/10 shrink-0"
                                    ></span>
                                    {group.name}
                                </label>
                            {/each}
                            <label class="inline-flex items-center gap-1.5 text-sm cursor-pointer">
                                <input
                                    type="checkbox"
                                    data-lg-id="unassigned"
                                    class="h-4 w-4 rounded border-slate-300 cursor-pointer"
                                    checked={isGroupChecked('unassigned')}
                                    onchange={(event) =>
                                        onGroupToggle('unassigned', event.currentTarget.checked)}
                                />
                                <span
                                    style={`background-color:${getLocationGroupColor(null)}`}
                                    class="inline-block h-3 w-3 rounded-sm border border-black/10 shrink-0"
                                ></span>
                                Unassigned
                            </label>
                        {/if}
                    </div>
                </div>
            </div>
        </div>
    </div>

    <!-- Scrollable children list (list on mobile, grid on desktop) -->
    <div
        id="children-list"
        bind:this={childrenListEl}
        class="grid grid-cols-1 gap-3 px-4 py-4 md:grid-cols-2 lg:flex-1 lg:min-h-0 lg:overflow-y-auto lg:content-start lg:grid-cols-3"
    >
        {#if pollFailed}
            <div class="text-center text-red-500 py-8">
                Error loading data. Please try again.
            </div>
        {:else if !loaded}
            <div class="text-center text-gray-500 py-8">Loading children data...</div>
        {:else if visibleChildren.length === 0}
            <div class="text-center py-12 text-3xl text-slate-500">{emptyMessage}</div>
        {:else}
            {#each visibleChildren as child, index (getChildId(child) || `anonymous-${index}`)}
                <ChildCard
                    {child}
                    {nowMs}
                    confirmed={isChildConfirmed(child)}
                    flash={Boolean(flashIds[getChildId(child)])}
                    onRequestConfirm={requestConfirm}
                />
            {/each}
        {/if}
    </div>
</div>

<!-- Overdue floating badge (phone-friendly, zero cost when no overdue) -->
<button
    id="overdue-badge"
    type="button"
    class="fixed bottom-4 left-1/2 -translate-x-1/2 z-30 rounded-full bg-red-600 text-white px-5 py-2.5 text-sm font-bold shadow-[0_4px_12px_rgba(0,0,0,0.3)] hover:bg-red-700 active:bg-red-700 transition-colors cursor-pointer whitespace-nowrap w-max"
    class:hidden={overdueCount === 0}
    class:overdue-badge-jiggle={badgeJiggle}
    aria-label={overdueCount > 0
        ? `${overdueCount} overdue checkouts, tap to view`
        : 'No overdue checkouts'}
    onclick={openSheet}
>
    {overdueCount > 0 ? `${overdueCount} overdue. Tap to view` : 'overdue'}
</button>

<!-- Overdue bottom sheet -->
<div
    bind:this={backdropEl}
    id="overdue-sheet-backdrop"
    class="fixed inset-0 bg-black/40 z-40 cursor-pointer touch-manipulation"
    class:hidden={!sheetOpen}
    aria-hidden="true"
    onclick={closeSheet}
></div>
<div
    bind:this={sheetEl}
    id="overdue-sheet"
    class="fixed inset-x-0 bottom-0 z-40 bg-slate-50 rounded-t-2xl shadow-2xl border-t border-slate-200 flex flex-col"
    class:translate-y-full={!sheetOpen}
    aria-hidden={sheetOpen ? 'false' : 'true'}
    aria-modal={sheetOpen ? 'true' : undefined}
    role="dialog"
    aria-label="Overdue checkouts"
    tabindex="-1"
    inert={sheetOpen ? undefined : true}
>
    <div class="flex items-center justify-between gap-2 px-4 pt-3 pb-2 shrink-0">
        <div class="flex items-center gap-2 min-w-0">
            <span class="h-2 w-2 rounded-full bg-red-500 shrink-0"></span>
            <h2 id="overdue-sheet-title" class="text-sm font-bold text-slate-800 truncate">
                Overdue &gt;5 min
            </h2>
            <span
                id="overdue-sheet-count"
                class="text-xs font-semibold bg-red-100 text-red-700 px-2 py-0.5 rounded-full shrink-0"
            >
                {sheetCount > 0 ? `${sheetCount} overdue` : 'No overdue'}
            </span>
        </div>
        <button
            id="overdue-sheet-close"
            type="button"
            aria-label="Close overdue list"
            class="h-8 w-8 shrink-0 inline-flex items-center justify-center rounded-full text-slate-500 hover:bg-slate-200 hover:text-slate-700 cursor-pointer touch-manipulation"
            onclick={closeSheet}
        >
            <svg
                xmlns="http://www.w3.org/2000/svg"
                viewBox="0 0 20 20"
                fill="currentColor"
                class="h-5 w-5"
            >
                <path
                    d="M6.28 5.22a.75.75 0 0 0-1.06 1.06L8.94 10l-3.72 3.72a.75.75 0 1 0 1.06 1.06L10 11.06l3.72 3.72a.75.75 0 1 0 1.06-1.06L11.06 10l3.72-3.72a.75.75 0 0 0-1.06-1.06L10 8.94 6.28 5.22Z"
                />
            </svg>
        </button>
    </div>
    <div class="px-2 pb-2">
        <div class="h-1 w-10 mx-auto rounded-full bg-slate-300"></div>
    </div>
    <div id="overdue-sheet-list" class="flex-1 overflow-y-auto px-4 pb-4 space-y-3">
        {#if sheetItems.length === 0}
            <div class="text-center py-6 text-sm text-slate-500">No overdue checkouts</div>
        {:else}
            {#each sheetItems as child, index (getChildId(child) || `anonymous-${index}`)}
                <OverdueCard
                    {child}
                    {nowMs}
                    {locationGroups}
                    confirmed={isChildConfirmed(child)}
                    onRequestConfirm={requestConfirm}
                />
            {/each}
        {/if}
    </div>
</div>
