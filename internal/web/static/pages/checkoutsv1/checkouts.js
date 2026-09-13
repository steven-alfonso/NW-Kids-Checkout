// NW Kids checkouts board.
//
// Single Alpine.js component (`checkoutsBoard`, see checkouts.html) owns all
// board state: children, search/filter UI, confirmations, flash highlights,
// overdue badge + sheet, and the clock. Rendering is declarative (x-for with
// :key keeps DOM nodes — and checkbox focus/scroll — stable across polls),
// so there is no innerHTML render pass and no morphing library.
//
// Pure helpers below are framework-free and unit-tested directly.

const API_URL = '';
const DEBUG = typeof window !== 'undefined' && new URLSearchParams(window.location.search).has('debug');

const CONFIRM_OVERRIDE_TTL_MS = 15000;
const FLASH_RESET_DELAY_MS = 4000;
const OVERDUE_MINUTES = 5;

const GRAY_UNASSIGNED = '#9CA3AF';
const PAUL_TOL_MUTED = [
    '#332288',
    '#117733',
    '#CC6677',
    '#44AA99',
    '#882255',
    '#88CCEE',
    '#DDCC77',
    '#AA4499',
];

function getLocationGroupColor(locationGroupId) {
    if (locationGroupId == null) return GRAY_UNASSIGNED;
    const id = Number(locationGroupId);
    if (!Number.isFinite(id)) return GRAY_UNASSIGNED;
    const idx = Math.abs(id - 1) % PAUL_TOL_MUTED.length;
    return PAUL_TOL_MUTED[idx];
}

function getChildId(child) {
    if (!child) return '';
    if (child.source === 'manual') return child.public_id ? `manual:${child.public_id}` : '';
    if (child.source === 'planning_center') return child.planning_center_id ? `pc:${child.planning_center_id}` : '';
    if (child.planning_center_id) return `pc:${child.planning_center_id}`;
    if (child.public_id) return `public:${child.public_id}`;
    return '';
}

function normalizeCheckoutsResponse(data) {
    if (Array.isArray(data)) return data;

    const normalizeList = (value) => {
        if (Array.isArray(value)) return value;
        if (Array.isArray(value?.checkins)) return value.checkins;
        return [];
    };

    const checkins = normalizeList(data?.checkins);
    const manualCheckins = normalizeList(data?.manual_checkins);
    return [...checkins, ...manualCheckins];
}

function getCheckedOutTimestamp(value) {
    if (!value) return 0;
    const parsed = Date.parse(value);
    if (Number.isNaN(parsed)) return 0;
    return parsed;
}

function calculateMinutesAgoFromTimestamp(checkedOutAtMs, nowMs) {
    if (!checkedOutAtMs) return '0 min ago';

    const now = typeof nowMs === 'number' ? nowMs : Date.now();
    const diffInMinutes = Math.max(0, Math.floor((now - checkedOutAtMs) / (1000 * 60)));

    return `${diffInMinutes} min ago`;
}

function getTimePillClass(checkedOutAtMs, confirmed, nowMs) {
    if (confirmed) return 'bg-gray-400';
    if (!checkedOutAtMs) return 'bg-green-500';

    const now = typeof nowMs === 'number' ? nowMs : Date.now();
    const diffInMinutes = Math.max(0, (now - checkedOutAtMs) / (1000 * 60));

    if (diffInMinutes >= 8) {
        return 'bg-red-500';
    }
    if (diffInMinutes >= 4) {
        return 'bg-yellow-500';
    }
    return 'bg-green-500';
}

