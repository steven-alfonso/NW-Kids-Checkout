import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  reactive,
  ref,
  watch
} from 'vue';
import {
  CONFIRM_OVERRIDE_TTL_MS,
  FLASH_RESET_DELAY_MS,
  OVERDUE_MINUTES,
  calculateMinutesAgoFromTimestamp,
  compareByCheckoutId,
  formatHeaderTime,
  getCheckedOutTimestamp,
  getChildId,
  getTimePillClass,
  normalizeCheckoutsResponse,
  sortCheckouts
} from '../lib/checkouts.js';
import {
  getSelectedFromURL,
  replaceSelectionInURL,
  replaceWithAllSelectionInURL,
  replaceWithEmptySelectionInURL
} from '../lib/urlFilters.js';

const OVERDUE_CUTOFF_MS = OVERDUE_MINUTES * 60 * 1000;

function callKebabMenuInitializer() {
  if (typeof window === 'undefined') return;
  if (typeof window.initKebabMenu === 'function') {
    window.initKebabMenu();
  } else if (
    window.NWKidsKebabMenu &&
    typeof window.NWKidsKebabMenu.initKebabMenu === 'function'
  ) {
    window.NWKidsKebabMenu.initKebabMenu();
  }
}

export function useCheckouts() {
  const children = ref([]);
  const locationGroups = ref([]);
  const groupsLoaded = ref(false);
  const nowMs = ref(Date.now());
  const rawSearch = ref('');
  const hideConfirmed = ref(false);
  const pollFailed = ref(false);
  const searchOpen = ref(false);
  const urlSearch = ref(
    typeof window === 'undefined' ? '' : window.location.search
  );

  const overrides = reactive(new Map());
  const confirming = reactive(new Set());
  const flashIds = reactive(new Set());

  const childrenList = ref(null);
  const badgeEl = ref(null);
  const sheetEl = ref(null);

  const sheetOpen = ref(false);
  const sheetRows = ref([]);
  const sheetCount = ref(0);
  const badgeCount = ref(0);
  const badgeJiggling = ref(false);

  let activeController = null;
  let previewBlocked = false;
  let knownChildIds = new Set();
  let lastFetchParams = null;
  let flashTimer = null;
  let jiggleTimer = null;
  let lastOverdueCount = 0;
  let sheetReturnFocus = null;
  let clockTimer = null;
  let pollTimer = null;
  let bodyScrollLock = null;

  const searchQuery = computed(() => rawSearch.value.trim());
  const currentTime = computed(() => formatHeaderTime(nowMs.value));
  const selection = computed(() =>
    getSelectedFromURL(urlSearch.value, locationGroups.value)
  );
  const hasSelectionFilter = computed(
    () =>
      selection.value.ids.size > 0 ||
      selection.value.names.size > 0 ||
      selection.value.includeUnassigned
  );
  const allSelected = computed(() => {
    if (!groupsLoaded.value) return false;
    const current = selection.value;
    if (current.isEmpty) return false;
    if (!hasSelectionFilter.value) return true;
    if (!current.includeUnassigned) return false;
    return (
      locationGroups.value.length > 0 &&
      locationGroups.value.every((group) =>
        current.ids.has(Number(group.id))
      )
    );
  });
  const selectAllText = computed(() =>
    allSelected.value ? 'Deselect all' : 'Select all'
  );

  const childById = computed(() => {
    const map = new Map();
    children.value.forEach((child) => {
      const id = getChildId(child);
      if (id) map.set(id, child);
    });
    return map;
  });

  function overrideFor(childId) {
    if (!childId) return null;
    const entry = overrides.get(childId);
    if (!entry) return null;
    if (nowMs.value - entry.timestamp > CONFIRM_OVERRIDE_TTL_MS) return null;
    return entry;
  }

  function isConfirmed(child) {
    const childId = getChildId(child);
    const override = overrideFor(childId);
    if (override) return override.confirmed;
    return Boolean(child.checked_out_confirmed_at);
  }

  function setOverride(childId, confirmed) {
    if (!childId) return;
    overrides.set(childId, {
      confirmed: Boolean(confirmed),
      timestamp: Date.now()
    });
  }

  function clearOverride(childId) {
    if (!childId) return;
    overrides.delete(childId);
  }

  const visibleChildren = computed(() => {
    let list = children.value;

    if (hideConfirmed.value) {
      list = list.filter((child) => !isConfirmed(child));
    }
    if (searchQuery.value) {
      const query = searchQuery.value.toLowerCase();
      list = list.filter((child) => {
        const name =
          `${child.first_name || ''} ${child.last_name || ''}`.toLowerCase();
        const code = (child.security_code || '').toLowerCase();
        return name.includes(query) || code.includes(query);
      });
    }

    const current = selection.value;
    if (current.isEmpty) return [];
    if (current.ids.size > 0 || current.includeUnassigned) {
      list = list.filter((child) => {
        if (child.source === 'manual') return true;
        const groupId = child.location_group_id;
        if (groupId == null) return current.includeUnassigned;
        return current.ids.has(Number(groupId));
      });
    }

    return list.slice(0, 100);
  });

  const overdueChildren = computed(() => {
    const now = nowMs.value;
    return children.value
      .filter((child) => {
        if (!child.checked_out_at_ms) return false;
        if (isConfirmed(child)) return false;
        return now - child.checked_out_at_ms >= OVERDUE_CUTOFF_MS;
      })
      .sort(
        (a, b) =>
          b.checked_out_at_ms - a.checked_out_at_ms ||
          compareByCheckoutId(a, b)
      );
  });

  const overdueCount = computed(() => overdueChildren.value.length);
  const badgeHidden = computed(() => badgeCount.value === 0);
  const badgeText = computed(() =>
    badgeCount.value > 0
      ? `${badgeCount.value} overdue. Tap to view`
      : 'overdue'
  );
  const badgeAriaLabel = computed(() =>
    badgeCount.value > 0
      ? `${badgeCount.value} overdue checkouts, tap to view`
      : 'No overdue checkouts'
  );
  const sheetCountLabel = computed(() =>
    sheetCount.value > 0 ? `${sheetCount.value} overdue` : 'No overdue'
  );

  const emptyMessage = computed(() => {
    if (searchQuery.value) return 'No matching children';
    if (hideConfirmed.value) return 'No unconfirmed children';
    return 'No children called yet';
  });

  function setSearchQuery(value) {
    rawSearch.value = value;
  }

  function setHideConfirmed(value) {
    hideConfirmed.value = Boolean(value);
  }

  function toggleSearch() {
    searchOpen.value = !searchOpen.value;
  }

  function rowKey(child, index) {
    const id = getChildId(child);
    return id || `row-${index}`;
  }

  function isGroupChecked(groupId) {
    const current = selection.value;
    if (current.isEmpty) return false;
    if (!hasSelectionFilter.value) return true;
    return current.ids.has(Number(groupId));
  }

  function isUnassignedChecked() {
    const current = selection.value;
    if (current.isEmpty) return false;
    if (!hasSelectionFilter.value) return true;
    return current.includeUnassigned;
  }

  function replaceURL(nextSearch) {
    history.replaceState(
      null,
      '',
      nextSearch || window.location.pathname
    );
    urlSearch.value = window.location.search;
  }

  function commitSelection(ids, includeUnassigned) {
    replaceURL(
      replaceSelectionInURL(urlSearch.value, ids, includeUnassigned)
    );
    fetchChildrenData();
  }

  function commitEmptySelection() {
    replaceURL(replaceWithEmptySelectionInURL(urlSearch.value));
    fetchChildrenData();
  }

  function commitAllSelection() {
    replaceURL(replaceWithAllSelectionInURL(urlSearch.value));
    fetchChildrenData();
  }

  function onGroupChange({ id, checked }) {
    const current = selection.value;
    let ids = new Set();
    let includeUnassigned = false;

    if (current.isEmpty) {
      ids = new Set();
      includeUnassigned = false;
    } else if (hasSelectionFilter.value) {
      ids = new Set(current.ids);
      includeUnassigned = current.includeUnassigned;
    } else {
      locationGroups.value.forEach((group) => ids.add(Number(group.id)));
      includeUnassigned = true;
    }

    if (id === 'unassigned') {
      includeUnassigned = Boolean(checked);
    } else if (checked) {
      ids.add(Number(id));
    } else {
      ids.delete(Number(id));
    }

    if (ids.size === 0 && !includeUnassigned) {
      commitEmptySelection();
    } else {
      commitSelection(ids, includeUnassigned);
    }
  }

  function onSelectAll() {
    if (allSelected.value) {
      commitEmptySelection();
    } else {
      commitAllSelection();
    }
  }

  async function fetchLocationGroups() {
    try {
      const response = await fetch('/v1/location_groups', {
        credentials: 'same-origin'
      });
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      const data = await response.json();
      let groups = [];
      if (Array.isArray(data)) groups = data;
      else if (Array.isArray(data.location_groups)) groups = data.location_groups;

      locationGroups.value = groups;
      groupsLoaded.value = true;
    } catch (error) {
      console.error('Error fetching location groups:', error);
    }
  }

  function buildPollParams() {
    const params = new URLSearchParams();
    const current = new URLSearchParams(urlSearch.value);
    params.set('limit', current.get('limit') || '100');
    const checkedOutAfter = current.get('checked_out_after');
    if (checkedOutAfter) {
      params.set('checked_out_after', checkedOutAfter);
    }
    return params;
  }

  function clearFlash() {
    flashIds.clear();
    if (flashTimer) {
      clearTimeout(flashTimer);
      flashTimer = null;
    }
  }

  function trackArrivals(sortedChildren, filterChanged, fetchSignature) {
    const currentIds = new Set(
      sortedChildren.map(getChildId).filter(Boolean)
    );

    if (filterChanged) {
      knownChildIds = currentIds;
      clearFlash();
      lastFetchParams = fetchSignature;
      return;
    }

    const arrivals = [];
    if (knownChildIds.size > 0) {
      currentIds.forEach((id) => {
        if (!knownChildIds.has(id)) arrivals.push(id);
      });
    }
    knownChildIds = currentIds;

    if (arrivals.length > 0) {
      flashIds.clear();
      arrivals.forEach((id) => flashIds.add(id));
      if (flashTimer) clearTimeout(flashTimer);
      flashTimer = setTimeout(() => {
        flashIds.clear();
        flashTimer = null;
      }, FLASH_RESET_DELAY_MS);
    }
  }

  function reconcileOverrides(sortedChildren) {
    const confirmedById = new Map();
    sortedChildren.forEach((child) => {
      const id = getChildId(child);
      if (id) confirmedById.set(id, Boolean(child.checked_out_confirmed_at));
    });
    overrides.forEach((value, id) => {
      if (confirmedById.get(id) === value.confirmed) overrides.delete(id);
    });
  }

  async function applyChildren(sortedChildren) {
    const list = childrenList.value;
    const previousScrollTop = list ? list.scrollTop : 0;

    children.value = sortedChildren;
    badgeCount.value = overdueChildren.value.length;
    refreshSheet();

    await nextTick();
    requestAnimationFrame(() => {
      const element = childrenList.value;
      if (!element) return;
      const maxScrollTop = Math.max(
        0,
        element.scrollHeight - element.clientHeight
      );
      element.scrollTop = Math.min(previousScrollTop, maxScrollTop);
    });
  }

  async function fetchChildrenData() {
    if (previewBlocked) return;

    let controller = null;
    try {
      if (activeController) activeController.abort();
      controller = new AbortController();
      activeController = controller;

      const fetchSignature = urlSearch.value;
      const filterChanged = fetchSignature !== lastFetchParams;

      const response = await fetch(
        `/v1/checkins/checkouts/?${buildPollParams().toString()}`,
        { signal: controller.signal }
      );
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }

      const data = await response.json();
      if (controller.signal.aborted) return;

      const combined = normalizeCheckoutsResponse(data).map((child) => ({
        ...child,
        checked_out_at_ms: getCheckedOutTimestamp(child.checked_out_at)
      }));
      const sorted = sortCheckouts(combined);

      pollFailed.value = false;
      trackArrivals(sorted, filterChanged, fetchSignature);
      reconcileOverrides(sorted);
      await applyChildren(sorted);
    } catch (error) {
      if (error?.name === 'AbortError') return;
      console.error('Error fetching children data:', error);
      pollFailed.value = true;
      children.value = [];
      await nextTick();
      if (childrenList.value) childrenList.value.scrollTop = 0;
    } finally {
      if (activeController === controller) {
        activeController = null;
      }
    }
  }

  function revertCheckbox(checkbox, previousConfirmed, childId) {
    checkbox.checked = Boolean(previousConfirmed);
    clearOverride(childId);
  }

  async function confirmCheckedOut(child, event) {
    const checkbox = event.target;
    const childId = getChildId(child);
    const previousConfirmed = isConfirmed(child);
    const nextConfirmed = checkbox.checked;

    if (childId && confirming.has(childId)) return;

    let endpoint = '';
    if (child.source === 'manual') {
      if (!child.public_id) {
        console.error('Missing public_id for manual confirmation');
        revertCheckbox(checkbox, previousConfirmed, childId);
        return;
      }
      endpoint = `/v1/checkins/manual-checkins/${encodeURIComponent(
        child.public_id
      )}/checked_out_confirmed`;
    } else {
      if (child.source && child.source !== 'planning_center') {
        console.warn(`Skipping confirmation for source: ${child.source}`);
        revertCheckbox(checkbox, previousConfirmed, childId);
        return;
      }
      if (!child.planning_center_id) {
        console.error('Missing planning_center_id for confirmation');
        revertCheckbox(checkbox, previousConfirmed, childId);
        return;
      }
      endpoint = `/v1/checkins/${encodeURIComponent(
        child.planning_center_id
      )}/checked_out_confirmed`;
    }

    setOverride(childId, nextConfirmed);
    badgeCount.value = overdueChildren.value.length;
    refreshSheet();
    if (childId) confirming.add(childId);

    try {
      const response = await fetch(endpoint, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ confirmed: nextConfirmed })
      });
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      // The browser already moved the checkbox, but a second click while the
      // first PATCH is in flight can leave it out of sync with the override.
      checkbox.checked = Boolean(nextConfirmed);
      refreshSheet();
    } catch (error) {
      console.error('Error confirming checkout:', error);
      checkbox.checked = Boolean(previousConfirmed);
      clearOverride(childId);
      badgeCount.value = overdueChildren.value.length;
      refreshSheet();
    } finally {
      if (childId) confirming.delete(childId);
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
    const sheet = sheetEl.value;
    if (!sheet) return;
    const viewport = window.visualViewport;
    if (viewport && viewport.scale !== 1) return;
    const viewportHeight =
      (viewport && viewport.height) || window.innerHeight || 0;
    if (viewportHeight <= 0) return;

    sheet.style.maxHeight = `${Math.floor(viewportHeight * 0.8)}px`;
    if (!lift) return;

    const overlap =
      document.documentElement.clientHeight -
      (viewport ? viewport.offsetTop : 0) -
      viewportHeight;
    sheet.style.bottom = overlap > 0 ? `${Math.floor(overlap)}px` : '';
  }

  function lockSheetOpen() {
    if (sheetOpen.value) return;
    sheetReturnFocus = document.activeElement;
    sheetRows.value = overdueChildren.value.slice();
    sheetCount.value = overdueChildren.value.length;
    sheetOpen.value = true;
    lockBodyScroll();
  }

  function closeSheet() {
    if (!sheetOpen.value) return;
    sheetOpen.value = false;
    sheetRows.value = [];
    unlockBodyScroll();
  }

  function refreshSheet() {
    if (!sheetOpen.value || pollFailed.value) return;

    const live = overdueChildren.value;
    const liveIds = new Set(live.map(getChildId));
    const byId = childById.value;
    const next = [];

    sheetRows.value.forEach((row) => {
      const id = getChildId(row);
      if (!id) {
        // A manual check-in without a public_id has no stable id. Keep the row
        // while the payload still has children; a successful empty poll drops
        // it with everything else.
        if (children.value.length > 0) next.push(row);
        return;
      }
      if (!liveIds.has(id) && !byId.has(id)) return;
      const latest = byId.get(id);
      if (latest) next.push(latest);
    });

    next.sort(
      (a, b) =>
        (b.checked_out_at_ms ?? 0) - (a.checked_out_at_ms ?? 0) ||
        compareByCheckoutId(a, b)
    );

    sheetRows.value = next;
    sheetCount.value = live.length;
  }

  function clampChildrenListScroll() {
    const list = childrenList.value;
    if (!list) return;
    const maxScrollTop = Math.max(0, list.scrollHeight - list.clientHeight);
    if (list.scrollTop > maxScrollTop) list.scrollTop = maxScrollTop;
  }

  function tick() {
    nowMs.value = Date.now();
    const expired = [];
    overrides.forEach((entry, id) => {
      if (nowMs.value - entry.timestamp > CONFIRM_OVERRIDE_TTL_MS) {
        expired.push(id);
      }
    });
    expired.forEach((id) => overrides.delete(id));
    if (!pollFailed.value) {
      badgeCount.value = overdueChildren.value.length;
    }
  }

  function onResize() {
    requestAnimationFrame(clampChildrenListScroll);
    requestAnimationFrame(() => {
      if (sheetOpen.value) syncSheetViewport();
    });
  }

  function onVisualViewportResize() {
    requestAnimationFrame(() => {
      if (sheetOpen.value) syncSheetViewport();
    });
  }

  function onKeydown(event) {
    if (event.key === 'Escape' && sheetOpen.value) closeSheet();
  }

  function loadPreviewData(previewChildren) {
    previewBlocked = true;
    clearFlash();
    const prepared = (previewChildren || []).map((child) => ({
      ...child,
      checked_out_at_ms:
        child.checked_out_at_ms ??
        getCheckedOutTimestamp(child.checked_out_at)
    }));
    children.value = sortCheckouts(prepared);
    pollFailed.value = false;
    badgeCount.value = overdueChildren.value.length;
    knownChildIds = new Set(
      children.value.map(getChildId).filter(Boolean)
    );
    lastFetchParams = urlSearch.value;
  }

  watch(
    () => badgeCount.value,
    (count) => {
      if (count > lastOverdueCount && count > 0) {
        badgeJiggling.value = false;
        nextTick(() => {
          void badgeEl.value?.offsetWidth;
          badgeJiggling.value = true;
          if (jiggleTimer) clearTimeout(jiggleTimer);
          jiggleTimer = setTimeout(() => {
            badgeJiggling.value = false;
          }, 1000);
        });
      } else if (count === 0) {
        badgeJiggling.value = false;
      }
      lastOverdueCount = count;
    }
  );

  watch(sheetOpen, async (open) => {
    await nextTick();
    const sheet = sheetEl.value;
    if (!sheet) return;

    if (open) {
      syncSheetViewport();
      sheet.removeAttribute('inert');
      sheet.focus({ preventScroll: true });
    } else {
      sheet.style.bottom = '';
      const returnFocus = sheetReturnFocus;
      sheetReturnFocus = null;
      if (
        returnFocus &&
        returnFocus !== document.body &&
        returnFocus.isConnected &&
        typeof returnFocus.focus === 'function'
      ) {
        returnFocus.focus({ preventScroll: true });
      }
      sheet.setAttribute('inert', '');
    }
  });

  onMounted(() => {
    fetchLocationGroups();
    fetchChildrenData();
    clockTimer = setInterval(tick, 1000);
    pollTimer = setInterval(fetchChildrenData, 3000);

    document.addEventListener('keydown', onKeydown);
    window.addEventListener('resize', onResize);
    if (window.visualViewport) {
      window.visualViewport.addEventListener(
        'resize',
        onVisualViewportResize
      );
    }

    callKebabMenuInitializer();
    sheetEl.value?.setAttribute('inert', '');
    nextTick(() => syncSheetViewport({ lift: false }));
    if (typeof window !== 'undefined') {
      window.__checkoutsPreview = { loadPreviewData };
    }
  });

  onBeforeUnmount(() => {
    if (clockTimer) clearInterval(clockTimer);
    if (pollTimer) clearInterval(pollTimer);
    if (activeController) activeController.abort();
    if (flashTimer) clearTimeout(flashTimer);
    if (jiggleTimer) clearTimeout(jiggleTimer);
    document.removeEventListener('keydown', onKeydown);
    window.removeEventListener('resize', onResize);
    if (window.visualViewport) {
      window.visualViewport.removeEventListener(
        'resize',
        onVisualViewportResize
      );
    }
    unlockBodyScroll();
  });

  return {
    childrenList,
    badgeEl,
    sheetEl,
    nowMs,
    currentTime,
    rawSearch,
    hideConfirmed,
    pollFailed,
    searchOpen,
    selection,
    allSelected,
    selectAllText,
    locationGroups,
    groupsLoaded,
    visibleChildren,
    overdueChildren,
    overdueCount,
    badgeHidden,
    badgeText,
    badgeAriaLabel,
    badgeJiggling,
    sheetOpen,
    sheetRows,
    sheetCountLabel,
    emptyMessage,
    setSearchQuery,
    setHideConfirmed,
    toggleSearch,
    rowKey,
    isGroupChecked,
    isUnassignedChecked,
    onGroupChange,
    onSelectAll,
    openSheet: lockSheetOpen,
    closeSheet,
    isConfirmed,
    flashIds,
    calculateMinutesAgo: calculateMinutesAgoFromTimestamp,
    getTimePillClass,
    confirmCheckedOut
  };
}
