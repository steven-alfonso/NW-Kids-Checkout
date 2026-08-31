// API URL
const API_URL = '';
const DEBUG = new URLSearchParams(window.location.search).has('debug');

// Store current data
let childrenData = [];
let childrenFetchController = null;
let childTimeElementsById = new Map();
let lastListSignature = null;
let searchQuery = '';
let hideConfirmed = false;
let knownChildIds = new Set();
let lastFetchParams = null;
let flashChildIds = new Set();
let flashTimeoutId = null;
const CONFIRM_OVERRIDE_TTL_MS = 15000;
const FLASH_RESET_DELAY_MS = 4000;
const confirmationOverrides = new Map();
const dom = {
    childrenList: null,
    currentTime: null
};

const API_CALL_BLOCKS = {
    fetchChildrenData: false
};

const CONFIRMED_ICON_SRC = '/static/img/confirmed-checkbox.svg';
const MANUAL_STAR_ICON_SRC = '/static/img/star.svg';

function escapeHtml(value) {
    return String(value ?? '').replace(/[&<>"']/g, (character) => ({
        '&': '&amp;',
        '<': '&lt;',
        '>': '&gt;',
        '"': '&quot;',
        "'": '&#39;'
    }[character]));
}

function getManualCheckinStarMarkup(source) {
    if (source !== 'manual') return '';
    return ` <img src="${MANUAL_STAR_ICON_SRC}" alt="Manual checkin" class="inline-block h-5 w-5 ml-2 relative -top-0.5">`;
}

function getChildId(child) {
    if (!child) return '';
    if (child.source === 'manual') return child.public_id ? `manual:${child.public_id}` : '';
    if (child.source === 'planning_center') return child.planning_center_id ? `pc:${child.planning_center_id}` : '';
    if (child.planning_center_id) return `pc:${child.planning_center_id}`;
    if (child.public_id) return `public:${child.public_id}`;
    return '';
}

function computeNewChildIds(children) {
    const currentIds = new Set(children.map(getChildId).filter(Boolean));
    const newlyAppeared = new Set();
    if (knownChildIds.size > 0) {
        currentIds.forEach((id) => {
            if (!knownChildIds.has(id)) newlyAppeared.add(id);
        });
    }
    knownChildIds = currentIds;
    return newlyAppeared;
}

function resetFlashClasses(container) {
    const flashed = container.querySelectorAll('.child-card-flash');
    if (flashed.length === 0) return;
    flashed.forEach((element) => {
        element.classList.remove('child-card-flash');
    });
    void container.offsetHeight;
}

