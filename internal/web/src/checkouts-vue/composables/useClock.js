import { inject } from 'vue';

// The page clock is owned by useCheckouts and provided once by App. Components
// that display relative times subscribe to it directly, so a one-second tick
// does not invalidate the whole board.
export const CLOCK_KEY = Symbol('checkouts-clock');

export function useClock() {
  return inject(CLOCK_KEY);
}