// Reads the location-group filter from the URL. locationGroups resolves
// location_group_name values to ids. Mirrors the previous behavior:
// - explicit empty filter (location_group_id= with nothing else) → isEmpty
// - no filter params at all → no filtering
// - otherwise filter by resolved ids / unassigned flag
function getSelectedFromURL(locationGroups) {
    const params = new URLSearchParams(window.location.search);
    const ids = new Set();
    const rawValues = params.getAll('location_group_id');
    rawValues.forEach((v) => {
        v.split(',').forEach((part) => {
            const trimmed = part.trim();
            if (!trimmed) return;
            const n = Number(trimmed);
            if (Number.isFinite(n) && n > 0) ids.add(n);
        });
    });
    const inc = params.get('include_unassigned');
    const includeUnassigned = inc === '1' || inc === 'true';
    const nameValues = params.getAll('location_group_name');
    const names = new Set();
    nameValues.forEach((v) => {
        v.split(',').forEach((part) => {
            const trimmed = part.trim();
            if (trimmed) names.add(trimmed);
        });
    });
    const hasLocationGroupParam = params.has('location_group_id') || params.has('location_group_name') || params.has('include_unassigned');
    const isEmpty = hasLocationGroupParam && ids.size === 0 && names.size === 0 && !includeUnassigned;
    if (names.size > 0 && (locationGroups || []).length > 0) {
        locationGroups.forEach((g) => {
            const gid = Number(g.id);
            if (names.has(g.name) && Number.isFinite(gid) && gid > 0) ids.add(gid);
        });
    }
    return { ids, includeUnassigned, names, isEmpty };
}

// Replaces the URL query with the given selection (no reload).
function writeURLFromSelection(idsSet, includeUnassigned) {
    const params = new URLSearchParams(window.location.search);
    params.delete('location_group_id');
    params.delete('location_group_name');
    params.delete('include_unassigned');
    if (idsSet && idsSet.size) {
        idsSet.forEach((id) => params.append('location_group_id', String(id)));
    }
    if (includeUnassigned) {
        params.append('include_unassigned', '1');
    }
    const newSearch = params.toString();
    history.replaceState(null, '', newSearch ? '?' + newSearch : window.location.pathname);
}

// Attaches derived fields used by rendering: stable _id and numeric timestamp.
function withChildMeta(child) {
    return {
        ...child,
        _id: getChildId(child),
        checked_out_at_ms: getCheckedOutTimestamp(child.checked_out_at)
    };
}

// Total ordering for checkouts: recency first, stable _id as tiebreaker.
// Ties are common (second-precision timestamps), and without the tiebreaker
// the order follows whatever row order the API happened to return — so a
// poll arriving after a confirm (which writes to the DB) can visibly swap
// tied rows. Sorting is applied everywhere lists are ordered.
function compareById(a, b) {
    if (a._id === b._id) return 0;
    return a._id < b._id ? -1 : 1;
}

function sortByCheckoutDesc(list) {
    return list.sort((a, b) => (b.checked_out_at_ms - a.checked_out_at_ms) || compareById(a, b));
}

function sortByCheckoutAsc(list) {
    return list.sort((a, b) => (a.checked_out_at_ms - b.checked_out_at_ms) || compareById(a, b));
}

// Diffs fresh children against previously known ids. Returns the new known
// set plus ids that appeared since (for flash highlighting).
function computeNewChildIds(children, knownIds) {
    const current = new Set(children.map((c) => c._id).filter(Boolean));
    const appeared = new Set();
    if (knownIds && knownIds.size > 0) {
        current.forEach((id) => {
            if (!knownIds.has(id)) appeared.add(id);
        });
    }
    return { current, appeared };
}

function filterVisibleChildren(children, opts) {
    const {
        hideConfirmed = false,
        searchQuery = '',
        filterEmpty = false,
        filterActive = false,
        selectedIds = new Set(),
        includeUnassigned = false,
        confirmedById = new Map()
    } = opts || {};

    if (filterEmpty) return [];

    let list = children;
    if (hideConfirmed) {
        list = list.filter((child) => !confirmedById.get(child._id));
    }
    const q = (searchQuery || '').trim().toLowerCase();
    if (q) {
        list = list.filter((child) => {
            const name = `${child.first_name || ''} ${child.last_name || ''}`.toLowerCase();
            const code = (child.security_code || '').toLowerCase();
            return name.includes(q) || code.includes(q);
        });
    }
    if (filterActive) {
        list = list.filter((child) => {
            if (child.source === 'manual') return true;
            if (child.location_group_id == null) return includeUnassigned;
            return selectedIds.has(Number(child.location_group_id));
        });
    }
    return list;
}

function getOverdueList(children, confirmedById, nowMs) {
    const now = typeof nowMs === 'number' ? nowMs : Date.now();
    const cutoff = OVERDUE_MINUTES * 60 * 1000;
    return sortByCheckoutAsc(children.filter((child) => {
        if (!child.checked_out_at_ms) return false;
        if (confirmedById.get(child._id)) return false;
        return now - child.checked_out_at_ms >= cutoff;
    }));
}

