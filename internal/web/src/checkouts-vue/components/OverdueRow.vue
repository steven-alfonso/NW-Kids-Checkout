<script setup>
import { computed } from 'vue';
import {
  calculateMinutesAgoFromTimestamp,
  getCheckedOutTimestamp,
  getLocationGroupColor,
  getTimePillClass,
  overdueGroupLabel
} from '../lib/checkouts.js';

const props = defineProps({
  child: { type: Object, required: true },
  childId: { type: String, required: true },
  confirmed: { type: Boolean, required: true },
  nowMs: { type: Number, required: true },
  locationGroups: { type: Array, default: () => [] }
});

defineEmits(['confirm']);

const fullName = computed(
  () => `${props.child.first_name || ''} ${props.child.last_name || ''}`
);
const code = computed(() =>
  props.child.source === 'manual'
    ? '---'
    : props.child.security_code || '----'
);
const checkedOutAtMs = computed(
  () =>
    props.child.checked_out_at_ms ??
    getCheckedOutTimestamp(props.child.checked_out_at)
);
const minutesLabel = computed(() =>
  calculateMinutesAgoFromTimestamp(checkedOutAtMs.value, props.nowMs)
);
const pillClass = computed(() =>
  getTimePillClass(checkedOutAtMs.value, props.confirmed, props.nowMs)
);
const barColor = computed(() =>
  getLocationGroupColor(props.child.location_group_id)
);
const groupLabel = computed(() =>
  overdueGroupLabel(props.child, props.locationGroups)
);
</script>

<template>
  <div class="bg-white rounded-lg shadow flex overflow-hidden">
    <div
      :style="{ backgroundColor: barColor, width: '6px', flexShrink: 0 }"
      aria-hidden="true"
    ></div>
    <div class="flex-1 py-2 px-3 flex flex-col gap-1">
      <div class="flex items-center gap-2">
        <span
          class="inline-block h-2.5 w-2.5 rounded-sm border border-black/10 shrink-0"
          :style="{ backgroundColor: barColor }"
        ></span>
        <span class="text-xs font-medium text-slate-600">{{ groupLabel }}</span>
        <span
          class="ml-auto text-white text-xs px-2.5 py-1 rounded-md child-time"
          :class="pillClass"
          :data-child-id="childId"
        >
          {{ minutesLabel }}
        </span>
      </div>
      <div class="font-bold text-gray-800 text-lg leading-tight">
        {{ fullName }}
        <img
          v-if="child.source === 'manual'"
          :src="'/static/img/star.svg'"
          alt="Manual checkin"
          class="inline-block h-5 w-5 ml-2 relative -top-0.5"
        />
      </div>
      <div class="flex justify-between items-center">
        <div class="text-black text-base">{{ code }}</div>
        <label
          class="relative flex items-center cursor-pointer leading-none"
          data-confirmed-label
          :data-confirmed-state="confirmed ? 'confirmed' : 'unconfirmed'"
        >
          <input
            type="checkbox"
            class="sr-only child-confirmed-checkbox"
            :aria-label="`Mark ${fullName} as confirmed`"
            :data-child-id="childId"
            :data-planning-center-id="child.planning_center_id || ''"
            :data-public-id="child.public_id || ''"
            :data-source="child.source || ''"
            :checked="confirmed"
            @change="$emit('confirm', $event)"
          />
          <img
            :src="'/static/img/confirmed-checkbox.svg'"
            alt=""
            class="h-9 w-9 block"
            data-confirmed-icon
          />
        </label>
      </div>
    </div>
  </div>
</template>