function clearFlashStyles() {
    flashChildIds = new Set();
    if (flashTimeoutId) {
        clearTimeout(flashTimeoutId);
        flashTimeoutId = null;
    }
    const list = document.getElementById('children-list');
    if (list) {
        resetFlashClasses(list);
    }
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

function updateConfirmedIcon(checkbox) {
    const icon = checkbox.closest('label')?.querySelector('[data-confirmed-icon]');
    if (!icon) return;

    const label = checkbox.closest('[data-confirmed-label]');
    if (label) {
        label.dataset.confirmedState = checkbox.checked ? 'confirmed' : 'unconfirmed';
    }
}

function isApiCallBlocked(callName) {
    return Boolean(API_CALL_BLOCKS[callName]);
}

function setConfirmationOverride(childId, confirmed) {
    if (!childId) return;
    confirmationOverrides.set(childId, {
        confirmed: Boolean(confirmed),
        timestamp: Date.now()
    });
}

function clearConfirmationOverride(childId) {
    if (!childId) return;
    confirmationOverrides.delete(childId);
}

function getConfirmationOverride(childId) {
    if (!childId) return null;
    const entry = confirmationOverrides.get(childId);
    if (!entry) return null;
    if (Date.now() - entry.timestamp > CONFIRM_OVERRIDE_TTL_MS) {
        confirmationOverrides.delete(childId);
        return null;
    }
    return entry;
}

function getChildSignature(child) {
    if (!child) return 'empty';
    return [
        child.source || '',
        child.planning_center_id || '',
        child.public_id || '',
        child.checked_out_at || '',
        child.first_name || '',
        child.last_name || '',
        child.security_code || '',
        child.location_group_id != null ? String(child.location_group_id) : ''
    ].join('|');
}

function getVisibleChildren() {
    let children = childrenData;
    if (hideConfirmed) {
        children = children.filter((child) => !child.checked_out_confirmed_at);
    }
    if (searchQuery) {
        const q = searchQuery.toLowerCase();
        children = children.filter((child) => {
            const name = `${child.first_name || ''} ${child.last_name || ''}`.toLowerCase();
            const code = (child.security_code || '').toLowerCase();
            return name.includes(q) || code.includes(q);
        });
    }
    const { ids, includeUnassigned, names, isEmpty } = getSelectedFromURL();
    if (isEmpty) return [];
    const hasFilter = ids.size > 0 || names.size > 0 || includeUnassigned;
    if (hasFilter) {
        // Build name->id map if filtering by name (requires groups fetched, fallback to no map)
        // For client filtering, check both id and name (name via container text map not available here, so use ids only; name case handled by server, but client fallback checks ids)
        children = children.filter((child) => {
            const lgId = child.location_group_id;
            if (lgId == null) return includeUnassigned;
            const num = Number(lgId);
            if (ids.has(num)) return true;
            // if filtering by name but ids empty, we don't have mapping here; keep visible to avoid hiding due to missing map (server already filtered)
            if (names.size > 0 && ids.size === 0) return true;
            return false;
        });
    }
    return children;
}

function setSearchQuery(query) {
    searchQuery = (query || '').trim();
    updateUI();
}

function setHideConfirmed(hidden) {
    hideConfirmed = Boolean(hidden);
    const toggle = document.getElementById('hide-confirmed-toggle');
    if (toggle) {
        toggle.checked = hideConfirmed;
    }
    updateUI();
}

function syncConfirmedStates() {
    if (!childrenData.length) return;

    const confirmedById = new Map();
    childrenData.forEach((child) => {
        const childId = getChildId(child);
        if (!childId) return;
        const override = getConfirmationOverride(childId);
        const confirmed = override ? override.confirmed : Boolean(child.checked_out_confirmed_at);
        confirmedById.set(childId, confirmed);
    });

    const roots = [dom.childrenList].filter(Boolean);
    roots.forEach((root) => {
        root.querySelectorAll('.child-confirmed-checkbox[data-child-id]').forEach((checkbox) => {
            const childId = checkbox.dataset.childId;
            if (!childId || !confirmedById.has(childId)) return;
            const confirmed = confirmedById.get(childId);
            if (checkbox.checked !== confirmed) {
                checkbox.checked = confirmed;
            }
            const label = checkbox.closest('[data-confirmed-label]');
            if (label) {
                label.dataset.confirmedState = confirmed ? 'confirmed' : 'unconfirmed';
            }
        });
    });
}

function clampChildrenListScroll() {
    if (!dom.childrenList) return;
    const maxScrollTop = Math.max(0, dom.childrenList.scrollHeight - dom.childrenList.clientHeight);
    if (dom.childrenList.scrollTop > maxScrollTop) {
        dom.childrenList.scrollTop = maxScrollTop;
    }
}

async function confirmCheckedOut(source, planningCenterId, publicId, checkbox, confirmed, previousConfirmed) {
    if (checkbox.dataset.confirming === 'true') return;
    const childId = checkbox.dataset.childId || '';
    setConfirmationOverride(childId, confirmed);
    let endpoint = '';
    if (source === 'manual') {
        if (!publicId) {
            console.error('Missing public_id for manual confirmation');
            checkbox.checked = previousConfirmed;
            updateConfirmedIcon(checkbox);
            clearConfirmationOverride(childId);
            return;
        }
        endpoint = `${API_URL}/v1/checkins/manual-checkins/${encodeURIComponent(publicId)}/checked_out_confirmed`;
    } else {
        if (source && source !== 'planning_center') {
            console.warn(`Skipping confirmation for source: ${source}`);
            checkbox.checked = previousConfirmed;
            updateConfirmedIcon(checkbox);
            clearConfirmationOverride(childId);
            return;
        }
        if (!planningCenterId) {
            console.error('Missing planning_center_id for confirmation');
            checkbox.checked = previousConfirmed;
            updateConfirmedIcon(checkbox);
            clearConfirmationOverride(childId);
            return;
        }
        endpoint = `${API_URL}/v1/checkins/${encodeURIComponent(planningCenterId)}/checked_out_confirmed`;
    }
    checkbox.dataset.confirming = 'true';
    try {
        const response = await fetch(endpoint, {
            method: 'PATCH',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ confirmed })
        });

        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }

        checkbox.checked = Boolean(confirmed);
    } catch (error) {
        console.error('Error confirming checkout:', error);
        checkbox.checked = previousConfirmed;
        updateConfirmedIcon(checkbox);
        clearConfirmationOverride(childId);
    } finally {
        delete checkbox.dataset.confirming;
    }
}

