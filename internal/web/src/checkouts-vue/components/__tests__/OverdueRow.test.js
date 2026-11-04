// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest';
import { createApp, h, nextTick, ref } from 'vue';
import OverdueRow from '../OverdueRow.vue';
import { CLOCK_KEY } from '../../composables/useClock.js';

const BASE = Date.parse('2026-01-15T12:00:00Z');

function mountRow(initialConfirmed) {
  const container = document.createElement('div');
  document.body.appendChild(container);

  const confirmed = ref(initialConfirmed);
  const app = createApp({
    setup() {
      return () =>
        h(OverdueRow, {
          child: {
            source: 'planning_center',
            planning_center_id: 'pc-1',
            first_name: 'Ada',
            last_name: 'Lovelace',
            security_code: '1234',
            location_group_id: 1,
            checked_out_at_ms: BASE
          },
          childId: 'pc:1',
          confirmed: confirmed.value,
          locationGroups: []
        });
    }
  });
  // Nine minutes after checkout keeps the unconfirmed pill red, so the test
  // sees the actual colour transition rather than a green-to-gray no-op.
  app.provide(CLOCK_KEY, ref(BASE + 9 * 60 * 1000));
  app.mount(container);

  return { app, container, confirmed };
}

afterEach(() => {
  document.body.innerHTML = '';
});

describe('OverdueRow', () => {
  it('fades the time pill when the checkout is confirmed', async () => {
    const { app, container, confirmed } = mountRow(false);
    const pill = container.querySelector('.child-time');

    expect(pill).not.toBeNull();
    expect(pill.className).toContain('bg-red-500');
    expect(pill.className).toContain('transition-colors');
    expect(pill.className).toContain('duration-1000');

    confirmed.value = true;
    await nextTick();

    expect(pill.className).toContain('bg-gray-400');
    expect(pill.className).not.toContain('bg-red-500');

    app.unmount();
  });
});
