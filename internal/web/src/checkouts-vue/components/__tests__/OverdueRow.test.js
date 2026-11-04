// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest';
import { createApp, ref } from 'vue';
import OverdueRow from '../OverdueRow.vue';
import { CLOCK_KEY } from '../../composables/useClock.js';

const BASE = Date.parse('2026-01-15T12:00:00Z');

function mountRow(confirmed) {
  const container = document.createElement('div');
  document.body.appendChild(container);

  const app = createApp(OverdueRow, {
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
    confirmed,
    locationGroups: []
  });
  app.provide(CLOCK_KEY, ref(BASE));
  app.mount(container);

  return { app, container };
}

afterEach(() => {
  document.body.innerHTML = '';
});

describe('OverdueRow', () => {
  it('fades the time pill when the checkout is confirmed', () => {
    const { app, container } = mountRow(true);
    const pill = container.querySelector('.child-time');

    expect(pill).not.toBeNull();
    expect(pill.className).toContain('bg-gray-400');
    expect(pill.className).toContain('transition-colors');
    expect(pill.className).toContain('duration-1000');

    app.unmount();
  });
});
