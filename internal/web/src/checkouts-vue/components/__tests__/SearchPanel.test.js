// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest';
import { createApp, h, nextTick, ref } from 'vue';
import SearchPanel from '../SearchPanel.vue';

function mountPanel() {
  const container = document.createElement('div');
  document.body.appendChild(container);

  const open = ref(false);
  const groupsLoaded = ref(false);
  const groups = ref([]);
  const selection = {
    ids: new Set(),
    names: new Set(),
    includeUnassigned: false,
    isEmpty: false,
    hasGroupParam: false
  };

  const app = createApp({
    setup() {
      return () =>
        h(SearchPanel, {
          open: open.value,
          searchText: '',
          hideConfirmed: false,
          groups: groups.value,
          groupsLoaded: groupsLoaded.value,
          selection,
          selectAllText: 'Select all'
        });
    }
  });
  app.mount(container);

  return { app, container, open, groupsLoaded, groups };
}

afterEach(() => {
  document.body.innerHTML = '';
});

describe('SearchPanel', () => {
  it('removes collapsed controls from the tab order and accessibility tree', async () => {
    const { app, container, open } = mountPanel();
    const controls = container.querySelector('#search-controls');

    expect(controls.hasAttribute('inert')).toBe(true);
    expect(controls.getAttribute('aria-hidden')).toBe('true');

    open.value = true;
    await nextTick();

    expect(controls.hasAttribute('inert')).toBe(false);
    expect(controls.getAttribute('aria-hidden')).toBe('false');
    expect(document.activeElement).toBe(
      container.querySelector('#search-input')
    );

    open.value = false;
    await nextTick();

    expect(controls.hasAttribute('inert')).toBe(true);
    expect(controls.getAttribute('aria-hidden')).toBe('true');

    app.unmount();
  });

  it('re-measures the expanded panel when location groups arrive late', async () => {
    const { app, container, open, groupsLoaded, groups } = mountPanel();
    const controls = container.querySelector('#search-controls');

    let scrollHeight = 100;
    Object.defineProperty(controls, 'scrollHeight', {
      configurable: true,
      get: () => scrollHeight
    });

    open.value = true;
    await nextTick();
    expect(controls.style.height).toBe('100px');

    scrollHeight = 180;
    groups.value = [{ id: 1, name: 'Nursery' }];
    groupsLoaded.value = true;
    await nextTick();

    expect(
      container.querySelectorAll('#location-group-checkboxes input')
    ).toHaveLength(2);
    expect(controls.style.height).toBe('180px');

    app.unmount();
  });
});