const GRAY_UNASSIGNED = '#9CA3AF';
const PAUL_TOL_MUTED = ['#332288', '#117733', '#44AA99', '#88CCEE', '#DDCC77', '#CC6677', '#AA4499', '#882255'];

function getLocationGroupColor(locationGroupId) {
    if (locationGroupId == null) return GRAY_UNASSIGNED;
    const id = Number(locationGroupId);
    if (!Number.isFinite(id)) return GRAY_UNASSIGNED;
    const idx = Math.abs(id - 1) % PAUL_TOL_MUTED.length;
    return PAUL_TOL_MUTED[idx];
}

if (typeof window !== 'undefined') {
    window.GRAY_UNASSIGNED = GRAY_UNASSIGNED;
    window.PAUL_TOL_MUTED = PAUL_TOL_MUTED;
    window.getLocationGroupColor = getLocationGroupColor;
}

function getSelectedFromURL() {
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
    return { ids, includeUnassigned, names, isEmpty };
}

function pushURLFromSelection(idsSet, includeUnassigned) {
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
    const newUrl = newSearch ? '?' + newSearch : window.location.pathname;
    history.replaceState(null, '', newUrl);
    syncLocationGroupUIFromURL();
    updateUI();
    fetchChildrenData();
}

function renderLocationGroupSettings(groups) {
    const container = document.getElementById('location-group-checkboxes');
    if (!container) return;
    const parts = [];
    (groups || []).forEach((g) => {
        const color = getLocationGroupColor(g.id);
        const name = escapeHtml(g.name || '');
        parts.push(`<label class="inline-flex items-center gap-1.5 text-sm cursor-pointer"><input type="checkbox" data-lg-id="${g.id}" class="h-4 w-4 rounded border-slate-300 cursor-pointer"> <span style="background-color:${color}" class="inline-block h-3 w-3 rounded-sm border border-black/10 shrink-0"></span> ${name}</label>`);
    });
    const unassignedColor = GRAY_UNASSIGNED;
    parts.push(`<label class="inline-flex items-center gap-1.5 text-sm cursor-pointer"><input type="checkbox" data-lg-id="unassigned" class="h-4 w-4 rounded border-slate-300 cursor-pointer"> <span style="background-color:${unassignedColor}" class="inline-block h-3 w-3 rounded-sm border border-black/10 shrink-0"></span> Unassigned</label>`);
    container.innerHTML = parts.join('');
    syncLocationGroupUIFromURL();
}

