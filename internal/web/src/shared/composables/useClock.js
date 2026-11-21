import { inject } from 'vue';

// The page clock is owned by useCheckouts and provided once by App. Components
// that display relative times subscribe to it directly, so a one-second tick
// does not invalidate the whole board.
// A string key rather than a Symbol: a Symbol is unique per module instance,
// so two separately bundled pages each get a different key and provide/inject
// silently fails across the bundle boundary.
export const CLOCK_KEY = 'nw-kids-clock';

export function useClock() {
  return inject(CLOCK_KEY);
}
