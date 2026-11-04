// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createApp, nextTick } from 'vue';
import App from '../App.vue';

const BASE = Date.parse('2026-01-15T12:00:00Z');
const MINUTE = 60 * 1000;

function child(overrides = {}) {
  return {
    source: 'planning_center',
    planning_center_id: 'pc-overdue',
    public_id: '',
    first_name: 'Overdue',
    last_name: 'Kid',
    security_code: '5555',
    location_group_id: 1,
    checked_out_at: new Date(BASE - 6 * MINUTE).toISOString(),
    checked_out_confirmed_at: null,
    ...overrides
  };
}

function jsonResponse(body) {
  return Promise.resolve({
    ok: true,
    json: () => Promise.resolve(body)
  });
}

function installFetch(payload) {
  const fetchMock = vi.fn((url) => {
    if (String(url).includes('/v1/location_groups')) return jsonResponse([]);
    return jsonResponse({ checkins: payload(), manual_checkins: [] });
  });
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

function mountApp() {
  document.body.innerHTML = `
    <div id="current-time"></div>
    <div id="search-toggle-slot"></div>
    <div id="app"></div>
  `;
  const app = createApp(App);
  app.mount(document.getElementById('app'));
  return app;
}

async function flush() {
  for (let i = 0; i < 10; i += 1) await Promise.resolve();
  await nextTick();
}

function sheetRowIds() {
  return Array.from(
    document.querySelectorAll('#overdue-sheet-list .child-time')
  ).map((pill) => pill.dataset.childId);
}

beforeEach(() => {
  vi.stubGlobal('requestAnimationFrame', (callback) => {
    callback();
    return 0;
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
  document.body.innerHTML = '';
});

describe('checkouts App', () => {
  it('shows the loading message until the first fetch settles', async () => {
    let release;
    const gate = new Promise((resolve) => {
      release = resolve;
    });
    const fetchMock = vi.fn((url) => {
      if (String(url).includes('/v1/location_groups')) return jsonResponse([]);
      return gate;
    });
    vi.stubGlobal('fetch', fetchMock);

    const app = mountApp();
    const list = document.getElementById('children-list');
    expect(list.textContent).toContain('Loading children data');
    expect(list.textContent).not.toContain('No children called yet');

    release(
      await jsonResponse({
        checkins: [child({ planning_center_id: 'pc-1', first_name: 'Ada' })],
        manual_checkins: []
      })
    );
    await flush();

    expect(list.textContent).not.toContain('Loading children data');
    expect(list.textContent).toContain('Ada');

    app.unmount();
  });

  it('keeps sheet rows during the close transition and replaces them on reopen', async () => {
    let payload = [child({ planning_center_id: 'old' })];
    installFetch(() => payload);

    const app = mountApp();
    await flush();
    document.getElementById('overdue-badge').click();
    await nextTick();

    expect(sheetRowIds()).toEqual(['pc:old']);

    document.getElementById('overdue-sheet-close').click();
    await nextTick();

    expect(
      document.getElementById('overdue-sheet').getAttribute('aria-hidden')
    ).toBe('true');
    expect(sheetRowIds()).toEqual(['pc:old']);

    payload = [child({ planning_center_id: 'new', first_name: 'New' })];
    window.dispatchEvent(
      new CustomEvent('checkouts-preview', { detail: payload })
    );
    document.getElementById('overdue-badge').click();
    await nextTick();

    expect(sheetRowIds()).toEqual(['pc:new']);

    app.unmount();
  });

  it('updates the open sheet count on the clock tick without adding rows', async () => {
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] });
    vi.setSystemTime(BASE);
    installFetch(() => [
      child({
        planning_center_id: 'almost',
        checked_out_at: new Date(BASE - 5 * MINUTE + 1000).toISOString()
      })
    ]);

    const app = mountApp();
    await flush();
    document.getElementById('overdue-badge').click();
    await nextTick();

    expect(sheetRowIds()).toEqual([]);
    expect(document.getElementById('overdue-sheet-count').textContent).toBe(
      'No overdue'
    );

    vi.advanceTimersByTime(1000);
    await nextTick();

    expect(document.getElementById('overdue-sheet-count').textContent).toBe(
      '1 overdue'
    );
    expect(sheetRowIds()).toEqual([]);

    app.unmount();
  });

  it('lets a slow poll finish instead of aborting it on the next interval', async () => {
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] });
    vi.setSystemTime(BASE);

    let releaseHeld;
    const signals = [];
    const fetchMock = vi.fn((url, options = {}) => {
      if (String(url).includes('/v1/location_groups')) return jsonResponse([]);
      signals.push(options.signal);
      if (signals.length === 1) {
        return jsonResponse({
          checkins: [child({ planning_center_id: 'first' })],
          manual_checkins: []
        });
      }
      return new Promise((resolve) => {
        releaseHeld = () =>
          resolve({
            ok: true,
            json: () =>
              Promise.resolve({
                checkins: [
                  child({ planning_center_id: 'first' }),
                  child({ planning_center_id: 'held', first_name: 'Held' })
                ],
                manual_checkins: []
              })
          });
      });
    });
    vi.stubGlobal('fetch', fetchMock);

    const app = mountApp();
    await flush();
    expect(
      document.querySelectorAll('#children-list .child-time')
    ).toHaveLength(1);

    vi.advanceTimersByTime(3000);
    await flush();
    expect(signals).toHaveLength(2);
    expect(releaseHeld).toBeTypeOf('function');
    expect(signals[1].aborted).toBe(false);

    vi.advanceTimersByTime(3000);
    await flush();
    expect(signals).toHaveLength(2);
    expect(signals[1].aborted).toBe(false);

    releaseHeld();
    await flush();

    expect(
      document.querySelectorAll('#children-list .child-time')
    ).toHaveLength(2);
    expect(document.getElementById('children-list').textContent).toContain(
      'Held'
    );

    app.unmount();
  });

  it('traps Tab and Shift+Tab inside the open sheet', async () => {
    installFetch(() => [child()]);

    const app = mountApp();
    await flush();
    const badge = document.getElementById('overdue-badge');
    badge.focus();
    badge.click();
    await nextTick();

    const close = document.getElementById('overdue-sheet-close');
    const checkbox = document.querySelector(
      '#overdue-sheet-list input[type="checkbox"]'
    );
    expect(document.activeElement).toBe(document.getElementById('overdue-sheet'));
    expect(
      document.getElementById('overdue-sheet').getAttribute('aria-modal')
    ).toBe('true');

    close.focus();
    document.dispatchEvent(
      new KeyboardEvent('keydown', {
        key: 'Tab',
        shiftKey: true,
        bubbles: true,
        cancelable: true
      })
    );
    expect(document.activeElement).toBe(checkbox);

    document.dispatchEvent(
      new KeyboardEvent('keydown', {
        key: 'Tab',
        bubbles: true,
        cancelable: true
      })
    );
    expect(document.activeElement).toBe(close);

    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })
    );
    await nextTick();
    expect(document.activeElement).toBe(badge);

    app.unmount();
  });
});