function syncLocationGroupUIFromURL() {
    const container = document.getElementById('location-group-checkboxes');
    const selectAll = document.getElementById('location-group-select-all');
    if (!container) return;
    const { ids, includeUnassigned, names, isEmpty } = getSelectedFromURL();
    const hasIdFilter = ids.size > 0;
    const hasNameFilter = names.size > 0;
    const hasFilter = hasIdFilter || hasNameFilter || includeUnassigned;
    const checkboxes = container.querySelectorAll('input[type="checkbox"][data-lg-id]');
    if (isEmpty) {
        checkboxes.forEach((cb) => { cb.checked = false; });
        if (selectAll) {
            selectAll.textContent = 'Select all';
            selectAll.disabled = false;
        }
        return;
    }
    if (!hasFilter) {
        checkboxes.forEach((cb) => { cb.checked = true; });
        if (selectAll) {
            selectAll.textContent = 'Deselect all';
            selectAll.disabled = false;
        }
        return;
    }
    // If filtering by name, map names to ids via rendered labels (fallback to id check)
    const nameToId = new Map();
    checkboxes.forEach((cb) => {
        const label = cb.closest('label');
        const text = label ? label.textContent.trim() : '';
        const idVal = cb.getAttribute('data-lg-id');
        if (idVal !== 'unassigned' && text) nameToId.set(text, Number(idVal));
    });
    const effectiveIds = new Set(ids);
    if (hasNameFilter) {
        names.forEach((n) => {
            const mapped = nameToId.get(n);
            if (mapped) effectiveIds.add(mapped);
        });
        // if names didn't map (unknown), fallback to treat name filter as ids filter
        if (effectiveIds.size === 0) {
            // keep original names as ids fallback not possible, so treat as hasFilter
        }
    }
    checkboxes.forEach((cb) => {
        const val = cb.getAttribute('data-lg-id');
        if (val === 'unassigned') {
            cb.checked = includeUnassigned;
        } else {
            const num = Number(val);
            if (hasNameFilter || hasIdFilter) {
                cb.checked = effectiveIds.has(num);
            } else {
                cb.checked = false;
            }
        }
    });
    if (selectAll) {
        const allChecked = checkboxes.length > 0 && [...checkboxes].every((cb) => cb.checked);
        selectAll.textContent = allChecked ? 'Deselect all' : 'Select all';
        selectAll.disabled = false;
    }
}

async function fetchLocationGroups() {
    try {
        const response = await fetch(`${API_URL}/v1/location_groups`, { credentials: 'same-origin' });
        if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
        const data = await response.json();
        let groups = [];
        if (Array.isArray(data)) groups = data;
        else if (Array.isArray(data.location_groups)) groups = data.location_groups;
        else if (Array.isArray(data.checkins)) groups = [];
        renderLocationGroupSettings(groups);
        syncLocationGroupUIFromURL();
    } catch (error) {
        console.error('Error fetching location groups:', error);
    }
}

if (typeof window !== 'undefined') {
    window.getSelectedFromURL = getSelectedFromURL;
    window.pushURLFromSelection = pushURLFromSelection;
    window.renderLocationGroupSettings = renderLocationGroupSettings;
    window.syncLocationGroupUIFromURL = syncLocationGroupUIFromURL;
    window.fetchLocationGroups = fetchLocationGroups;
}

const PILL_BG_CLASSES = ['bg-gray-400', 'bg-green-500', 'bg-yellow-500', 'bg-red-500'];

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

function applyPillColor(pill, child, confirmed, nowMs) {
    if (!pill || !child) return;
    const checkedOutAtMs = child.checked_out_at_ms ?? getCheckedOutTimestamp(child.checked_out_at);
    const nextClass = getTimePillClass(checkedOutAtMs, confirmed, nowMs);
    if (pill.classList.contains(nextClass)) return;
    PILL_BG_CLASSES.forEach((className) => pill.classList.remove(className));
    pill.classList.add(nextClass);
}

// Function to calculate minutes ago
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

// Function to update time display for all children
function cacheChildTimeElements(container) {
    const timeElements = container.querySelectorAll('.child-time[data-child-id]');
    const nextMap = new Map();
    timeElements.forEach((element) => {
        const id = element.dataset.childId;
        if (id) nextMap.set(id, element);
    });
    childTimeElementsById = nextMap;
}

