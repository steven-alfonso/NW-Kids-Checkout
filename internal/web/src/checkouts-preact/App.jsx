import { Component, createPortal } from 'preact';
import {
  useCallback,
  useLayoutEffect,
  useMemo,
  useRef,
  useState
} from 'preact/hooks';
import { subscribeClock } from './clock.js';
import {
  CONFIRM_OVERRIDE_TTL_MS,
  FLASH_RESET_DELAY_MS,
  TIME_PILL_BASE_CLASS,
  TIME_PILL_OVERDUE_BASE_CLASS,
  buildLocationGroupSearch,
  compareByCheckoutId,
  filterVisibleChildren,
  formatHeaderTime,
  formatMinutesAgo,
  getChildId,
  getChildSignature,
  getLocationGroupColor,
  getMinutesAgoFromTimestamp,
  getOverdueChildren,
  getOverdueGroupLabel,
  getSelectAllLabel,
  getSelectedFromURL,
  getTimePillClassFromMinutes,
  isGroupSelected,
  prepareChildren,
  reconcileChildren
} from './core.js';

const CONFIRMED_ICON_SRC = '/static/img/confirmed-checkbox.svg';
const MANUAL_STAR_ICON_SRC = '/static/img/star.svg';
const API_URL = '';

const badgeBaseClass =
  'fixed bottom-4 left-1/2 -translate-x-1/2 z-30 rounded-full bg-red-600 text-white px-5 py-2.5 text-sm font-bold shadow-[0_4px_12px_rgba(0,0,0,0.3)] hover:bg-red-700 active:bg-red-700 transition-colors cursor-pointer whitespace-nowrap w-max';
const sheetBaseClass =
  'fixed inset-x-0 bottom-0 z-40 bg-slate-50 rounded-t-2xl shadow-2xl border-t border-slate-200 flex flex-col';
const searchTogglePathOne =
  'M9.594 3.94c.09-.542.56-.94 1.11-.94h2.593c.55 0 1.02.398 1.11.94l.213 1.281c.063.374.313.686.645.87.074.04.147.083.22.127.325.196.72.257 1.075.124l1.217-.456a1.125 1.125 0 0 1 1.37.49l1.296 2.247a1.125 1.125 0 0 1-.26 1.431l-1.003.827c-.293.241-.438.613-.43.992a7.723 7.723 0 0 1 0 .255c-.008.378.137.75.43.991l1.004.827c.424.35.534.955.26 1.43l-1.298 2.247a1.125 1.125 0 0 1-1.369.491l-1.217-.456c-.355-.133-.75-.072-1.076.124a6.47 6.47 0 0 1-.22.128c-.331.183-.581.495-.644.869l-.213 1.281c-.09.543-.56.94-1.11.94h-2.594c-.55 0-1.019-.398-1.111-.94l-.213-1.281c-.062-.374-.312-.686-.644-.87a6.52 6.52 0 0 1-.22-.127c-.325-.196-.72-.257-1.076-.124l-1.217.456a1.125 1.125 0 0 1-1.369-.49l-1.297-2.247a1.125 1.125 0 0 1 .26-1.431l1.004-.827c.292-.24.437-.613.43-.991a6.932 6.932 0 0 1 0-.255c.007-.38-.138-.751-.43-.992l-1.004-.827a1.125 1.125 0 0 1-.26-1.43l1.297-2.247a1.125 1.125 0 0 1 1.37-.491l1.216.456c.356.133.751.072 1.076-.124.072-.044.146-.086.22-.128.332-.183.582-.495.644-.869l.214-1.28Z';
const searchTogglePathTwo =
  'M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z';

function childLabelName(child) {
  return `${child.first_name || ''} ${child.last_name || ''}`.trim();
}

function childCode(child) {
  return child.source === 'manual'
    ? '---'
    : child.security_code || '----';
}

function stableRowKey(child, index) {
  return getChildId(child) || getChildSignature(child) || `row-${index}`;
}

function locationGroupStyle(child) {
  return { backgroundColor: getLocationGroupColor(child.location_group_id) };
}

function ConfirmControl({ child, confirmed, onChange }) {
  return (
    <label
      class="relative flex items-center text-xs text-gray-600 cursor-pointer leading-none"
      data-confirmed-label
      data-confirmed-state={confirmed ? 'confirmed' : 'unconfirmed'}
    >
      <input
        type="checkbox"
        class="sr-only child-confirmed-checkbox"
        aria-label={`Mark ${childLabelName(child)} as confirmed`}
        data-child-id={getChildId(child)}
        data-planning-center-id={child.planning_center_id || ''}
        data-public-id={child.public_id || ''}
        data-source={child.source || ''}
        checked={confirmed}
        onChange={(event) => onChange(event.currentTarget)}
      />
      <img
        src={CONFIRMED_ICON_SRC}
        alt=""
        class="h-10 w-10 block"
        data-confirmed-icon
      />
    </label>
  );
}

