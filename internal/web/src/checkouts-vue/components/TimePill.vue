<script setup>
import { computed } from 'vue';
import { useClock } from '../composables/useClock.js';
import {
  calculateMinutesAgoFromTimestamp,
  getTimePillClass
} from '../lib/checkouts.js';

const props = defineProps({
  checkedOutAtMs: { type: Number, required: true },
  confirmed: { type: Boolean, required: true },
  childId: { type: String, required: true }
});

const nowMs = useClock();

const minutesLabel = computed(() =>
  calculateMinutesAgoFromTimestamp(props.checkedOutAtMs, nowMs.value)
);
const pillClass = computed(() =>
  getTimePillClass(props.checkedOutAtMs, props.confirmed, nowMs.value)
);
</script>

<template>
  <div :class="pillClass" :data-child-id="childId">
    {{ minutesLabel }}
  </div>
</template>