function updateTimes() {
    if (!childrenData.length) {
        return;
    }
    const nowMs = Date.now();
    childrenData.slice(0, 100).forEach((child) => {
        const id = getChildId(child);
        const element = childTimeElementsById.get(id);
        if (!element) return;
        const checkedOutAtMs = child.checked_out_at_ms ?? getCheckedOutTimestamp(child.checked_out_at);
        const nextValue = calculateMinutesAgoFromTimestamp(checkedOutAtMs, nowMs);
        if (element.textContent !== nextValue) {
            element.textContent = nextValue;
        }
        const override = getConfirmationOverride(id);
        const confirmed = override ? override.confirmed : Boolean(child.checked_out_confirmed_at);
        applyPillColor(element, child, confirmed, nowMs);
    });
}

// Function to fetch data from API
async function fetchChildrenData() {
    if (isApiCallBlocked('fetchChildrenData')) return;

    let controller = null;
    try {
        API_CALL_BLOCKS.fetchChildrenData = true;
        if (childrenFetchController) {
            childrenFetchController.abort();
        }
        controller = new AbortController();
        childrenFetchController = controller;
        let params = new URLSearchParams(window.location.search)
        let outParams = new URLSearchParams();

        const limit = params.get('limit')
        if (limit) {
            outParams.append('limit', limit);
        } else {
            outParams.append('limit', '100');
        }

        const locationGroupName = params.get('location_group_name')
        if (locationGroupName) outParams.append('location_group_name', locationGroupName);

        const locationGroupIds = params.getAll('location_group_id')
        locationGroupIds.forEach((v) => outParams.append('location_group_id', v));

        const includeUnassigned = params.get('include_unassigned')
        if (includeUnassigned) outParams.append('include_unassigned', includeUnassigned);

        const checkedOutAfter = params.get('checked_out_after')
        if (checkedOutAfter) outParams.append('checked_out_after', checkedOutAfter);

        const fetchSignature = outParams.toString();
        const filterChanged = fetchSignature !== lastFetchParams;

        const response = await fetch(`${API_URL}/v1/checkins/checkouts/?${outParams.toString()}`, {
            signal: controller.signal
        });
        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }

        const data = await response.json();
        const combined = normalizeCheckoutsResponse(data).map((child) => {
            const checkedOutAtMs = getCheckedOutTimestamp(child.checked_out_at);
            return {
                ...child,
                checked_out_at_ms: checkedOutAtMs
            };
        });

        // Sort by checked_out_at (most recent first)
        const sortedData = combined
            .filter(child => child.checked_out_at_ms) // Only include children who have been called
            .sort((a, b) => b.checked_out_at_ms - a.checked_out_at_ms);

        childrenData = sortedData;
        if (filterChanged) {
            // Filter changed: treat this response as the new baseline rather
            // than a set of arrivals, so existing children don't flash.
            knownChildIds = new Set(childrenData.map(getChildId).filter(Boolean));
            clearFlashStyles();
            lastFetchParams = fetchSignature;
        } else {
            const newIds = computeNewChildIds(childrenData);
            if (newIds.size > 0) {
                flashChildIds = newIds;
                clearTimeout(flashTimeoutId);
                flashTimeoutId = setTimeout(clearFlashStyles, FLASH_RESET_DELAY_MS);
            }
        }
        const confirmedById = new Map();
        childrenData.forEach((child) => {
            const childId = getChildId(child);
            if (!childId) return;
            confirmedById.set(childId, Boolean(child.checked_out_confirmed_at));
        });
        confirmationOverrides.forEach((value, childId) => {
            const current = confirmedById.get(childId);
            if (typeof current === 'boolean' && current === value.confirmed) {
                confirmationOverrides.delete(childId);
            }
        });
        updateUI();
        updateTimes(); // Initialize times

        if (DEBUG) {
            console.log(`Fetched ${sortedData.length} children`);
        }
    } catch (error) {
        if (error?.name === 'AbortError') return;
        console.error('Error fetching children data:', error);
        childrenData = [];
        lastListSignature = '';
        if (!dom.childrenList) {
            dom.childrenList = document.getElementById('children-list');
        }
        if (dom.childrenList) {
            dom.childrenList.innerHTML =
                '<div class="text-center text-red-500 py-8">Error loading data. Please try again.</div>';
            dom.childrenList.scrollTop = 0;
        }
    } finally {
        API_CALL_BLOCKS.fetchChildrenData = false;
        if (childrenFetchController === controller) {
            childrenFetchController = null;
        }
    }
}