function lockBodyScroll(scrollState) {
    if (scrollState.locked || !document.body) return scrollState;
    const next = { ...scrollState, locked: true, scrollY: window.scrollY || 0 };
    const style = document.body.style;
    style.position = 'fixed';
    style.top = `-${next.scrollY}px`;
    style.left = '0';
    style.right = '0';
    style.width = '100%';
    style.overflow = 'hidden';
    return next;
}

function unlockBodyScroll(scrollState) {
    if (!scrollState.locked || !document.body) return { locked: false, scrollY: 0 };
    const scrollY = scrollState.scrollY;
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
        } catch (e) { /* jsdom and some embedders don't implement scrollTo */ }
    }
    return { locked: false, scrollY: 0 };
}

// The board component (registered as Alpine data `checkoutsBoard`).
// All mutable board state lives here; the HTML reads it declaratively.
function checkoutsBoardData() {
    return {
        // Server state
        children: [],
        locationGroups: [],

        // Filter UI (checkboxes bind here; applyURL derives them from the URL)
        searchQuery: '',
        hideConfirmed: false,
        groups: [],
        selected: [],
        includeUnassigned: true,
        filterEmpty: false,
        filterActive: false,

        // Board UI
        loading: true,
        loadError: false,
        searchOpen: false,
        clock: '',
        nowMs: Date.now(),

        // Confirmations: server truth in children, optimistic TTL overrides here
        confirmationOverrides: new Map(),
        confirmingIds: new Set(),

        // New-arrival flash
        knownIds: new Set(),
        flashIds: new Set(),
        flashTimeoutId: null,

        // Overdue badge + sheet (badgeVisible/sheetOverdue are refreshed by
        // refreshOverdue(); labels derive from them declaratively)
        sheetOpen: false,
        overdueRetainedIds: new Set(),
        lastOverdueCount: 0,
        badgeVisible: false,
        badgeCount: 0,
        sheetOverdue: [],

        // Fetch control
        fetchController: null,
        fetchBlocked: false,
        lastFetchParams: null,
        pollFetchId: null,
        pollTickId: null,
        scrollState: { locked: false, scrollY: 0 },

        // ----- derived -----

        get confirmedById() {
            const map = new Map();
            for (const child of this.children) {
                if (!child._id) continue;
                map.set(child._id, this.isConfirmed(child));
            }
            return map;
        },

        get visibleChildren() {
            const selectedIds = new Set(
                this.selected.map(Number).filter((n) => Number.isFinite(n) && n > 0)
            );
            return filterVisibleChildren(this.children, {
                hideConfirmed: this.hideConfirmed,
                searchQuery: this.searchQuery,
                filterEmpty: this.filterEmpty,
                filterActive: this.filterActive,
                selectedIds,
                includeUnassigned: this.includeUnassigned,
                confirmedById: this.confirmedById
            }).slice(0, 100);
        },

        get emptyMessage() {
            if (this.searchQuery.trim()) return 'No matching children';
            if (this.hideConfirmed) return 'No unconfirmed children';
            return 'No children called yet';
        },

        get overdueChildren() {
            return getOverdueList(this.children, this.confirmedById, this.nowMs);
        },

        get selectAllLabel() {
            const total = this.groups.length + 1; // +1 for Unassigned
            const checked = this.selected.length + (this.includeUnassigned ? 1 : 0);
            return total > 0 && checked === total ? 'Deselect all' : 'Select all';
        },

        get badgeText() {
            return `${this.badgeCount} overdue. Tap to view`;
        },

        get badgeAria() {
            return `${this.badgeCount} overdue checkouts, tap to view`;
        },

        get sheetLabel() {
            return this.sheetOverdue.length > 0 ? `${this.sheetOverdue.length} overdue` : 'No overdue';
        },

        // ----- per-child view helpers -----

        displayName(child) {
            return `${child.first_name || ''} ${child.last_name || ''}`.trim();
        },

        displayCode(child) {
            if (child.source === 'manual') return '---';
            return child.security_code || '----';
        },

        isConfirmed(child) {
            const entry = this.confirmationOverrides.get(child._id);
            if (entry && Date.now() - entry.timestamp <= CONFIRM_OVERRIDE_TTL_MS) {
                return entry.confirmed;
            }
            return Boolean(child.checked_out_confirmed_at);
        },

        pillClass(child) {
            const checkedOutAtMs = child.checked_out_at_ms ?? getCheckedOutTimestamp(child.checked_out_at);
            return getTimePillClass(checkedOutAtMs, this.isConfirmed(child), this.nowMs);
        },

        minutesAgo(child) {
            const checkedOutAtMs = child.checked_out_at_ms ?? getCheckedOutTimestamp(child.checked_out_at);
            return calculateMinutesAgoFromTimestamp(checkedOutAtMs, this.nowMs);
        },

        groupColor(locationGroupId) {
            return getLocationGroupColor(locationGroupId);
        },

        groupLabel(locationGroupId) {
            if (locationGroupId == null) return 'Unassigned';
            const g = this.locationGroups.find((lg) => Number(lg.id) === Number(locationGroupId));
            return g ? g.name : `Group ${locationGroupId}`;
        },

        // ----- lifecycle -----

        init() {
            window.__checkoutsBoard = this;
            this.applyURL();
            this.tickClock();
            this.fetchLocationGroups();
            this.fetchChildrenData();
            this.pollFetchId = setInterval(() => { this.fetchChildrenData(); }, 3000);
            this.pollTickId = setInterval(() => { this.tick(); }, 1000);
        },

        destroy() {
            if (this.pollFetchId) clearInterval(this.pollFetchId);
            if (this.pollTickId) clearInterval(this.pollTickId);
            if (this.flashTimeoutId) clearTimeout(this.flashTimeoutId);
            this.pollFetchId = this.pollTickId = this.flashTimeoutId = null;
        },

        tick() {
            this.tickClock();
            this.sweepOverrides();
            this.refreshOverdue();
        },

        tickClock() {
            this.nowMs = Date.now();
            this.clock = new Date().toLocaleTimeString('en-US', {
                hour: '2-digit',
                minute: '2-digit',
                hour12: false
            });
        },

        sweepOverrides() {
            const now = Date.now();
            let changed = false;
            for (const [id, entry] of this.confirmationOverrides) {
                if (now - entry.timestamp > CONFIRM_OVERRIDE_TTL_MS) {
                    this.confirmationOverrides.delete(id);
                    changed = true;
                }
            }
            return changed;
        },

        clearFlash() {
            this.flashIds = new Set();
            if (this.flashTimeoutId) {
                clearTimeout(this.flashTimeoutId);
                this.flashTimeoutId = null;
            }
        },

        onResize() {
            const list = this.$refs ? this.$refs.childrenList : null;
            if (list) {
                const maxScrollTop = Math.max(0, list.scrollHeight - list.clientHeight);
                if (list.scrollTop > maxScrollTop) list.scrollTop = maxScrollTop;
            }
            const controls = this.$refs ? this.$refs.searchControls : null;
            if (controls && this.searchOpen) {
                controls.style.height = `${controls.scrollHeight}px`;
            }
        },

        toggleSearch() {
            const controls = this.$refs ? this.$refs.searchControls : null;
            const expanded = !this.searchOpen;
            this.searchOpen = expanded;
            if (!controls) return;
            controls.classList.toggle('is-expanded', expanded);
            if (expanded) {
                controls.style.height = '0px';
                void controls.offsetHeight;
                controls.style.height = `${controls.scrollHeight}px`;
                if (this.$refs.searchInput) this.$refs.searchInput.focus();
            } else {
                controls.style.height = `${controls.scrollHeight}px`;
                void controls.offsetHeight;
                controls.style.height = '0px';
            }
        },

        // ----- data -----

        async fetchChildrenData() {
            if (this.fetchBlocked) return;
            let controller = null;
            try {
                this.fetchBlocked = true;
                if (this.fetchController) this.fetchController.abort();
                controller = new AbortController();
                this.fetchController = controller;

                const params = new URLSearchParams(window.location.search);
                const outParams = new URLSearchParams();
                outParams.append('limit', params.get('limit') || '100');

                // Single unfiltered poll: location-group filtering is done
                // client-side so the overdue badge sees all kids regardless
                // of filter. Only non-group params are forwarded.
                const checkedOutAfter = params.get('checked_out_after');
                if (checkedOutAfter) outParams.append('checked_out_after', checkedOutAfter);

                const fetchSignature = window.location.search;
                const filterChanged = fetchSignature !== this.lastFetchParams;

                const response = await fetch(`${API_URL}/v1/checkins/checkouts/?${outParams.toString()}`, {
                    signal: controller.signal
                });
                if (!response.ok) {
                    throw new Error(`HTTP error! status: ${response.status}`);
                }

                const data = await response.json();
                const combined = sortByCheckoutDesc(normalizeCheckoutsResponse(data)
                    .map(withChildMeta)
                    .filter((child) => child.checked_out_at_ms)); // only kids who have been called

                this.children = combined;
                if (filterChanged) {
                    // Filter changed: treat this response as the new baseline
                    // rather than arrivals, so existing children don't flash.
                    this.knownIds = new Set(combined.map((c) => c._id).filter(Boolean));
                    this.clearFlash();
                    this.lastFetchParams = fetchSignature;
                } else {
                    const { current, appeared } = computeNewChildIds(combined, this.knownIds);
                    this.knownIds = current;
                    if (appeared.size > 0) {
                        this.flashIds = appeared;
                        if (this.flashTimeoutId) clearTimeout(this.flashTimeoutId);
                        this.flashTimeoutId = setTimeout(() => this.clearFlash(), FLASH_RESET_DELAY_MS);
                    }
                }

                // Server caught up with an optimistic override: drop it.
                const serverById = new Map(combined.map((c) => [c._id, Boolean(c.checked_out_confirmed_at)]));
                for (const [id, entry] of this.confirmationOverrides) {
                    if (serverById.get(id) === entry.confirmed) {
                        this.confirmationOverrides.delete(id);
                    }
                }

                this.loadError = false;
                this.refreshOverdue();
                this.tickClock(); // initialize times

                if (DEBUG) {
                    console.log(`Fetched ${combined.length} children`);
                }
            } catch (error) {
                if (error?.name === 'AbortError') return;
                console.error('Error fetching children data:', error);
                this.children = [];
                this.loadError = true;
            } finally {
                this.loading = false;
                this.fetchBlocked = false;
                if (this.fetchController === controller) {
                    this.fetchController = null;
                }
            }
        },

        async fetchLocationGroups() {
            try {
                const response = await fetch(`${API_URL}/v1/location_groups`, { credentials: 'same-origin' });
                if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
                const data = await response.json();
                let groups = [];
                if (Array.isArray(data)) groups = data;
                else if (Array.isArray(data.location_groups)) groups = data.location_groups;
                this.setGroups(groups);
            } catch (error) {
                console.error('Error fetching location groups:', error);
            }
        },

        // ----- location-group filter -----

        setGroups(groups) {
            this.locationGroups = groups || [];
            this.groups = this.locationGroups.map((g) => ({
                key: String(g.id),
                name: g.name || '',
                color: getLocationGroupColor(g.id)
            }));
            this.applyURL();
        },

        applyURL() {
            const { ids, includeUnassigned, isEmpty } = getSelectedFromURL(this.locationGroups);
            // Mirror the legacy semantics: an explicit empty filter shows
            // nothing; no params means no filtering; otherwise filter.
            this.filterEmpty = isEmpty;
            this.filterActive = !isEmpty && (ids.size > 0 || includeUnassigned);
            if (isEmpty) {
                this.selected = [];
                this.includeUnassigned = false;
            } else if (!this.filterActive) {
                this.selected = this.groups.map((g) => g.key);
                this.includeUnassigned = true;
            } else {
                this.selected = this.groups
                    .filter((g) => ids.has(Number(g.key)))
                    .map((g) => g.key);
                this.includeUnassigned = includeUnassigned;
            }
        },

        selectedIds() {
            const ids = new Set();
            this.selected.forEach((key) => {
                const n = Number(key);
                if (Number.isFinite(n) && n > 0) ids.add(n);
            });
            return ids;
        },

        onFilterChange() {
            const ids = this.selectedIds();
            if (ids.size === 0 && !this.includeUnassigned) {
                // Explicit empty filter shows no children.
                const params = new URLSearchParams(window.location.search);
                params.delete('location_group_id');
                params.delete('location_group_name');
                params.delete('include_unassigned');
                params.append('location_group_id', '');
                const newSearch = params.toString();
                history.replaceState(null, '', newSearch ? '?' + newSearch : window.location.pathname);
                this.applyURL();
                this.fetchChildrenData();
                return;
            }
            writeURLFromSelection(ids, this.includeUnassigned);
            this.applyURL();
            this.fetchChildrenData();
        },

        toggleSelectAll() {
            const deselect = this.selectAllLabel === 'Deselect all';
            const params = new URLSearchParams(window.location.search);
            params.delete('location_group_id');
            params.delete('location_group_name');
            params.delete('include_unassigned');
            if (deselect) {
                params.append('location_group_id', '');
            }
            const newSearch = params.toString();
            history.replaceState(null, '', newSearch ? '?' + newSearch : window.location.pathname);
            this.applyURL();
            this.fetchChildrenData();
        },

        // ----- confirmations -----

        confirmEndpoint(child) {
            if (child.source === 'manual') {
                if (!child.public_id) {
                    console.error('Missing public_id for manual confirmation');
                    return null;
                }
                return `${API_URL}/v1/checkins/manual-checkins/${encodeURIComponent(child.public_id)}/checked_out_confirmed`;
            }
            if (child.source && child.source !== 'planning_center') {
                console.warn(`Skipping confirmation for source: ${child.source}`);
                return null;
            }
            if (!child.planning_center_id) {
                console.error('Missing planning_center_id for confirmation');
                return null;
            }
            return `${API_URL}/v1/checkins/${encodeURIComponent(child.planning_center_id)}/checked_out_confirmed`;
        },

        async onConfirmToggle(child, event) {
            const id = child._id;
            const checkbox = event && event.target ? event.target : null;
            if (!id || this.confirmingIds.has(id)) {
                if (checkbox) checkbox.checked = this.isConfirmed(child);
                return;
            }
            const previous = this.isConfirmed(child);
            const next = checkbox ? checkbox.checked : !previous;

            // Retain confirmed overdue rows in the drawer until it closes.
            const wasOverdue = this.overdueChildren.some((c) => c._id === id);
            if (wasOverdue && !previous && next && this.sheetOpen) {
                this.overdueRetainedIds.add(id);
            } else if (!next) {
                this.overdueRetainedIds.delete(id);
            }

            this.confirmationOverrides.set(id, { confirmed: next, timestamp: Date.now() });

            const endpoint = this.confirmEndpoint(child);
            if (!endpoint) {
                this.confirmationOverrides.delete(id);
                if (checkbox) checkbox.checked = previous;
                return;
            }

            this.confirmingIds.add(id);
            try {
                const response = await fetch(endpoint, {
                    method: 'PATCH',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ confirmed: next })
                });
                if (!response.ok) {
                    throw new Error(`HTTP error! status: ${response.status}`);
                }
            } catch (error) {
                console.error('Error confirming checkout:', error);
                this.confirmationOverrides.delete(id);
                if (checkbox) checkbox.checked = previous;
            } finally {
                this.confirmingIds.delete(id);
            }
            this.refreshOverdue();
        },

        // ----- overdue badge + sheet -----

        refreshOverdue() {
            const live = this.overdueChildren;
            const count = live.length;
            this.badgeCount = count;

            if (count > 0) {
                if (!this.sheetOpen) {
                    this.badgeVisible = true;
                    if (count > this.lastOverdueCount) this.jiggleBadge();
                }
            } else if (this.sheetOpen) {
                // Defer hiding the badge and auto-closing the drawer while
                // the sheet is open — keep confirmed rows visible until close.
            } else {
                this.badgeVisible = false;
                this.closeOverdueSheet();
            }

            // While the drawer is open, keep confirmed rows visible until
            // close, but still allow newly-overdue children to appear.
            if (this.sheetOpen) {
                this.lastOverdueCount = count;
                const liveIds = new Set(live.map((c) => c._id));
                const retained = [];
                for (const id of this.overdueRetainedIds) {
                    if (liveIds.has(id)) {
                        // No longer needs retaining — live overdue again.
                        this.overdueRetainedIds.delete(id);
                        continue;
                    }
                    const child = this.children.find((c) => c._id === id);
                    if (child) retained.push(child);
                    else this.overdueRetainedIds.delete(id);
                }
                this.sheetOverdue = sortByCheckoutAsc([...live, ...retained]);
            } else {
                this.lastOverdueCount = count;
                this.sheetOverdue = live;
            }
        },

        jiggleBadge() {
            const badge = this.$refs ? this.$refs.overdueBadge : null;
            if (!badge) return;
            badge.classList.remove('overdue-badge-jiggle');
            void badge.offsetWidth;
            badge.classList.add('overdue-badge-jiggle');
        },

        openOverdueSheet() {
            if (this.sheetOpen) return;
            this.sheetOpen = true;
            this.overdueRetainedIds = new Set();
            // Fresh snapshot on every open so prior confirms are reflected.
            this.refreshOverdue();
        },

        closeOverdueSheet() {
            const wasOpen = this.sheetOpen;
            this.sheetOpen = false;
            this.overdueRetainedIds = new Set();
            if (wasOpen) this.refreshOverdue();
        },

        // Body scroll follows the sheet declaratively (x-effect on the page
        // calls this whenever sheetOpen changes).
        syncScrollLock() {
            this.scrollState = this.sheetOpen
                ? lockBodyScroll(this.scrollState)
                : unlockBodyScroll(this.scrollState);
        },

        // ----- dev tooling -----

        // Seeds demo children (used by dev-assets/preview.js via
        // window.__checkoutsBoard). Blocks polling so the demo is stable.
        previewChildren(raw) {
            this.fetchBlocked = true;
            // Keeps the given order (unlike live fetches, which sort by
            // recency) so curated demo lists display as authored.
            const combined = (raw || [])
                .map(withChildMeta)
                .filter((child) => child.checked_out_at_ms);
            this.children = combined;
            this.knownIds = new Set(combined.map((c) => c._id).filter(Boolean));
            this.clearFlash();
            this.loading = false;
            this.loadError = false;
            this.refreshOverdue();
            this.tickClock();
        }
    };
}

