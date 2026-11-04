<script setup>
import { provide } from 'vue';
import ChildCard from './components/ChildCard.vue';
import ClockTime from './components/ClockTime.vue';
import OverdueRow from './components/OverdueRow.vue';
import SearchPanel from './components/SearchPanel.vue';
import { CLOCK_KEY } from './composables/useClock.js';
import { useCheckouts } from './composables/useCheckouts.js';
import { getChildId } from './lib/checkouts.js';

const {
  childrenList,
  badgeEl,
  sheetEl,
  nowMs,
  rawSearch,
  hideConfirmed,
  pollFailed,
  searchOpen,
  selection,
  selectAllText,
  locationGroups,
  groupsLoaded,
  visibleChildren,
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
  onGroupChange,
  onSelectAll,
  openSheet,
  closeSheet,
  isConfirmed,
  flashIds,
  confirmCheckedOut
} = useCheckouts();

provide(CLOCK_KEY, nowMs);

function childIdFor(child) {
  return getChildId(child);
}
</script>

<template>
  <Teleport to="#current-time"><ClockTime /></Teleport>

  <Teleport to="#search-toggle-slot">
    <button
      id="search-toggle-button"
      type="button"
      :aria-expanded="searchOpen ? 'true' : 'false'"
      aria-controls="search-controls"
      aria-label="Toggle search"
      class="cursor-pointer rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-700 shadow-sm hover:bg-slate-50"
      @click="toggleSearch"
    >
      <svg
        xmlns="http://www.w3.org/2000/svg"
        fill="none"
        viewBox="0 0 24 24"
        stroke-width="2"
        stroke="currentColor"
        class="h-5 w-5 transition-transform duration-300"
        :class="{ 'rotate-180': searchOpen }"
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
  </Teleport>

  <div
    class="relative z-10 min-h-screen flex flex-col box-border lg:h-screen lg:pt-28 pb-20"
  >
    <div
      class="relative z-10 flex flex-col items-center gap-2 px-4 md:flex-row md:justify-center md:gap-3"
    >
      <SearchPanel
        :open="searchOpen"
        :search-text="rawSearch"
        :hide-confirmed="hideConfirmed"
        :groups="locationGroups"
        :groups-loaded="groupsLoaded"
        :selection="selection"
        :select-all-text="selectAllText"
        @search-input="setSearchQuery"
        @hide-confirmed-change="setHideConfirmed"
        @group-change="onGroupChange"
        @select-all="onSelectAll"
      />
    </div>

    <div
      id="children-list"
      ref="childrenList"
      class="grid grid-cols-1 gap-3 px-4 py-4 md:grid-cols-2 lg:flex-1 lg:min-h-0 lg:overflow-y-auto lg:content-start lg:grid-cols-3"
    >
      <div v-if="pollFailed" class="text-center text-red-500 py-8">
        Error loading data. Please try again.
      </div>
      <div
        v-else-if="visibleChildren.length === 0"
        class="text-center py-12 text-3xl text-slate-500"
      >
        {{ emptyMessage }}
      </div>
      <template v-else>
        <ChildCard
          v-for="(child, index) in visibleChildren"
          :key="rowKey(child, index)"
          :child="child"
          :child-id="childIdFor(child)"
          :confirmed="isConfirmed(child)"
          :flashing="flashIds.has(childIdFor(child))"
          @confirm="confirmCheckedOut(child, $event)"
        />
      </template>
    </div>
  </div>

  <button
    id="overdue-badge"
    ref="badgeEl"
    type="button"
    class="fixed bottom-4 left-1/2 -translate-x-1/2 z-30 rounded-full bg-red-600 text-white px-5 py-2.5 text-sm font-bold shadow-[0_4px_12px_rgba(0,0,0,0.3)] hover:bg-red-700 active:bg-red-700 transition-colors cursor-pointer whitespace-nowrap w-max"
    :class="{ hidden: badgeHidden, 'overdue-badge-jiggle': badgeJiggling }"
    :aria-label="badgeAriaLabel"
    @click="openSheet"
  >
    {{ badgeText }}
  </button>

  <div
    id="overdue-sheet-backdrop"
    class="fixed inset-0 bg-black/40 z-40 cursor-pointer touch-manipulation"
    :class="{ hidden: !sheetOpen }"
    aria-hidden="true"
    @click="closeSheet"
  ></div>

  <div
    id="overdue-sheet"
    ref="sheetEl"
    class="fixed inset-x-0 bottom-0 z-40 bg-slate-50 rounded-t-2xl shadow-2xl border-t border-slate-200 flex flex-col"
    :class="{ 'translate-y-full': !sheetOpen }"
    :aria-hidden="sheetOpen ? 'false' : 'true'"
    role="dialog"
    aria-label="Overdue checkouts"
    tabindex="-1"
    inert
  >
    <div
      class="flex items-center justify-between gap-2 px-4 pt-3 pb-2 shrink-0"
    >
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
          {{ sheetCountLabel }}
        </span>
      </div>
      <button
        id="overdue-sheet-close"
        type="button"
        aria-label="Close overdue list"
        class="h-8 w-8 shrink-0 inline-flex items-center justify-center rounded-full text-slate-500 hover:bg-slate-200 hover:text-slate-700 cursor-pointer touch-manipulation"
        @click="closeSheet"
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
    <div
      id="overdue-sheet-list"
      class="flex-1 overflow-y-auto px-4 pb-4 space-y-3"
    >
      <div
        v-if="sheetRows.length === 0"
        class="text-center py-6 text-sm text-slate-500"
      >
        No overdue checkouts
      </div>
      <template v-else>
        <OverdueRow
          v-for="(child, index) in sheetRows"
          :key="rowKey(child, index)"
          :child="child"
          :child-id="childIdFor(child)"
          :confirmed="isConfirmed(child)"
          :location-groups="locationGroups"
          @confirm="confirmCheckedOut(child, $event)"
        />
      </template>
    </div>
  </div>
</template>