// Function to update the UI with fetched data
function updateUI() {
    if (!dom.childrenList) {
        dom.childrenList = document.getElementById('children-list');
    }

    const nowMs = Date.now();
    const visibleChildren = getVisibleChildren();
    const listSignature = hideConfirmed + '||' + searchQuery + '||' + window.location.search + '||' + visibleChildren.slice(0, 100).map(getChildSignature).join('||');
    if (dom.childrenList && listSignature !== lastListSignature) {
        const previousScrollTop = dom.childrenList.scrollTop;
        resetFlashClasses(dom.childrenList);
        const markup = renderChildren(visibleChildren.slice(0, 100), nowMs, Boolean(searchQuery));
        morphChildren(dom.childrenList, markup);
        cacheChildTimeElements(dom.childrenList);
        requestAnimationFrame(() => {
            if (!dom.childrenList) return;
            const maxScrollTop = Math.max(0, dom.childrenList.scrollHeight - dom.childrenList.clientHeight);
            dom.childrenList.scrollTop = Math.min(previousScrollTop, maxScrollTop);
        });
        lastListSignature = listSignature;
    }
    syncConfirmedStates();
}

function renderChildren(children, nowMs, searchActive) {
    if (children.length === 0) {
        if (searchActive) {
            return '<div class="text-center py-12 text-3xl text-slate-500">No matching children</div>';
        }
        if (hideConfirmed) {
            return '<div class="text-center py-12 text-3xl text-slate-500">No unconfirmed children</div>';
        }
        return '<div class="text-center py-12 text-3xl text-slate-500">No children called yet</div>';
    }

    return children.map((child) => {
        const name = `${escapeHtml(child.first_name)} ${escapeHtml(child.last_name)}`;
        const code = child.source === 'manual' ? '---' : escapeHtml(child.security_code || '----');
        const confirmed = Boolean(child.checked_out_confirmed_at);
        const planningCenterId = escapeHtml(child.planning_center_id || '');
        const publicId = escapeHtml(child.public_id || '');
        const source = escapeHtml(child.source || '');
        const starMarkup = getManualCheckinStarMarkup(child.source);
        const childId = escapeHtml(getChildId(child));
        const checkedOutAtMs = child.checked_out_at_ms ?? getCheckedOutTimestamp(child.checked_out_at);
        const flashClass = flashChildIds.has(childId) ? ' child-card-flash' : '';
        const barColor = getLocationGroupColor(child.location_group_id);

        return `
            <div class="bg-white rounded-lg shadow-[0_0_10px_rgba(0,0,0,0.25)] flex${flashClass}">
                <div class="rounded-l-lg" style="background-color:${barColor}; width:6px; flex-shrink:0" aria-hidden="true"></div>
                <div class="flex-1 py-2.5 px-4 flex flex-col justify-center">
                    <div class="font-bold text-gray-800 text-2xl mb-0">
                        ${name}${starMarkup}
                    </div>
                    <div class="flex justify-between items-center">
                        <div class="text-black text-xl">
                            ${code}
                        </div>
                        <div class="flex items-center gap-3">
                            <div class="text-white transition-colors duration-1000 px-1.5 py-0 rounded-md text-base child-time ${getTimePillClass(checkedOutAtMs, confirmed, nowMs)}" data-child-id="${childId}">
                                ${calculateMinutesAgoFromTimestamp(checkedOutAtMs, nowMs)}
                            </div>
                            <label class="relative flex items-center text-xs text-gray-600 cursor-pointer leading-none" data-confirmed-label data-confirmed-state="${confirmed ? 'confirmed' : 'unconfirmed'}">
                                <input type="checkbox" class="sr-only child-confirmed-checkbox" aria-label="Mark ${name} as confirmed"
                                    data-child-id="${childId}" data-planning-center-id="${planningCenterId}" data-public-id="${publicId}" data-source="${source}" ${confirmed ? 'checked' : ''}>
                                <img src="${CONFIRMED_ICON_SRC}" alt="" class="h-10 w-10 block" data-confirmed-icon>
                            </label>
                        </div>
                    </div>
                </div>
            </div>
        `;
    }).join('');
}