function TimeLabel({ baseClass, childId, checkedOutAtMs, confirmed }) {
  const [minutes, setMinutes] = useState(() =>
    getMinutesAgoFromTimestamp(checkedOutAtMs, Date.now())
  );

  useLayoutEffect(() => {
    const update = () =>
      setMinutes(getMinutesAgoFromTimestamp(checkedOutAtMs, Date.now()));
    update();
    return subscribeClock(update);
  }, [checkedOutAtMs]);

  const pillClass = getTimePillClassFromMinutes(
    checkedOutAtMs,
    confirmed,
    minutes
  );

  return (
    <div class={`${baseClass} ${pillClass}`} data-child-id={childId}>
      {formatMinutesAgo(minutes)}
    </div>
  );
}

function HeaderTime() {
  const [text, setText] = useState(() => formatHeaderTime(Date.now()));

  useLayoutEffect(() => {
    const update = () => {
      const next = formatHeaderTime(Date.now());
      setText((previous) => (previous === next ? previous : next));
    };
    update();
    return subscribeClock(update);
  }, []);

  return text;
}

class ChildRow extends Component {
  shouldComponentUpdate(nextProps) {
    const props = this.props;
    return (
      nextProps.child !== props.child ||
      nextProps.confirmed !== props.confirmed ||
      nextProps.flash !== props.flash ||
      nextProps.onConfirm !== props.onConfirm
    );
  }

  render({ child, confirmed, flash, onConfirm }) {
    const name = childLabelName(child);

    return (
      <div
        class={`bg-white rounded-lg shadow-[0_0_10px_rgba(0,0,0,0.25)] flex${flash ? ' child-card-flash' : ''}`}
      >
        <div
          class="rounded-l-lg"
          style={{ ...locationGroupStyle(child), width: '6px', flexShrink: 0 }}
          aria-hidden="true"
        ></div>
        <div class="flex-1 py-2.5 px-4 flex flex-col justify-center">
          <div class="font-bold text-gray-800 text-2xl mb-0">
            {name}
            {child.source === 'manual' && (
              <img
                src={MANUAL_STAR_ICON_SRC}
                alt="Manual checkin"
                class="inline-block h-5 w-5 ml-2 relative -top-0.5"
              />
            )}
          </div>
          <div class="flex justify-between items-center">
            <div class="text-black text-xl">{childCode(child)}</div>
            <div class="flex items-center gap-3">
              <TimeLabel
                baseClass={TIME_PILL_BASE_CLASS}
                childId={getChildId(child)}
                checkedOutAtMs={child.checked_out_at_ms}
                confirmed={confirmed}
              />
              <ConfirmControl
                child={child}
                confirmed={confirmed}
                onChange={(checkbox) =>
                  onConfirm(child, checkbox.checked, checkbox)
                }
              />
            </div>
          </div>
        </div>
      </div>
    );
  }
}

class OverdueRow extends Component {
  shouldComponentUpdate(nextProps) {
    const props = this.props;
    return (
      nextProps.child !== props.child ||
      nextProps.confirmed !== props.confirmed ||
      nextProps.groups !== props.groups ||
      nextProps.onConfirm !== props.onConfirm
    );
  }

  render({ child, confirmed, groups, onConfirm }) {
    const checkedOutAtMs = child.checked_out_at_ms;
    const name = childLabelName(child);
    const barColor = getLocationGroupColor(child.location_group_id);
    const groupLabel = getOverdueGroupLabel(child, groups);

    return (
      <div class="bg-white rounded-lg shadow flex overflow-hidden">
        <div
          style={{ backgroundColor: barColor, width: '6px', flexShrink: 0 }}
          aria-hidden="true"
        ></div>
        <div class="flex-1 py-2 px-3 flex flex-col gap-1">
          <div class="flex items-center gap-2">
            <span
              class="inline-block h-2.5 w-2.5 rounded-sm border border-black/10 shrink-0"
              style={{ backgroundColor: barColor }}
            ></span>
            <span class="text-xs font-medium text-slate-600">{groupLabel}</span>
            <TimeLabel
              baseClass={TIME_PILL_OVERDUE_BASE_CLASS}
              childId={getChildId(child)}
              checkedOutAtMs={checkedOutAtMs}
              confirmed={confirmed}
            />
          </div>
          <div class="font-bold text-gray-800 text-lg leading-tight">
            {name}
            {child.source === 'manual' && (
              <img
                src={MANUAL_STAR_ICON_SRC}
                alt="Manual checkin"
                class="inline-block h-5 w-5 ml-2 relative -top-0.5"
              />
            )}
          </div>
          <div class="flex justify-between items-center">
            <div class="text-black text-base">{childCode(child)}</div>
            <label
              class="relative flex items-center cursor-pointer leading-none"
              data-confirmed-label
              data-confirmed-state={confirmed ? 'confirmed' : 'unconfirmed'}
            >
              <input
                type="checkbox"
                class="sr-only child-confirmed-checkbox"
                aria-label={`Mark ${name} as confirmed`}
                data-child-id={getChildId(child)}
                data-planning-center-id={child.planning_center_id || ''}
                data-public-id={child.public_id || ''}
                data-source={child.source || ''}
                checked={confirmed}
                onChange={(event) =>
                  onConfirm(
                    child,
                    event.currentTarget.checked,
                    event.currentTarget
                  )
                }
              />
              <img
                src={CONFIRMED_ICON_SRC}
                alt=""
                class="h-9 w-9 block"
                data-confirmed-icon
              />
            </label>
          </div>
        </div>
      </div>
    );
  }
}

