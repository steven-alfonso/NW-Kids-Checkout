<script setup>
import { onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { getLocationGroupColor } from '../lib/checkouts.js';

const props = defineProps({
  open: { type: Boolean, required: true },
  searchText: { type: String, default: '' },
  hideConfirmed: { type: Boolean, required: true },
  groups: { type: Array, default: () => [] },
  groupsLoaded: { type: Boolean, default: false },
  selection: { type: Object, required: true },
  selectAllText: { type: String, required: true }
});

const emit = defineEmits([
  'search-input',
  'hide-confirmed-change',
  'group-change',
  'select-all'
]);

const controlsEl = ref(null);
const inputEl = ref(null);

function hasFilter() {
  return (
    props.selection.ids.size > 0 ||
    props.selection.names.size > 0 ||
    props.selection.includeUnassigned
  );
}

function isGroupChecked(groupId) {
  if (props.selection.isEmpty) return false;
  if (!hasFilter()) return true;
  return props.selection.ids.has(Number(groupId));
}

function isUnassignedChecked() {
  if (props.selection.isEmpty) return false;
  if (!hasFilter()) return true;
  return props.selection.includeUnassigned;
}

function animate(expanded) {
  const element = controlsEl.value;
  if (!element) return;
  if (expanded) {
    element.style.height = '0px';
    void element.offsetHeight;
    element.style.height = `${element.scrollHeight}px`;
    inputEl.value?.focus();
  } else {
    element.style.height = `${element.scrollHeight}px`;
    void element.offsetHeight;
    element.style.height = '0px';
  }
}

watch(() => props.open, animate, { flush: 'post' });

function resizeToContent() {
  if (!props.open || !controlsEl.value) return;
  controlsEl.value.style.height = `${controlsEl.value.scrollHeight}px`;
}

// Location groups can arrive after the panel is already expanded (the board
// loads them in parallel with checkouts). Re-measure the fixed pixel height so
// the new filter rows are not clipped by the panel's overflow-hidden wrapper.
watch(
  [() => props.groupsLoaded, () => props.groups],
  resizeToContent,
  { flush: 'post' }
);

function onResize() {
  resizeToContent();
}

onMounted(() => window.addEventListener('resize', onResize));
onBeforeUnmount(() => window.removeEventListener('resize', onResize));
</script>

<template>
  <div
    id="search-controls"
    ref="controlsEl"
    class="w-full max-w-md md:flex-1 md:max-w-2xl"
    :class="{ 'is-expanded': open }"
  >
    <div class="overflow-hidden search-controls-panel pb-2 md:pt-4">
      <div
        class="flex flex-col items-center gap-2 md:flex-row md:items-center md:gap-3"
      >
        <input
          id="search-input"
          ref="inputEl"
          type="search"
          autocomplete="off"
          placeholder="Search by name or code"
          :value="searchText"
          class="w-full rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-base shadow-sm focus:border-slate-500 focus:outline-none md:flex-1 md:min-w-0"
          @input="emit('search-input', $event.target.value)"
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
              :checked="hideConfirmed"
              @change="emit('hide-confirmed-change', $event.target.checked)"
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
      <div
        id="location-group-filter"
        class="mt-3 border-t border-slate-200 pt-3"
      >
        <div class="flex items-center justify-between mb-2">
          <span class="text-sm font-medium text-slate-700">Location groups</span>
          <button
            id="location-group-select-all"
            type="button"
            class="cursor-pointer text-xs font-semibold text-blue-600 hover:text-blue-700"
            @click="emit('select-all')"
          >
            {{ selectAllText }}
          </button>
        </div>
        <div id="location-group-checkboxes" class="flex flex-wrap gap-2">
          <template v-if="groupsLoaded">
            <label
              v-for="group in groups"
              :key="group.id"
              class="inline-flex items-center gap-1.5 text-sm cursor-pointer"
            >
              <input
                type="checkbox"
                :data-lg-id="String(group.id)"
                class="h-4 w-4 rounded border-slate-300 cursor-pointer"
                :checked="isGroupChecked(group.id)"
                @change="
                  emit('group-change', {
                    id: group.id,
                    checked: $event.target.checked
                  })
                "
              />
              <span
                class="inline-block h-3 w-3 rounded-sm border border-black/10 shrink-0"
                :style="{ backgroundColor: getLocationGroupColor(group.id) }"
              ></span>
              {{ group.name }}
            </label>
            <label
              class="inline-flex items-center gap-1.5 text-sm cursor-pointer"
            >
              <input
                type="checkbox"
                data-lg-id="unassigned"
                class="h-4 w-4 rounded border-slate-300 cursor-pointer"
                :checked="isUnassignedChecked()"
                @change="
                  emit('group-change', {
                    id: 'unassigned',
                    checked: $event.target.checked
                  })
                "
              />
              <span
                class="inline-block h-3 w-3 rounded-sm border border-black/10 shrink-0"
                :style="{ backgroundColor: getLocationGroupColor(null) }"
              ></span>
              Unassigned
            </label>
          </template>
        </div>
      </div>
    </div>
  </div>
</template>
