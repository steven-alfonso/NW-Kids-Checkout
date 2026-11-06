// One shared 1s timer for every clock-driven label on the page.
//
// The timer only exists while something is subscribed, so the board does not
// keep a wakeup alive after unmount. Subscribers decide whether their own
// value changed; a label that still reads "3 min ago" does not re-render.
const TICK_MS = 1000;

const listeners = new Set();
let timer = null;

function tick() {
  listeners.forEach((listener) => listener());
}

export function subscribeClock(listener) {
  listeners.add(listener);
  if (!timer) {
    timer = setInterval(tick, TICK_MS);
  }
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0 && timer) {
      clearInterval(timer);
      timer = null;
    }
  };
}