if (typeof document !== 'undefined' && document.addEventListener) {
    document.addEventListener('alpine:init', () => {
        if (window.Alpine && typeof window.Alpine.data === 'function') {
            window.Alpine.data('checkoutsBoard', checkoutsBoardData);
        }
    });
}

if (typeof window !== 'undefined') {
    window.checkoutsBoardData = checkoutsBoardData;
    window.getChildId = getChildId;
    window.normalizeCheckoutsResponse = normalizeCheckoutsResponse;
    window.getCheckedOutTimestamp = getCheckedOutTimestamp;
    window.calculateMinutesAgoFromTimestamp = calculateMinutesAgoFromTimestamp;
    window.getTimePillClass = getTimePillClass;
    window.getSelectedFromURL = getSelectedFromURL;
    window.getLocationGroupColor = getLocationGroupColor;
    window.filterVisibleChildren = filterVisibleChildren;
    window.getOverdueList = getOverdueList;
    window.withChildMeta = withChildMeta;
    window.sortByCheckoutDesc = sortByCheckoutDesc;
    window.sortByCheckoutAsc = sortByCheckoutAsc;
    window.computeNewChildIds = computeNewChildIds;
    window.GRAY_UNASSIGNED = GRAY_UNASSIGNED;
    window.PAUL_TOL_MUTED = PAUL_TOL_MUTED;
    window.OVERDUE_MINUTES = OVERDUE_MINUTES;
}

if (typeof document !== 'undefined') {
    document.addEventListener('DOMContentLoaded', function () {
        if (window.__checkoutsInitialized) return;
        window.__checkoutsInitialized = true;

        if (typeof window.initKebabMenu === 'function') {
            window.initKebabMenu();
        } else if (window.NWKidsKebabMenu && typeof window.NWKidsKebabMenu.initKebabMenu === 'function') {
            window.NWKidsKebabMenu.initKebabMenu();
        }

        // The board boots from its Alpine init(). If Alpine failed to load
        // (vendored script missing), fail loudly instead of a blank page.
        if (!window.Alpine) {
            const list = document.getElementById('children-list');
            if (list) {
                list.innerHTML = '<div class="text-center text-red-500 py-8">Checkout board failed to load. Please refresh.</div>';
            }
        }
        // NOTE: data fetching + intervals live in the component init() —
        // not here — so there is exactly one boot path.
    });
}