function morphChildren(target, html) {
    const template = document.createElement('div');
    template.innerHTML = html;
    if (DEBUG) {
        const start = performance.now();
        morphdom(target, template, { childrenOnly: true });
        const end = performance.now();
        console.log(`[morphdom] ${target.id || target.className || target.tagName} updated in ${(end - start).toFixed(2)}ms`);
        return;
    }
    morphdom(target, template, { childrenOnly: true });
}

// Function to update current time display
function updateCurrentTime() {
    if (!dom.currentTime) {
        dom.currentTime = document.getElementById('current-time');
    }
    if (!dom.currentTime) {
        return;
    }
    const now = new Date();
    const timeString = now.toLocaleTimeString('en-US', {
        hour: '2-digit',
        minute: '2-digit',
        hour12: false
    });
    dom.currentTime.textContent = timeString;
}

// Function to update all times (current time and minutes ago)
function updateAllTimes() {
    updateCurrentTime();
    updateTimes();
}

function setupMenu() {
    const menuButton = document.getElementById('menu-button');
    const menu = document.getElementById('kebab-menu');

    if (!menuButton || !menu) return;

    function closeMenu() {
        menu.classList.add('hidden');
        menuButton.setAttribute('aria-expanded', 'false');
    }

    function toggleMenu(event) {
        event.stopPropagation();
        const isOpen = !menu.classList.contains('hidden');
        if (isOpen) {
            closeMenu();
            return;
        }
        menu.classList.remove('hidden');
        menuButton.setAttribute('aria-expanded', 'true');
    }

    menuButton.addEventListener('click', toggleMenu);
    menu.querySelectorAll('a').forEach((link) => {
        link.addEventListener('click', closeMenu);
    });
    document.addEventListener('click', (event) => {
        if (menu.classList.contains('hidden')) return;
        if (!menu.contains(event.target)) closeMenu();
    });
    document.addEventListener('keydown', (event) => {
        if (event.key === 'Escape') closeMenu();
    });
}

async function revealAdminLink() {
    try {
        const response = await fetch('/api/session', { credentials: 'same-origin' });
        const loginLink = document.getElementById('login-link');
        const logoutLink = document.getElementById('logout-link');
        const adminLink = document.getElementById('admin-link');
        if (!loginLink || !logoutLink || !adminLink) return;
        if (!response.ok) {
            loginLink.classList.remove('hidden');
            logoutLink.classList.add('hidden');
            return;
        }
        const data = await response.json();
        if (!data || !data.authenticated) {
            loginLink.classList.remove('hidden');
            logoutLink.classList.add('hidden');
            return;
        }
        loginLink.classList.add('hidden');
        logoutLink.classList.remove('hidden');
        if (data.role === 'admin') {
            adminLink.classList.remove('hidden');
        }
    } catch (error) {
        return;
    }
}

