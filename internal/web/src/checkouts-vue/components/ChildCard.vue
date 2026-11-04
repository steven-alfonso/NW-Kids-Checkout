<script setup>
import { computed } from 'vue';
import {
  calculateMinutesAgoFromTimestamp,
  getCheckedOutTimestamp,
  getLocationGroupColor,
  getTimePillClass
} from '../lib/checkouts.js';

const props = defineProps({
  child: { type: Object, required: true },
  childId: { type: String, required: true },
  confirmed: { type: Boolean, required: true },
  nowMs: { type: Number, required: true },
  flashing: { type: Boolean, default: false }
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
</script>

<template>
  <div
    class="bg-white rounded-lg shadow-[0_0_10px_rgba(0,0,0,0.25)] flex"
    :class="{ 'child-card-flash': flashing }"
  >
    <div
      class="rounded-l-lg"
      :style="{ backgroundColor: barColor, width: '6px', flexShrink: 0 }"
      aria-hidden="true"
    ></div>
    <div class="flex-1 py-2.5 px-4 flex flex-col justify-center">
      <div class="font-bold text-gray-800 text-2xl mb-0">
        {{ fullName }}
        <img
          v-if="child.source === 'manual'"
          :src="'/static/img/star.svg'"
          alt="Manual checkin"
          class="inline-block h-5 w-5 ml-2 relative -top-0.5"
        />
      </div>
      <div class="flex justify-between items-center">
        <div class="text-black text-xl">{{ code }}</div>
        <div class="flex items-center gap-3">
          <div
            class="text-white transition-colors duration-1000 px-1.5 py-0 rounded-md text-base child-time"
            :class="pillClass"
            :data-child-id="childId"
          >
            {{ minutesLabel }}
          </div>
          <label
            class="relative flex items-center text-xs text-gray-600 cursor-pointer leading-none"
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
              class="h-10 w-10 block"
              data-confirmed-icon
            />
          </label>
        </div>
      </div>
    </div>
  </div>
</template>