export function App() {
  const [children, setChildren] = useState([]);
  const [groups, setGroups] = useState([]);
  const [groupsLoaded, setGroupsLoaded] = useState(false);
  const [selection, setSelection] = useState(() =>
    getSelectedFromURL(window.location.search, [])
  );
  const [searchText, setSearchText] = useState('');
  const [hideConfirmed, setHideConfirmed] = useState(false);
  const [searchExpanded, setSearchExpanded] = useState(false);
  const [loading, setLoading] = useState(true);
  const [listError, setListError] = useState(false);
  const [pollFailed, setPollFailed] = useState(false);
  const [overrides, setOverrides] = useState(() => new Map());
  const [flashIds, setFlashIds] = useState(() => new Set());
  const [overdueOpen, setOverdueOpen] = useState(false);
  const [drawerRows, setDrawerRows] = useState([]);
  const [drawerCount, setDrawerCount] = useState(0);
  const [overdueEpoch, setOverdueEpoch] = useState(0);

  const listRef = useRef(null);
  const searchControlsRef = useRef(null);
  const searchInputRef = useRef(null);
  const groupContainerRef = useRef(null);
  const sheetRef = useRef(null);
  const badgeRef = useRef(null);
  const openerRef = useRef(null);
  const activeFetch = useRef(null);
  const knownChildIds = useRef(new Set());
  const lastFetchParams = useRef(null);
  const flashTimeout = useRef(null);
  const previewBlocked = useRef(false);
  const inFlight = useRef(new Set());
  const overridesRef = useRef(overrides);
  const fetchChildrenRef = useRef(() => {});
  const expandedRef = useRef(searchExpanded);
  const overdueOpenRef = useRef(overdueOpen);
  const lastBadgeCount = useRef(0);

  overridesRef.current = overrides;
  expandedRef.current = searchExpanded;
  overdueOpenRef.current = overdueOpen;

  const isConfirmed = useCallback((child) => {
    const childId = getChildId(child);
    if (!childId) return Boolean(child.checked_out_confirmed_at);
    const override = overridesRef.current.get(childId);
    if (
      override &&
      Date.now() - override.timestamp <= CONFIRM_OVERRIDE_TTL_MS
    ) {
      return override.confirmed;
    }
    return Boolean(child.checked_out_confirmed_at);
  }, []);

  const visibleChildren = useMemo(
    () =>
      filterVisibleChildren(children, {
        searchQuery: searchText,
        hideConfirmed,
        selection
      }),
    [children, searchText, hideConfirmed, selection]
  );

  const displayChildren = visibleChildren.slice(0, 100);
  const liveOverdue = useMemo(
    () => getOverdueChildren(children, isConfirmed, Date.now()),
    [children, overrides, overdueEpoch, isConfirmed]
  );
  const overdueCount = liveOverdue.length;
  const overdueKeyRef = useRef('');

  useLayoutEffect(() => {
    overdueKeyRef.current = liveOverdue.map(getChildId).join('|');
    const update = () => {
      const rows = getOverdueChildren(children, isConfirmed, Date.now());
      const key = rows.map(getChildId).join('|');
      if (key !== overdueKeyRef.current) {
        overdueKeyRef.current = key;
        setOverdueEpoch((value) => value + 1);
      }
    };
    return subscribeClock(update);
  }, [children, overrides, isConfirmed]);

  const selectAllLabel = getSelectAllLabel(selection, groups, groupsLoaded);

  function clearFlashStyles() {
    if (flashTimeout.current) {
      clearTimeout(flashTimeout.current);
      flashTimeout.current = null;
    }
    setFlashIds((previous) => (previous.size === 0 ? previous : new Set()));
  }

  function computeNewChildIds(nextChildren) {
    const currentIds = new Set(nextChildren.map(getChildId).filter(Boolean));
    const newlyAppeared = new Set();
    if (knownChildIds.current.size > 0) {
      currentIds.forEach((id) => {
        if (!knownChildIds.current.has(id)) newlyAppeared.add(id);
      });
    }
    knownChildIds.current = currentIds;
    return newlyAppeared;
  }

  async function fetchChildren() {
    if (previewBlocked.current) return;

    if (activeFetch.current) activeFetch.current.abort();
    const controller = new AbortController();
    activeFetch.current = controller;

    const urlParams = new URLSearchParams(window.location.search);
    const outParams = new URLSearchParams();
    outParams.append('limit', urlParams.get('limit') || '100');
    const checkedOutAfter = urlParams.get('checked_out_after');
    if (checkedOutAfter) outParams.append('checked_out_after', checkedOutAfter);

    const fetchSignature = window.location.search;
    const filterChanged = fetchSignature !== lastFetchParams.current;

    try {
      const response = await fetch(
        `${API_URL}/v1/checkins/checkouts/?${outParams.toString()}`,
        { signal: controller.signal }
      );
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }

      const sorted = prepareChildren(await response.json());
      setChildren((previous) => reconcileChildren(previous, sorted));
      setListError(false);
      setLoading(false);
      setPollFailed(false);

      const serverConfirmedById = new Map();
      sorted.forEach((child) => {
        const childId = getChildId(child);
        if (childId) {
          serverConfirmedById.set(
            childId,
            Boolean(child.checked_out_confirmed_at)
          );
        }
      });
      setOverrides((previous) => {
        let changed = false;
        const next = new Map(previous);
        next.forEach((override, childId) => {
          const serverConfirmed = serverConfirmedById.get(childId);
          if (
            typeof serverConfirmed === 'boolean' &&
            serverConfirmed === override.confirmed
          ) {
            next.delete(childId);
            changed = true;
          }
        });
        return changed ? next : previous;
      });

      if (filterChanged) {
        knownChildIds.current = new Set(sorted.map(getChildId).filter(Boolean));
        clearFlashStyles();
        lastFetchParams.current = fetchSignature;
      } else {
        const newIds = computeNewChildIds(sorted);
        if (newIds.size > 0) {
          setFlashIds(newIds);
          if (flashTimeout.current) clearTimeout(flashTimeout.current);
          flashTimeout.current = setTimeout(
            clearFlashStyles,
            FLASH_RESET_DELAY_MS
          );
        }
      }
    } catch (error) {
      if (error?.name === 'AbortError') return;
      console.error('Error fetching children data:', error);
      setChildren([]);
      setListError(true);
      setLoading(false);
      setPollFailed(true);
      setFlashIds(new Set());
    } finally {
      if (activeFetch.current === controller) activeFetch.current = null;
    }
  }
  fetchChildrenRef.current = fetchChildren;

  function replaceSelection(ids, includeUnassigned, { empty = false } = {}) {
    const newSearch = buildLocationGroupSearch(window.location.search, {
      ids,
      includeUnassigned,
      empty
    });
    const newUrl = newSearch
      ? `?${newSearch}`
      : window.location.pathname;
    history.replaceState(null, '', newUrl);
    setSelection(getSelectedFromURL(window.location.search, groups));
    fetchChildrenRef.current();
  }

  function handleGroupChange(event) {
    const container = groupContainerRef.current;
    if (!container) return;

    const ids = [];
    let includeUnassigned = false;
    container
      .querySelectorAll('input[type="checkbox"][data-lg-id]')
      .forEach((checkbox) => {
        if (!checkbox.checked) return;
        const value = checkbox.getAttribute('data-lg-id');
        if (value === 'unassigned') {
          includeUnassigned = true;
        } else {
          const id = Number(value);
          if (Number.isFinite(id) && id > 0) ids.push(id);
        }
      });

    if (ids.length === 0 && !includeUnassigned) {
      replaceSelection([], false, { empty: true });
      return;
    }
    replaceSelection(ids, includeUnassigned);
  }

  function handleSelectAll() {
    if (selectAllLabel === 'Deselect all') {
      replaceSelection([], false, { empty: true });
      return;
    }
    replaceSelection([], false);
  }

  const handleConfirm = useCallback(async (child, confirmed, checkbox) => {
    const childId = getChildId(child);
    const flightKey = childId || getChildSignature(child);
    if (inFlight.current.has(flightKey)) {
      checkbox.checked = isConfirmed(child);
      return;
    }

    let endpoint = '';
    if (child.source === 'manual') {
      if (!child.public_id) {
        console.error('Missing public_id for manual confirmation');
        checkbox.checked = isConfirmed(child);
        return;
      }
      endpoint = `${API_URL}/v1/checkins/manual-checkins/${encodeURIComponent(child.public_id)}/checked_out_confirmed`;
    } else {
      if (child.source && child.source !== 'planning_center') {
        console.warn(`Skipping confirmation for source: ${child.source}`);
        checkbox.checked = isConfirmed(child);
        return;
      }
      if (!child.planning_center_id) {
        console.error('Missing planning_center_id for confirmation');
        checkbox.checked = isConfirmed(child);
        return;
      }
      endpoint = `${API_URL}/v1/checkins/${encodeURIComponent(child.planning_center_id)}/checked_out_confirmed`;
    }

    const optimisticOverrides = new Map(overridesRef.current);
    optimisticOverrides.set(childId, { confirmed, timestamp: Date.now() });
    overridesRef.current = optimisticOverrides;
    setOverrides(optimisticOverrides);

    inFlight.current.add(flightKey);
    try {
      const response = await fetch(endpoint, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ confirmed })
      });
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
    } catch (error) {
      console.error('Error confirming checkout:', error);
      const revertedOverrides = new Map(overridesRef.current);
      revertedOverrides.delete(childId);
      overridesRef.current = revertedOverrides;
      setOverrides(revertedOverrides);
    } finally {
      inFlight.current.delete(flightKey);
    }
  }, [isConfirmed]);

  function openOverdueSheet() {
    openerRef.current = document.activeElement;
    const snapshot = liveOverdue.slice();
    setDrawerRows(snapshot);
    setDrawerCount(snapshot.length);
    setOverdueOpen(true);
  }

  function closeOverdueSheet() {
    setOverdueOpen(false);
  }

  function syncSheetToViewport({ lift = true } = {}) {
    const sheet = sheetRef.current;
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

  function lockBodyScroll() {
    if (!document.body) return;
    const scrollY = window.scrollY || 0;
    document.body.dataset.checkoutsScrollY = String(scrollY);
    const style = document.body.style;
    style.position = 'fixed';
    style.top = `-${scrollY}px`;
    style.left = '0';
    style.right = '0';
    style.width = '100%';
    style.overflow = 'hidden';
  }

  function unlockBodyScroll() {
    if (!document.body) return;
    const rawScrollY = document.body.dataset.checkoutsScrollY;
    delete document.body.dataset.checkoutsScrollY;
    const style = document.body.style;
    style.position = '';
    style.top = '';
    style.left = '';
    style.right = '';
    style.width = '';
    style.overflow = '';
    if (rawScrollY != null && typeof window.scrollTo === 'function') {
      try {
        window.scrollTo(0, Number(rawScrollY) || 0);
      } catch {
        // Some embedders and jsdom do not implement scrollTo.
      }
    }
  }

  useLayoutEffect(() => {
    if (overrides.size === 0) return undefined;
    const prune = () => {
      const now = Date.now();
      let changed = false;
      const next = new Map(overridesRef.current);
      next.forEach((override, childId) => {
        if (now - override.timestamp > CONFIRM_OVERRIDE_TTL_MS) {
          next.delete(childId);
          changed = true;
        }
      });
      if (changed) {
        overridesRef.current = next;
        setOverrides(next);
      }
    };
    return subscribeClock(prune);
  }, [overrides]);

  useLayoutEffect(() => {
    fetchChildrenRef.current();
    const interval = setInterval(() => fetchChildrenRef.current(), 3000);
    return () => {
      clearInterval(interval);
      if (activeFetch.current) activeFetch.current.abort();
      if (flashTimeout.current) clearTimeout(flashTimeout.current);
    };
  }, []);

  useLayoutEffect(() => {
    let cancelled = false;
    fetch(`${API_URL}/v1/location_groups`, { credentials: 'same-origin' })
      .then((response) => {
        if (!response.ok) {
          throw new Error(`HTTP error! status: ${response.status}`);
        }
        return response.json();
      })
      .then((data) => {
        if (cancelled) return;
        let nextGroups = [];
        if (Array.isArray(data)) nextGroups = data;
        else if (Array.isArray(data.location_groups)) {
          nextGroups = data.location_groups;
        } else if (Array.isArray(data.checkins)) nextGroups = [];
        setGroups(nextGroups);
        setGroupsLoaded(true);
        setSelection(getSelectedFromURL(window.location.search, nextGroups));
      })
      .catch((error) => {
        console.error('Error fetching location groups:', error);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useLayoutEffect(() => {
    if (typeof window.initKebabMenu === 'function') {
      window.initKebabMenu();
    } else if (
      window.NWKidsKebabMenu &&
      typeof window.NWKidsKebabMenu.initKebabMenu === 'function'
    ) {
      window.NWKidsKebabMenu.initKebabMenu();
    }
  }, []);

  useLayoutEffect(() => {
    const controls = searchControlsRef.current;
    if (!controls) return;

    if (searchExpanded) {
      controls.style.height = '0px';
      void controls.offsetHeight;
      controls.style.height = `${controls.scrollHeight}px`;
      searchInputRef.current?.focus();
    } else {
      controls.style.height = `${controls.scrollHeight}px`;
      void controls.offsetHeight;
      controls.style.height = '0px';
    }
  }, [searchExpanded]);

  useLayoutEffect(() => {
    const onResize = () => {
      requestAnimationFrame(() => {
        const list = listRef.current;
        if (list) {
          const maxScrollTop = Math.max(
            0,
            list.scrollHeight - list.clientHeight
          );
          if (list.scrollTop > maxScrollTop) list.scrollTop = maxScrollTop;
        }
        if (overdueOpenRef.current) syncSheetToViewport();
        if (expandedRef.current && searchControlsRef.current) {
          searchControlsRef.current.style.height = `${searchControlsRef.current.scrollHeight}px`;
        }
      });
    };

    window.addEventListener('resize', onResize);
    const viewport = window.visualViewport;
    if (viewport) viewport.addEventListener('resize', onResize);
    return () => {
      window.removeEventListener('resize', onResize);
      if (viewport) viewport.removeEventListener('resize', onResize);
    };
  }, []);

  useLayoutEffect(() => {
    syncSheetToViewport({ lift: false });
  }, []);

  useLayoutEffect(() => {
    const sheet = sheetRef.current;
    if (!sheet) return;

    if (overdueOpen) {
      sheet.removeAttribute('inert');
      syncSheetToViewport();
      lockBodyScroll();
      sheet.focus({ preventScroll: true });
      return;
    }

    sheet.setAttribute('inert', '');
    sheet.style.bottom = '';
    unlockBodyScroll();
    const opener = openerRef.current;
    openerRef.current = null;
    if (
      opener &&
      opener !== document.body &&
      opener.isConnected &&
      typeof opener.focus === 'function'
    ) {
      opener.focus({ preventScroll: true });
    }
  }, [overdueOpen]);

  useLayoutEffect(() => {
    if (!overdueOpen) return undefined;
    const onKeyDown = (event) => {
      if (event.key === 'Escape') closeOverdueSheet();
    };
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [overdueOpen]);

  useLayoutEffect(() => {
    if (!overdueOpen || pollFailed) return;
    const byId = new Map(
      children.map((child) => [getChildId(child), child])
    );
    setDrawerRows((previous) => {
      let changed = false;
      const next = [];
      previous.forEach((row) => {
        const current = byId.get(getChildId(row));
        if (!current) {
          changed = true;
          return;
        }
        if (current !== row) changed = true;
        next.push(current);
      });
      next.sort(
        (a, b) =>
          (b.checked_out_at_ms ?? 0) -
            (a.checked_out_at_ms ?? 0) || compareByCheckoutId(a, b)
      );
      return changed ? next : previous;
    });
  }, [children, overdueOpen, pollFailed]);

  useLayoutEffect(() => {
    if (!overdueOpen || pollFailed) return;
    setDrawerCount(liveOverdue.length);
  }, [overdueOpen, pollFailed, liveOverdue.length]);

  useLayoutEffect(() => {
    const list = listRef.current;
    if (!list) return;
    if (listError) {
      list.scrollTop = 0;
      return;
    }
    const maxScrollTop = Math.max(0, list.scrollHeight - list.clientHeight);
    if (list.scrollTop > maxScrollTop) list.scrollTop = maxScrollTop;
  }, [children, searchText, hideConfirmed, selection, listError]);

  useLayoutEffect(() => {
    if (!badgeRef.current) return;
    if (overdueCount > lastBadgeCount.current) {
      const badge = badgeRef.current;
      badge.classList.remove('overdue-badge-jiggle');
      void badge.offsetWidth;
      badge.classList.add('overdue-badge-jiggle');
    }
    lastBadgeCount.current = overdueCount;
  }, [overdueCount]);

  useLayoutEffect(() => {
    window.__checkoutsInitialized = true;

    // The dev preview helper is a classic script served only in dev. It
    // dispatches the demo rows instead of using a production window handle.
    const onPreview = (event) => {
      previewBlocked.current = true;
      if (activeFetch.current) activeFetch.current.abort();
      const prepared = prepareChildren(event.detail);
      knownChildIds.current = new Set(
        prepared.map(getChildId).filter(Boolean)
      );
      setChildren((previous) => reconcileChildren(previous, prepared));
      setListError(false);
      setLoading(false);
      setPollFailed(false);
      setFlashIds(new Set());
    };
    window.addEventListener('checkouts-preview', onPreview);

    return () => {
      window.removeEventListener('checkouts-preview', onPreview);
      if (activeFetch.current) activeFetch.current.abort();
      unlockBodyScroll();
    };
  }, []);

  const currentTimeHost = document.getElementById('current-time');
  const searchToggleHost = document.getElementById('search-toggle-host');
  const emptyMessage = searchText.trim()
    ? 'No matching children'
    : hideConfirmed
      ? 'No unconfirmed children'
      : 'No children called yet';

  return (
    <>
      {currentTimeHost && createPortal(<HeaderTime />, currentTimeHost)}
      {searchToggleHost &&
        createPortal(
          <button
            id="search-toggle-button"
            type="button"
            aria-expanded={searchExpanded ? 'true' : 'false'}
            aria-controls="search-controls"
            aria-label="Toggle search"
            class="cursor-pointer rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-700 shadow-sm hover:bg-slate-50"
            onClick={() => setSearchExpanded((value) => !value)}
          >
            <svg
              xmlns="http://www.w3.org/2000/svg"
              fill="none"
              viewBox="0 0 24 24"
              stroke-width="2"
              stroke="currentColor"
              class={`h-5 w-5 transition-transform duration-300${searchExpanded ? ' rotate-180' : ''}`}
              data-search-toggle-icon
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d={searchTogglePathOne}
              />
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d={searchTogglePathTwo}
              />
            </svg>
          </button>,
          searchToggleHost
        )}

      <div class="relative z-10 min-h-screen flex flex-col box-border lg:h-screen lg:pt-28 pb-20">
        <div class="relative z-10 flex flex-col items-center gap-2 px-4 md:flex-row md:justify-center md:gap-3">
          <div
            id="search-controls"
            ref={searchControlsRef}
            class={`w-full max-w-md md:flex-1 md:max-w-2xl${searchExpanded ? ' is-expanded' : ''}`}
          >
            <div class="overflow-hidden search-controls-panel pb-2 md:pt-4">
              <div class="flex flex-col items-center gap-2 md:flex-row md:items-center md:gap-3">
                <input
                  id="search-input"
                  ref={searchInputRef}
                  type="search"
                  autocomplete="off"
                  placeholder="Search by name or code"
                  value={searchText}
                  onInput={(event) => setSearchText(event.currentTarget.value)}
                  class="w-full rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-base shadow-sm focus:border-slate-500 focus:outline-none md:flex-1 md:min-w-0"
                />
                <label class="inline-flex cursor-pointer select-none items-center gap-2 text-sm font-medium text-slate-700">
                  Hide confirmed
                  <span class="relative inline-flex h-5 w-9 shrink-0 items-center">
                    <input
                      id="hide-confirmed-toggle"
                      type="checkbox"
                      role="switch"
                      aria-label="Hide confirmed children"
                      class="peer absolute h-0 w-0 opacity-0"
                      checked={hideConfirmed}
                      onChange={(event) =>
                        setHideConfirmed(event.currentTarget.checked)
                      }
                    />
                    <span class="pointer-events-none absolute inset-0 rounded-full bg-slate-200 transition-colors duration-200 peer-checked:bg-emerald-500"></span>
                    <span class="pointer-events-none absolute left-0.5 top-0.5 h-4 w-4 rounded-full bg-white shadow transition-transform duration-200 peer-checked:translate-x-4"></span>
                  </span>
                </label>
              </div>
              <div
                id="location-group-filter"
                class="mt-3 border-t border-slate-200 pt-3"
              >
                <div class="flex items-center justify-between mb-2">
                  <span class="text-sm font-medium text-slate-700">
                    Location groups
                  </span>
                  <button
                    id="location-group-select-all"
                    type="button"
                    class="cursor-pointer text-xs font-semibold text-blue-600 hover:text-blue-700"
                    onClick={handleSelectAll}
                  >
                    {selectAllLabel}
                  </button>
                </div>
                <div
                  id="location-group-checkboxes"
                  ref={groupContainerRef}
                  class="flex flex-wrap gap-2"
                  onChange={handleGroupChange}
                >
                  {groupsLoaded &&
                    groups.map((group) => (
                      <label
                        class="inline-flex items-center gap-1.5 text-sm cursor-pointer"
                        key={`group-${group.id}`}
                      >
                        <input
                          type="checkbox"
                          data-lg-id={String(group.id)}
                          class="h-4 w-4 rounded border-slate-300 cursor-pointer"
                          checked={isGroupSelected(
                            selection,
                            String(group.id)
                          )}
                        />{' '}
                        <span
                          style={{
                            backgroundColor: getLocationGroupColor(group.id)
                          }}
                          class="inline-block h-3 w-3 rounded-sm border border-black/10 shrink-0"
                        ></span>{' '}
                        {group.name || ''}
                      </label>
                    ))}
                  {groupsLoaded && (
                    <label class="inline-flex items-center gap-1.5 text-sm cursor-pointer">
                      <input
                        type="checkbox"
                        data-lg-id="unassigned"
                        class="h-4 w-4 rounded border-slate-300 cursor-pointer"
                        checked={isGroupSelected(selection, 'unassigned')}
                      />{' '}
                      <span
                        style={{
                          backgroundColor: getLocationGroupColor(null)
                        }}
                        class="inline-block h-3 w-3 rounded-sm border border-black/10 shrink-0"
                      ></span>{' '}
                      Unassigned
                    </label>
                  )}
                </div>
              </div>
            </div>
          </div>
        </div>

        <div
          id="children-list"
          ref={listRef}
          style={{ overflowAnchor: 'none' }}
          class="grid grid-cols-1 gap-3 px-4 py-4 md:grid-cols-2 lg:flex-1 lg:min-h-0 lg:overflow-y-auto lg:content-start lg:grid-cols-3"
        >
          {listError ? (
            <div class="text-center text-red-500 py-8">
              Error loading data. Please try again.
            </div>
          ) : loading ? (
            <div class="text-center text-gray-500 py-8">
              Loading children data...
            </div>
          ) : displayChildren.length === 0 ? (
            <div class="text-center py-12 text-3xl text-slate-500">
              {emptyMessage}
            </div>
          ) : (
            displayChildren.map((child, index) => (
              <ChildRow
                key={stableRowKey(child, index)}
                child={child}
                confirmed={isConfirmed(child)}
                flash={flashIds.has(getChildId(child))}
                onConfirm={handleConfirm}
              />
            ))
          )}
        </div>
      </div>

      <button
        id="overdue-badge"
        ref={badgeRef}
        type="button"
        class={`${badgeBaseClass}${overdueCount > 0 ? '' : ' hidden'}`}
        aria-label={
          overdueCount > 0
            ? `${overdueCount} overdue checkouts, tap to view`
            : 'No overdue checkouts'
        }
        onClick={openOverdueSheet}
      >
        {overdueCount > 0
          ? `${overdueCount} overdue. Tap to view`
          : 'overdue'}
      </button>

      <div
        id="overdue-sheet-backdrop"
        class={`fixed inset-0 bg-black/40 z-40 cursor-pointer touch-manipulation${overdueOpen ? '' : ' hidden'}`}
        aria-hidden="true"
        onClick={closeOverdueSheet}
      ></div>
      <div
        id="overdue-sheet"
        ref={sheetRef}
        class={`${sheetBaseClass}${overdueOpen ? '' : ' translate-y-full'}`}
        aria-hidden={overdueOpen ? 'false' : 'true'}
        role="dialog"
        aria-label="Overdue checkouts"
        tabindex="-1"
      >
        <div class="flex items-center justify-between gap-2 px-4 pt-3 pb-2 shrink-0">
          <div class="flex items-center gap-2 min-w-0">
            <span class="h-2 w-2 rounded-full bg-red-500 shrink-0"></span>
            <h2
              id="overdue-sheet-title"
              class="text-sm font-bold text-slate-800 truncate"
            >
              Overdue &gt;5 min
            </h2>
            <span
              id="overdue-sheet-count"
              class="text-xs font-semibold bg-red-100 text-red-700 px-2 py-0.5 rounded-full shrink-0"
            >
              {drawerCount > 0 ? `${drawerCount} overdue` : 'No overdue'}
            </span>
          </div>
          <button
            id="overdue-sheet-close"
            type="button"
            aria-label="Close overdue list"
            class="h-8 w-8 shrink-0 inline-flex items-center justify-center rounded-full text-slate-500 hover:bg-slate-200 hover:text-slate-700 cursor-pointer touch-manipulation"
            onClick={closeOverdueSheet}
          >
            <svg
              xmlns="http://www.w3.org/2000/svg"
              viewBox="0 0 20 20"
              fill="currentColor"
              class="h-5 w-5"
            >
              <path d="M6.28 5.22a.75.75 0 0 0-1.06 1.06L8.94 10l-3.72 3.72a.75.75 0 1 0 1.06 1.06L10 11.06l3.72 3.72a.75.75 0 1 0 1.06-1.06L11.06 10l3.72-3.72a.75.75 0 0 0-1.06-1.06L10 8.94 6.28 5.22Z" />
            </svg>
          </button>
        </div>
        <div class="px-2 pb-2">
          <div class="h-1 w-10 mx-auto rounded-full bg-slate-300"></div>
        </div>
        <div
          id="overdue-sheet-list"
          class="flex-1 overflow-y-auto px-4 pb-4 space-y-3"
        >
          {drawerRows.length === 0 ? (
            <div class="text-center py-6 text-sm text-slate-500">
              No overdue checkouts
            </div>
          ) : (
            drawerRows.map((child, index) => (
              <OverdueRow
                key={stableRowKey(child, index)}
                child={child}
                confirmed={isConfirmed(child)}
                groups={groups}
                onConfirm={handleConfirm}
              />
            ))
          )}
        </div>
      </div>
    </>
  );
}
