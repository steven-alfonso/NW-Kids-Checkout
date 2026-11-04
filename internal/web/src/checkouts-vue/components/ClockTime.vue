<script setup>
import { computed } from 'vue';
import { useClock } from '../composables/useClock.js';
import { formatHeaderTime } from '../lib/checkouts.js';

// Kept as its own component so the clock's reactive read stays out of App's
// render effect; only this text updates once per second.
const nowMs = useClock();
const label = computed(() => formatHeaderTime(nowMs.value));
</script>

<template>{{ label }}</template>