// Initialize and start periodic updates
document.addEventListener('DOMContentLoaded', function () {
    if (window.__checkoutsInitialized) return;
    window.__checkoutsInitialized = true;
    dom.childrenList = document.getElementById('children-list');
    dom.currentTime = document.getElementById('current-time');

    setupMenu();
    revealAdminLink();

    window.addEventListener('resize', () => {
        requestAnimationFrame(clampChildrenListScroll);
    });

    const searchInput = document.getElementById('search-input');
    if (searchInput) searchInput.addEventListener('input', () => setSearchQuery(searchInput.value));

    const hideConfirmedToggle = document.getElementById('hide-confirmed-toggle');
    if (hideConfirmedToggle) hideConfirmedToggle.addEventListener('change', () => setHideConfirmed(hideConfirmedToggle.checked));

    const searchToggleButton = document.getElementById('search-toggle-button');
    const searchControls = document.getElementById('search-controls');
    if (searchToggleButton && searchControls) {
        const searchToggleIcon = searchToggleButton.querySelector('[data-search-toggle-icon]');
        function animateSearchControls(expanded) {
            searchControls.classList.toggle('is-expanded', expanded);
            if (expanded) {
                searchControls.style.height = '0px';
                void searchControls.offsetHeight;
                searchControls.style.height = `${searchControls.scrollHeight}px`;
            } else {
                searchControls.style.height = `${searchControls.scrollHeight}px`;
                void searchControls.offsetHeight;
                searchControls.style.height = '0px';
            }
        }
        searchToggleButton.addEventListener('click', () => {
            const expanded = !searchControls.classList.contains('is-expanded');
            animateSearchControls(expanded);
            searchToggleButton.setAttribute('aria-expanded', String(expanded));
            searchToggleIcon?.classList.toggle('rotate-180', expanded);
            if (expanded) {
                searchInput?.focus();
            }
        });
        window.addEventListener('resize', () => {
            if (searchControls.classList.contains('is-expanded')) {
                searchControls.style.height = `${searchControls.scrollHeight}px`;
            }
        });
    }

    const locationGroupCheckboxes = document.getElementById('location-group-checkboxes');
    if (locationGroupCheckboxes) {
        locationGroupCheckboxes.addEventListener('change', function (event) {
            const target = event.target;
            if (!target.matches('input[type="checkbox"][data-lg-id]')) return;
            const ids = new Set();
            let includeUnassigned = false;
            locationGroupCheckboxes.querySelectorAll('input[type="checkbox"][data-lg-id]').forEach((cb) => {
                if (!cb.checked) return;
                const val = cb.getAttribute('data-lg-id');
                if (val === 'unassigned') includeUnassigned = true;
                else {
                    const n = Number(val);
                    if (Number.isFinite(n) && n > 0) ids.add(n);
                }
            });
            if (ids.size === 0 && !includeUnassigned) {
                const params = new URLSearchParams(window.location.search);
                params.delete('location_group_id');
                params.delete('location_group_name');
                params.delete('include_unassigned');
                params.append('location_group_id', '');
                const newSearch = params.toString();
                const newUrl = newSearch ? '?' + newSearch : window.location.pathname;
                history.replaceState(null, '', newUrl);
                syncLocationGroupUIFromURL();
                updateUI();
                fetchChildrenData();
                return;
            }
            pushURLFromSelection(ids, includeUnassigned);
        });
    }

    const locationGroupSelectAll = document.getElementById('location-group-select-all');
    if (locationGroupSelectAll) {
        locationGroupSelectAll.addEventListener('click', function () {
            const isDeselect = locationGroupSelectAll.textContent.trim() === 'Deselect all';
            const params = new URLSearchParams(window.location.search);
            params.delete('location_group_id');
            params.delete('location_group_name');
            params.delete('include_unassigned');
            if (isDeselect) {
                params.append('location_group_id', '');
            }
            const newSearch = params.toString();
            const newUrl = newSearch ? '?' + newSearch : window.location.pathname;
            history.replaceState(null, '', newUrl);
            syncLocationGroupUIFromURL();
            fetchChildrenData();
        });
    }

    document.addEventListener('change', function (event) {
        const checkbox = event.target;
        if (!checkbox.classList.contains('child-confirmed-checkbox')) return;

        const planningCenterId = checkbox.dataset.planningCenterId;
        const publicId = checkbox.dataset.publicId;
        const source = checkbox.dataset.source;
        const label = checkbox.closest('[data-confirmed-label]');
        const previousConfirmed = label?.dataset.confirmedState === 'confirmed';
        updateConfirmedIcon(checkbox);
        confirmCheckedOut(source, planningCenterId, publicId, checkbox, checkbox.checked, previousConfirmed);
        const childId = checkbox.dataset.childId;
        const child = childrenData.find((item) => getChildId(item) === childId);
        applyPillColor(childTimeElementsById.get(childId), child, checkbox.checked, Date.now());
    });

    // Initial fetch
    fetchLocationGroups();
    fetchChildrenData();

    // Fetch new data from API every 3 seconds
    setInterval(fetchChildrenData, 3000);

    updateAllTimes();
    setInterval(updateAllTimes, 1000);
});
