import { test, expect } from '@playwright/test';
import {
  BASE_TIME,
  defaultChildren,
  makeChild,
  makeManualChild,
  minutesAgo,
  minutesFromNow
} from './fixtures.js';
import { createState, installFakeApi } from './fake-api.js';
import {
  childCard,
  childCheckbox,
  childIds,
  childLabel,
  groupCheckbox,
  overdueCheckbox,
  overdueIds,
  overdueLabel,
  overdueRow,
  timeText,
  visibleChildRows
} from './selectors.js';

async function openBoard(page, testInfo, { state = createState(), query = '' } = {}) {
  const api = await installFakeApi(page, state);
  await page.clock.install({ time: BASE_TIME });
  await page.clock.pauseAt(BASE_TIME);
  await page.goto(`${testInfo.project.metadata.pagePath}${query}`);
  return api;
}

async function openSearch(page) {
  await page.locator('#search-toggle-button').click();
  await expect(page.locator('#search-input')).toBeVisible();
}

async function pollOnce(page) {
  const request = page.waitForRequest((request) =>
    request.url().includes('/v1/checkins/checkouts')
  );
  await page.clock.runFor(3000);
  await request;
}

async function openOverdueSheet(page) {
  await page.locator('#overdue-badge').click();
  await expect(page.locator('#overdue-sheet')).toHaveAttribute('aria-hidden', 'false');
}

async function toggleHideConfirmed(page) {
  await page.locator('label:has(#hide-confirmed-toggle)').click();
}

test.describe('checkouts board', () => {
  test('renders children newest-first with manual and confirmed states', async ({
    page
  }, testInfo) => {
    await openBoard(page, testInfo);

    await expect(visibleChildRows(page)).toHaveCount(7);
    expect(await childIds(page)).toEqual([
      'pc:fresh',
      'manual:manu',
      'pc:unassigned',
      'pc:done',
      'pc:edge',
      'pc:overdue',
      'pc:late'
    ]);

    await expect(childCard(page, 'pc:fresh')).toContainText('Fresh Kid');
    await expect(childCard(page, 'pc:fresh')).toContainText('1111');
    await expect(childCard(page, 'manual:manu')).toContainText('Manu Checkin');
    await expect(childCard(page, 'manual:manu')).toContainText('---');
    await expect(
      childCard(page, 'manual:manu').locator('img[alt="Manual checkin"]')
    ).toBeVisible();
    await expect(childLabel(page, 'pc:done')).toHaveAttribute(
      'data-confirmed-state',
      'confirmed'
    );
    await expect(childCheckbox(page, 'pc:done')).toBeChecked();
    await expect(childLabel(page, 'pc:fresh')).toHaveAttribute(
      'data-confirmed-state',
      'unconfirmed'
    );
  });

  test('exposes the expected accessible names and dialog roles', async ({
    page
  }, testInfo) => {
    await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);

    await expect(page.locator('#overdue-badge')).toHaveAttribute(
      'aria-label',
      '2 overdue checkouts, tap to view'
    );
    await expect(
      page.getByRole('checkbox', { name: 'Mark Fresh Kid as confirmed' })
    ).toHaveCount(1);
    await expect(page.locator('#search-toggle-button')).toHaveAttribute(
      'aria-label',
      'Toggle search'
    );
    await expect(page.locator('#search-toggle-button')).toHaveAttribute(
      'aria-controls',
      'search-controls'
    );

    await expect(page.locator('#overdue-sheet')).toHaveAttribute('aria-hidden', 'true');
    await expect(page.locator('#overdue-sheet')).toHaveAttribute('inert', '');

    await openOverdueSheet(page);
    const dialog = page.getByRole('dialog', { name: 'Overdue checkouts' });
    await expect(dialog).toBeVisible();
    await expect(dialog).toHaveAttribute('aria-hidden', 'false');
  });

  test('shows the empty state when the initial payload has no checkouts', async ({
    page
  }, testInfo) => {
    await openBoard(page, testInfo, { state: createState({ children: [] }) });

    await expect(visibleChildRows(page)).toHaveCount(0);
    await expect(page.locator('#children-list')).toContainText('No children called yet');
    await expect(page.locator('#overdue-badge')).toBeHidden();
  });

  test('skips children without a checkout time and clamps future timestamps', async ({
    page
  }, testInfo) => {
    const state = createState({
      children: [
        makeChild({
          planning_center_id: 'no-time',
          first_name: 'No',
          last_name: 'Time',
          checked_out_at: null
        }),
        makeChild({
          planning_center_id: 'bad-time',
          first_name: 'Bad',
          last_name: 'Time',
          checked_out_at: 'not-a-date'
        }),
        makeChild({
          planning_center_id: 'future',
          first_name: 'Future',
          last_name: 'Kid',
          checked_out_at: minutesFromNow(3)
        }),
        makeChild({
          planning_center_id: 'fresh',
          first_name: 'Fresh',
          last_name: 'Kid',
          checked_out_at: minutesAgo(0.1)
        })
      ]
    });

    await openBoard(page, testInfo, { state });
    await expect(visibleChildRows(page)).toHaveCount(2);
    await expect(childCard(page, 'pc:no-time')).toHaveCount(0);
    await expect(childCard(page, 'pc:bad-time')).toHaveCount(0);
    await expect(timeText(page, 'pc:future')).toHaveText('0 min ago');
  });

  test('updates the header and time pills from the clock', async ({ page }, testInfo) => {
    await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);

    await expect(page.locator('#current-time')).toHaveText('12:00');
    await expect(timeText(page, 'pc:fresh')).toHaveText('0 min ago');
    await expect(timeText(page, 'pc:edge')).toHaveText('3 min ago');
    await expect(timeText(page, 'pc:late')).toHaveText('7 min ago');

    await page.clock.runFor(60_000);

    await expect(page.locator('#current-time')).toHaveText('12:01');
    await expect(timeText(page, 'pc:fresh')).toHaveText('1 min ago');
    await expect(timeText(page, 'pc:edge')).toHaveText('4 min ago');
    await expect(timeText(page, 'pc:late')).toHaveText('8 min ago');
  });

  test('searches by name and security code, and restores on clear', async ({
    page
  }, testInfo) => {
    await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);
    await openSearch(page);

    await page.locator('#search-input').fill('fresh');
    await expect(visibleChildRows(page)).toHaveCount(1);
    expect(await childIds(page)).toEqual(['pc:fresh']);

    await page.locator('#search-input').fill('1111');
    await expect(visibleChildRows(page)).toHaveCount(1);
    expect(await childIds(page)).toEqual(['pc:fresh']);

    await page.locator('#search-input').fill('  FRESH  ');
    await expect(visibleChildRows(page)).toHaveCount(1);
    expect(await childIds(page)).toEqual(['pc:fresh']);

    await page.locator('#search-input').fill('kId');
    await expect(visibleChildRows(page)).toHaveCount(6);

    await page.locator('#search-input').fill('CHECKIN');
    await expect(visibleChildRows(page)).toHaveCount(1);
    expect(await childIds(page)).toEqual(['manual:manu']);

    await page.locator('#search-input').fill('nope');
    await expect(visibleChildRows(page)).toHaveCount(0);
    await expect(page.locator('#children-list')).toContainText('No matching children');

    await page.locator('#search-input').fill('');
    await expect(visibleChildRows(page)).toHaveCount(7);

    expect(new URL(page.url()).searchParams.has('q')).toBe(false);
  });

  test('hide-confirmed hides confirmed rows and shows its empty state', async ({
    page
  }, testInfo) => {
    await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);
    await openSearch(page);

    await toggleHideConfirmed(page);
    await expect(page.locator('#hide-confirmed-toggle')).toBeChecked();
    await expect(visibleChildRows(page)).toHaveCount(6);
    await expect(childCard(page, 'pc:done')).toHaveCount(0);
    expect(new URL(page.url()).searchParams.has('hide_confirmed')).toBe(false);

    await toggleHideConfirmed(page);
    await expect(page.locator('#hide-confirmed-toggle')).not.toBeChecked();
    await expect(visibleChildRows(page)).toHaveCount(7);

    const state = createState({
      children: [
        makeChild({
          planning_center_id: 'only-confirmed',
          first_name: 'Only',
          last_name: 'Confirmed',
          checked_out_at: minutesAgo(2),
          checked_out_confirmed_at: minutesAgo(1)
        })
      ]
    });
    await page.unroute('**/v1/**');
    await openBoard(page, testInfo, { state });
    await expect(visibleChildRows(page)).toHaveCount(1);
    await openSearch(page);
    await toggleHideConfirmed(page);
    await expect(visibleChildRows(page)).toHaveCount(0);
    await expect(page.locator('#children-list')).toContainText('No unconfirmed children');
  });

  test('filters location groups, keeps manual rows, and handles unassigned', async ({
    page
  }, testInfo) => {
    await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);
    await openSearch(page);

    await expect(page.locator('#location-group-checkboxes input[data-lg-id]')).toHaveCount(3);
    await expect(groupCheckbox(page, 1)).toBeChecked();
    await expect(groupCheckbox(page, 2)).toBeChecked();
    await expect(groupCheckbox(page, 'unassigned')).toBeChecked();
    await expect(page.locator('#location-group-select-all')).toHaveText('Deselect all');

    await groupCheckbox(page, 2).uncheck();
    await expect(visibleChildRows(page)).toHaveCount(5);
    expect(await childIds(page)).toEqual([
      'pc:fresh',
      'manual:manu',
      'pc:unassigned',
      'pc:done',
      'pc:late'
    ]);
    expect(new URL(page.url()).searchParams.getAll('location_group_id')).toEqual(['1']);
    expect(new URL(page.url()).searchParams.get('include_unassigned')).toBe('1');

    await groupCheckbox(page, 1).uncheck();
    await expect(visibleChildRows(page)).toHaveCount(2);
    expect(await childIds(page)).toEqual(['manual:manu', 'pc:unassigned']);
    expect(new URL(page.url()).searchParams.get('include_unassigned')).toBe('1');

    await groupCheckbox(page, 'unassigned').uncheck();
    await expect(visibleChildRows(page)).toHaveCount(0);
    expect(new URL(page.url()).searchParams.get('location_group_id')).toBe('');

    await page.locator('#location-group-select-all').click();
    await expect(visibleChildRows(page)).toHaveCount(7);
    await expect(groupCheckbox(page, 1)).toBeChecked();
    await expect(groupCheckbox(page, 2)).toBeChecked();
    await expect(groupCheckbox(page, 'unassigned')).toBeChecked();
    expect(new URL(page.url()).searchParams.has('location_group_id')).toBe(false);
    expect(new URL(page.url()).searchParams.has('include_unassigned')).toBe(false);

    await page.locator('#location-group-select-all').click();
    await expect(visibleChildRows(page)).toHaveCount(0);
    await expect(page.locator('#location-group-select-all')).toHaveText('Select all');
    expect(new URL(page.url()).searchParams.get('location_group_id')).toBe('');

    await groupCheckbox(page, 'unassigned').check();
    await expect(visibleChildRows(page)).toHaveCount(2);
    expect(await childIds(page)).toEqual(['manual:manu', 'pc:unassigned']);
    expect(new URL(page.url()).searchParams.get('include_unassigned')).toBe('1');
  });

  test('applies URL prefilters for group id, name, and unassigned', async ({
    page
  }, testInfo) => {
    await openBoard(page, testInfo, { query: '?location_group_id=2' });
    await expect(visibleChildRows(page)).toHaveCount(3);
    expect(await childIds(page)).toEqual(['manual:manu', 'pc:edge', 'pc:overdue']);

    await page.unroute('**/v1/**');
    await openBoard(page, testInfo, { query: '?location_group_name=Elementary' });
    await expect(visibleChildRows(page)).toHaveCount(3);
    expect(await childIds(page)).toEqual(['manual:manu', 'pc:edge', 'pc:overdue']);

    await page.unroute('**/v1/**');
    await openBoard(page, testInfo, { query: '?include_unassigned=1' });
    await expect(visibleChildRows(page)).toHaveCount(2);
    expect(await childIds(page)).toEqual(['manual:manu', 'pc:unassigned']);

    await page.unroute('**/v1/**');
    await openBoard(page, testInfo, { query: '?location_group_id=' });
    await expect(visibleChildRows(page)).toHaveCount(0);
    await expect(page.locator('#children-list')).toContainText('No children called yet');
  });

  test('combines search, hide-confirmed, and group filters across a poll', async ({
    page
  }, testInfo) => {
    await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);
    await openSearch(page);

    await page.locator('#search-input').fill('kid');
    await expect(visibleChildRows(page)).toHaveCount(6);

    await toggleHideConfirmed(page);
    await expect(visibleChildRows(page)).toHaveCount(5);

    await groupCheckbox(page, 2).uncheck();
    await expect(visibleChildRows(page)).toHaveCount(3);
    expect(await childIds(page)).toEqual([
      'pc:fresh',
      'pc:unassigned',
      'pc:late'
    ]);

    await pollOnce(page);
    await expect(visibleChildRows(page)).toHaveCount(3);
    await expect(page.locator('#search-input')).toHaveValue('kid');
    await expect(page.locator('#hide-confirmed-toggle')).toBeChecked();
    await expect(groupCheckbox(page, 2)).not.toBeChecked();

    const params = new URL(page.url()).searchParams;
    expect(params.getAll('location_group_id')).toEqual(['1']);
    expect(params.get('include_unassigned')).toBe('1');
  });

  test('adds and removes children between polls', async ({ page }, testInfo) => {
    const api = await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);

    api.addChild(
      makeChild({
        planning_center_id: 'new',
        first_name: 'New',
        last_name: 'Kid',
        security_code: '9999',
        checked_out_at: minutesAgo(0.05)
      })
    );
    await pollOnce(page);
    await expect(visibleChildRows(page)).toHaveCount(8);
    expect((await childIds(page))[0]).toBe('pc:new');

    api.removeChild('pc:edge');
    await pollOnce(page);
    await expect(visibleChildRows(page)).toHaveCount(7);
    await expect(childCard(page, 'pc:edge')).toHaveCount(0);
  });

  test('keeps tied timestamps in a stable order across polls', async ({ page }, testInfo) => {
    const tied = minutesAgo(2);
    const state = createState({
      children: [
        makeChild({ planning_center_id: 'b', first_name: 'B', last_name: 'Kid', checked_out_at: tied }),
        makeChild({ planning_center_id: 'a', first_name: 'A', last_name: 'Kid', checked_out_at: tied }),
        makeChild({ planning_center_id: 'c', first_name: 'C', last_name: 'Kid', checked_out_at: tied })
      ]
    });
    const api = await openBoard(page, testInfo, { state });
    await expect(visibleChildRows(page)).toHaveCount(3);
    const firstOrder = await childIds(page);
    expect(firstOrder).toHaveLength(3);
    expect(new Set(firstOrder).size).toBe(3);

    api.state.children.reverse();
    await pollOnce(page);
    expect(await childIds(page)).toEqual(firstOrder);
  });

  test('keeps focus inside a child row across a poll', async ({ page }, testInfo) => {
    await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);

    await childCheckbox(page, 'pc:fresh').focus();
    expect(await page.evaluate(() => document.activeElement?.dataset?.childId)).toBe(
      'pc:fresh'
    );

    await pollOnce(page);
    expect(await page.evaluate(() => document.activeElement?.dataset?.childId)).toBe(
      'pc:fresh'
    );
  });

  test('shows the current error state on a failed poll and recovers', async ({
    page
  }, testInfo) => {
    const api = await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);

    api.failNextPoll();
    await pollOnce(page);
    await expect(visibleChildRows(page)).toHaveCount(0);
    await expect(page.locator('#children-list')).toContainText(
      'Error loading data. Please try again.'
    );

    await pollOnce(page);
    await expect(visibleChildRows(page)).toHaveCount(7);
  });

  test('sends limit and checked_out_after on the poll request', async ({
    page
  }, testInfo) => {
    const api = await openBoard(page, testInfo, { query: '?checked_out_after=-31m' });
    await expect(visibleChildRows(page)).toHaveCount(7);

    const poll = api.lastPollRequest();
    expect(poll).toBeTruthy();
    const params = new URLSearchParams(poll.search);
    expect(params.get('limit')).toBe('100');
    expect(params.get('checked_out_after')).toBe('-31m');
  });

  test('renders at most 100 children', async ({ page }, testInfo) => {
    const children = Array.from({ length: 101 }, (_, index) =>
      makeChild({
        planning_center_id: `limit-${index}`,
        first_name: 'Limit',
        last_name: `Kid ${index}`,
        checked_out_at: minutesAgo(index / 60)
      })
    );
    await openBoard(page, testInfo, { state: createState({ children }) });
    await expect(visibleChildRows(page)).toHaveCount(100);
  });

  test('escapes child and group content instead of injecting HTML', async ({
    page
  }, testInfo) => {
    const payload = '<img src=x onerror="window.__xss=1">';
    const state = createState({
      groups: [{ id: 1, name: payload }],
      children: [
        makeChild({
          planning_center_id: 'evil',
          first_name: payload,
          last_name: 'Kid',
          security_code: '<b>123</b>',
          location_group_id: 1,
          checked_out_at: minutesAgo(6)
        })
      ]
    });

    await openBoard(page, testInfo, { state });
    await expect(visibleChildRows(page)).toHaveCount(1);
    await expect(childCard(page, 'pc:evil')).toContainText(payload);
    await expect(childCard(page, 'pc:evil')).toContainText('<b>123</b>');
    await expect(childCard(page, 'pc:evil').locator('b')).toHaveCount(0);
    await expect(page.locator('#children-list img[src="x"]')).toHaveCount(0);
    await expect(page.locator('#location-group-checkboxes img[src="x"]')).toHaveCount(0);
    expect(await page.evaluate(() => window.__xss)).toBeUndefined();

    await openOverdueSheet(page);
    await expect(overdueRow(page, 'pc:evil')).toContainText(payload);
    await expect(page.locator('#overdue-sheet-list img[src="x"]')).toHaveCount(0);
    expect(await page.evaluate(() => window.__xss)).toBeUndefined();
  });

  test('still renders the board when location groups fail', async ({
    page
  }, testInfo) => {
    const state = createState({ groupsStatus: 500 });
    await openBoard(page, testInfo, { state });

    await expect(visibleChildRows(page)).toHaveCount(7);
    await expect(page.locator('#location-group-checkboxes input')).toHaveCount(0);
    await openSearch(page);
    await expect(page.locator('#search-input')).toBeVisible();
  });

  test('recovers from malformed and aborted polls', async ({ page }, testInfo) => {
    const api = await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);

    api.failNextPoll('malformed');
    await pollOnce(page);
    await expect(page.locator('#children-list')).toContainText(
      'Error loading data. Please try again.'
    );
    await pollOnce(page);
    await expect(visibleChildRows(page)).toHaveCount(7);

    api.failNextPoll('abort');
    await pollOnce(page);
    await expect(page.locator('#children-list')).toContainText(
      'Error loading data. Please try again.'
    );
    await pollOnce(page);
    await expect(visibleChildRows(page)).toHaveCount(7);
  });

  test('preserves URL window parameters when filters change', async ({
    page
  }, testInfo) => {
    const api = await openBoard(page, testInfo, {
      query: '?checked_out_after=-31m&limit=50'
    });
    await expect(visibleChildRows(page)).toHaveCount(7);

    let params = new URLSearchParams(api.lastPollRequest().search);
    expect(params.get('checked_out_after')).toBe('-31m');
    expect(params.get('limit')).toBe('50');

    await openSearch(page);
    const request = page.waitForRequest((request) =>
      request.url().includes('/v1/checkins/checkouts')
    );
    await groupCheckbox(page, 2).uncheck();
    await request;

    const pageParams = new URL(page.url()).searchParams;
    expect(pageParams.get('checked_out_after')).toBe('-31m');
    expect(pageParams.get('limit')).toBe('50');

    params = new URLSearchParams(api.lastPollRequest().search);
    expect(params.get('checked_out_after')).toBe('-31m');
    expect(params.get('limit')).toBe('50');
  });

  test('uses limit from the URL', async ({ page }, testInfo) => {
    const api = await openBoard(page, testInfo, { query: '?limit=3' });
    await expect(visibleChildRows(page)).toHaveCount(3);
    expect(new URLSearchParams(api.lastPollRequest().search).get('limit')).toBe('3');
  });

  test('expands and collapses search without losing filter state', async ({
    page
  }, testInfo) => {
    await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);

    const toggle = page.locator('#search-toggle-button');
    const input = page.locator('#search-input');
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await expect(input).toBeHidden();

    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    await expect(input).toBeVisible();
    expect(await page.evaluate(() => document.activeElement?.id)).toBe('search-input');

    await input.fill('fresh');
    await expect(visibleChildRows(page)).toHaveCount(1);
    await pollOnce(page);
    await expect(input).toHaveValue('fresh');
    await expect(visibleChildRows(page)).toHaveCount(1);

    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await expect(input).toBeHidden();
    await expect(input).toHaveValue('fresh');
    await expect(visibleChildRows(page)).toHaveCount(1);
  });

  test('keeps the main list scroll position across a poll', async ({
    page
  }, testInfo) => {
    const children = Array.from({ length: 60 }, (_, index) =>
      makeChild({
        planning_center_id: `scroll-${index}`,
        first_name: 'Scroll',
        last_name: `Kid ${index}`,
        checked_out_at: minutesAgo(index / 60)
      })
    );
    const api = await openBoard(page, testInfo, { state: createState({ children }) });
    await expect(visibleChildRows(page)).toHaveCount(60);
    // Flush the initial render's scroll-restoration frame before setting the
    // scroll position; the fake clock is paused, so it would otherwise fire
    // during the next runFor and restore the pre-scroll value.
    await page.clock.runFor(16);

    const list = page.locator('#children-list');
    const metrics = await list.evaluate((element) => ({
      scrollHeight: element.scrollHeight,
      clientHeight: element.clientHeight
    }));
    expect(metrics.scrollHeight).toBeGreaterThan(metrics.clientHeight);

    const before = await list.evaluate((element) => {
      element.scrollTop = 200;
      return element.scrollTop;
    });
    expect(before).toBe(200);

    api.addChild(
      makeChild({
        planning_center_id: 'scroll-new',
        first_name: 'Scroll',
        last_name: 'New',
        checked_out_at: minutesAgo(0.01)
      })
    );
    await pollOnce(page);
    await expect(visibleChildRows(page)).toHaveCount(61);
    await page.clock.runFor(16);
    await expect.poll(() => list.evaluate((element) => element.scrollTop)).toBe(before);
  });
});

test.describe('confirmations', () => {
  test('checks a Planning Center child optimistically and persists it', async ({
    page
  }, testInfo) => {
    const api = await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);

    const release = api.holdConfirmations();
    const patch = page.waitForRequest(
      (request) =>
        request.method() === 'PATCH' &&
        request.url().includes('/checked_out_confirmed')
    );

    await childLabel(page, 'pc:fresh').click();
    await expect(childLabel(page, 'pc:fresh')).toHaveAttribute(
      'data-confirmed-state',
      'confirmed'
    );
    await expect(childCheckbox(page, 'pc:fresh')).toBeChecked();

    const request = await patch;
    expect(new URL(request.url()).pathname).toBe(
      '/v1/checkins/fresh/checked_out_confirmed'
    );
    expect(request.postDataJSON()).toEqual({ confirmed: true });

    release();
    await pollOnce(page);
    await expect(childLabel(page, 'pc:fresh')).toHaveAttribute(
      'data-confirmed-state',
      'confirmed'
    );
    await expect(childCheckbox(page, 'pc:fresh')).toBeChecked();
  });

  test('checks a manual child through the manual endpoint', async ({ page }, testInfo) => {
    await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);

    const patch = page.waitForRequest(
      (request) =>
        request.method() === 'PATCH' &&
        request.url().includes('/checked_out_confirmed')
    );
    await childLabel(page, 'manual:manu').click();
    const request = await patch;

    expect(new URL(request.url()).pathname).toBe(
      '/v1/checkins/manual-checkins/manu/checked_out_confirmed'
    );
    expect(request.postDataJSON()).toEqual({ confirmed: true });
    await expect(childLabel(page, 'manual:manu')).toHaveAttribute(
      'data-confirmed-state',
      'confirmed'
    );
  });

  test('encodes a manual public_id in the confirmation path', async ({
    page
  }, testInfo) => {
    const state = createState({
      children: [
        makeManualChild({
          public_id: 'a b/c?d',
          first_name: 'Weird',
          last_name: 'Manual',
          checked_out_at: minutesAgo(1)
        })
      ]
    });
    await openBoard(page, testInfo, { state });
    await expect(visibleChildRows(page)).toHaveCount(1);

    const patch = page.waitForRequest(
      (request) =>
        request.method() === 'PATCH' &&
        request.url().includes('/checked_out_confirmed')
    );
    await childLabel(page, 'manual:a b/c?d').click();
    const request = await patch;

    expect(new URL(request.url()).pathname).toBe(
      '/v1/checkins/manual-checkins/a%20b%2Fc%3Fd/checked_out_confirmed'
    );
    expect(request.postDataJSON()).toEqual({ confirmed: true });
  });

  test('reverts manual and unknown-source confirmations without a request', async ({
    page
  }, testInfo) => {
    const state = createState({
      children: [
        makeManualChild({
          public_id: '',
          first_name: 'No',
          last_name: 'Id',
          checked_out_at: minutesAgo(1)
        }),
        makeChild({
          source: 'other',
          planning_center_id: 'other-1',
          first_name: 'Other',
          last_name: 'Source',
          checked_out_at: minutesAgo(2)
        })
      ]
    });
    const api = await openBoard(page, testInfo, { state });
    await expect(visibleChildRows(page)).toHaveCount(2);

    await page
      .locator('label:has(input[aria-label="Mark No Id as confirmed"])')
      .click();
    await page
      .locator('label:has(input[aria-label="Mark Other Source as confirmed"])')
      .click();

    expect(api.confirmationRequests()).toHaveLength(0);
    await expect(
      page.getByRole('checkbox', { name: 'Mark No Id as confirmed' })
    ).not.toBeChecked();
    await expect(
      page.getByRole('checkbox', { name: 'Mark Other Source as confirmed' })
    ).not.toBeChecked();
  });

  test('reverts an optimistic check when the PATCH fails', async ({ page }, testInfo) => {
    const api = await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);

    api.setConfirmStatus(500);
    const response = page.waitForResponse(
      (response) =>
        response.request().method() === 'PATCH' &&
        response.url().includes('/checked_out_confirmed')
    );
    await childLabel(page, 'pc:fresh').click();
    await response;

    await expect(childLabel(page, 'pc:fresh')).toHaveAttribute(
      'data-confirmed-state',
      'unconfirmed'
    );
    await expect(childCheckbox(page, 'pc:fresh')).not.toBeChecked();

    const uncheckResponse = page.waitForResponse(
      (response) =>
        response.request().method() === 'PATCH' &&
        response.url().includes('/checked_out_confirmed')
    );
    await childLabel(page, 'pc:done').click();
    await uncheckResponse;

    await expect(childLabel(page, 'pc:done')).toHaveAttribute(
      'data-confirmed-state',
      'confirmed'
    );
    await expect(childCheckbox(page, 'pc:done')).toBeChecked();
  });

  test('unchecks a confirmed child and sends confirmed false', async ({
    page
  }, testInfo) => {
    await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);

    const patch = page.waitForRequest(
      (request) =>
        request.method() === 'PATCH' &&
        request.url().includes('/checked_out_confirmed')
    );
    await childLabel(page, 'pc:done').click();
    const request = await patch;

    expect(request.postDataJSON()).toEqual({ confirmed: false });
    await expect(childLabel(page, 'pc:done')).toHaveAttribute(
      'data-confirmed-state',
      'unconfirmed'
    );
    await expect(childCheckbox(page, 'pc:done')).not.toBeChecked();
  });

  test('reconciles a confirmation made on another device', async ({ page }, testInfo) => {
    const api = await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);

    api.setConfirmed('pc:edge', true);
    await pollOnce(page);
    await expect(childLabel(page, 'pc:edge')).toHaveAttribute(
      'data-confirmed-state',
      'confirmed'
    );
    await expect(childCheckbox(page, 'pc:edge')).toBeChecked();
  });

  test('holds the local override for 15s and then reconciles', async ({
    page
  }, testInfo) => {
    const api = await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);
    api.setApplyConfirmations(false);

    const patch = page.waitForRequest(
      (request) =>
        request.method() === 'PATCH' &&
        request.url().includes('/checked_out_confirmed')
    );
    await childLabel(page, 'pc:fresh').click();
    await patch;
    await expect(childCheckbox(page, 'pc:fresh')).toBeChecked();

    await page.clock.runFor(3000);
    await expect(childCheckbox(page, 'pc:fresh')).toBeChecked();

    await page.clock.runFor(12_000);
    await expect(childCheckbox(page, 'pc:fresh')).toBeChecked();

    await page.clock.runFor(6000);
    await expect(childCheckbox(page, 'pc:fresh')).not.toBeChecked();
  });

  test('sends one PATCH when the confirm control is double-clicked', async ({
    page
  }, testInfo) => {
    const api = await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);

    const release = api.holdConfirmations();
    const response = page.waitForResponse(
      (response) =>
        response.request().method() === 'PATCH' &&
        response.url().includes('/checked_out_confirmed')
    );

    await childLabel(page, 'pc:fresh').click();
    await childLabel(page, 'pc:fresh').click();
    await expect.poll(() => api.confirmationRequests().length).toBe(1);

    release();
    await response;
    await pollOnce(page);
    await expect(childCheckbox(page, 'pc:fresh')).toBeChecked();
    await expect(childLabel(page, 'pc:fresh')).toHaveAttribute(
      'data-confirmed-state',
      'confirmed'
    );
  });
});

test.describe('overdue', () => {
  test('shows the badge and sheet with newest-first rows', async ({ page }, testInfo) => {
    await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);

    const badge = page.locator('#overdue-badge');
    await expect(badge).toBeVisible();
    await expect(badge).toHaveText('2 overdue. Tap to view');

    await openOverdueSheet(page);
    await expect(page.locator('#overdue-sheet-count')).toHaveText('2 overdue');
    await expect(page.locator('#overdue-sheet-title')).toHaveText('Overdue >5 min');
    expect(await overdueIds(page)).toEqual(['pc:overdue', 'pc:late']);
    await expect(overdueRow(page, 'pc:overdue')).toContainText('Elementary');
    await expect(overdueRow(page, 'pc:late')).toContainText('Nursery');
  });

  test('closes by button and restores focus', async ({ page }, testInfo) => {
    await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);
    await openOverdueSheet(page);

    expect(await page.evaluate(() => document.activeElement?.id)).toBe('overdue-sheet');

    await page.locator('#overdue-sheet-close').click();
    await expect(page.locator('#overdue-sheet')).toHaveAttribute('aria-hidden', 'true');
    await expect(page.locator('#overdue-sheet')).toHaveAttribute('inert', '');
    expect(await page.evaluate(() => document.activeElement?.id)).toBe('overdue-badge');
  });

  test('closes by Escape and backdrop', async ({ page }, testInfo) => {
    await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);

    await openOverdueSheet(page);
    await page.keyboard.press('Escape');
    await expect(page.locator('#overdue-sheet')).toHaveAttribute('aria-hidden', 'true');

    await openOverdueSheet(page);
    await page.locator('#overdue-sheet-backdrop').click({ position: { x: 5, y: 5 } });
    await expect(page.locator('#overdue-sheet')).toHaveAttribute('aria-hidden', 'true');
  });

  test('retains a confirmed row until the sheet closes', async ({ page }, testInfo) => {
    await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);
    await openOverdueSheet(page);

    const patch = page.waitForRequest(
      (request) =>
        request.method() === 'PATCH' &&
        request.url().includes('/checked_out_confirmed')
    );
    await overdueLabel(page, 'pc:overdue').click();
    await patch;

    await expect(overdueRow(page, 'pc:overdue')).toHaveCount(1);
    await expect(overdueLabel(page, 'pc:overdue')).toHaveAttribute(
      'data-confirmed-state',
      'confirmed'
    );
    await expect(page.locator('#overdue-sheet-count')).toHaveText('1 overdue');

    await pollOnce(page);
    await expect(overdueRow(page, 'pc:overdue')).toHaveCount(1);

    await page.locator('#overdue-sheet-close').click();
    await openOverdueSheet(page);
    await expect(overdueRow(page, 'pc:overdue')).toHaveCount(0);
    expect(await overdueIds(page)).toEqual(['pc:late']);
    await expect(page.locator('#overdue-sheet-count')).toHaveText('1 overdue');
  });

  test('does not insert a newly overdue child while the sheet is open', async ({
    page
  }, testInfo) => {
    const api = await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);
    await openOverdueSheet(page);
    expect(await overdueIds(page)).toEqual(['pc:overdue', 'pc:late']);

    api.addChild(
      makeChild({
        planning_center_id: 'new-overdue',
        first_name: 'New',
        last_name: 'Overdue',
        checked_out_at: minutesAgo(6.5),
        location_group_id: 2
      })
    );
    await pollOnce(page);
    expect(await overdueIds(page)).toEqual(['pc:overdue', 'pc:late']);

    await page.locator('#overdue-sheet-close').click();
    await openOverdueSheet(page);
    expect(await overdueIds(page)).toEqual(['pc:overdue', 'pc:new-overdue', 'pc:late']);
  });

  test('keeps the open sheet populated when a poll fails', async ({ page }, testInfo) => {
    const api = await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);
    await openOverdueSheet(page);

    api.failNextPoll();
    await pollOnce(page);
    await expect(visibleChildRows(page)).toHaveCount(0);
    expect(await overdueIds(page)).toEqual(['pc:overdue', 'pc:late']);
  });

  test('hides the badge when nothing is overdue', async ({ page }, testInfo) => {
    const state = createState({
      children: defaultChildren().filter(
        (child) => child.planning_center_id !== 'overdue' && child.planning_center_id !== 'late'
      )
    });
    await openBoard(page, testInfo, { state });
    await expect(visibleChildRows(page)).toHaveCount(5);
    await expect(page.locator('#overdue-badge')).toBeHidden();
  });

  test('keeps the badge and sheet unfiltered when groups are hidden', async ({
    page
  }, testInfo) => {
    await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);
    await openSearch(page);

    await page.locator('#location-group-select-all').click();
    await expect(visibleChildRows(page)).toHaveCount(0);
    await expect(page.locator('#overdue-badge')).toHaveText('2 overdue. Tap to view');

    await openOverdueSheet(page);
    expect(await overdueIds(page)).toEqual(['pc:overdue', 'pc:late']);
  });

  test('empties the open sheet on a successful empty poll', async ({
    page
  }, testInfo) => {
    const api = await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);
    await openOverdueSheet(page);
    expect(await overdueIds(page)).toEqual(['pc:overdue', 'pc:late']);

    api.setChildren([]);
    await pollOnce(page);

    await expect(visibleChildRows(page)).toHaveCount(0);
    expect(await overdueIds(page)).toEqual([]);
    await expect(page.locator('#overdue-sheet-list')).toContainText(
      'No overdue checkouts'
    );
    await expect(page.locator('#overdue-sheet-count')).toHaveText('No overdue');
    await expect(page.locator('#overdue-badge')).toBeHidden();
  });

  test('excludes confirmed overdue children and zeroes the badge', async ({
    page
  }, testInfo) => {
    const children = defaultChildren();
    children.find(
      (child) => child.planning_center_id === 'overdue'
    ).checked_out_confirmed_at = minutesAgo(1);
    await openBoard(page, testInfo, { state: createState({ children }) });

    await expect(visibleChildRows(page)).toHaveCount(7);
    await expect(page.locator('#overdue-badge')).toHaveText('1 overdue. Tap to view');
    await openOverdueSheet(page);
    expect(await overdueIds(page)).toEqual(['pc:late']);

    const patch = page.waitForRequest(
      (request) =>
        request.method() === 'PATCH' &&
        request.url().includes('/checked_out_confirmed')
    );
    await overdueLabel(page, 'pc:late').click();
    await patch;

    await expect(page.locator('#overdue-sheet-count')).toHaveText('No overdue');
    await expect(overdueRow(page, 'pc:late')).toHaveCount(1);

    await page.locator('#overdue-sheet-close').click();
    await expect(page.locator('#overdue-badge')).toBeHidden();
  });

  test('treats exactly five minutes as overdue', async ({ page }, testInfo) => {
    const state = createState({
      children: [
        makeChild({
          planning_center_id: 'five',
          first_name: 'Five',
          last_name: 'Minutes',
          checked_out_at: minutesAgo(5),
          location_group_id: 1
        }),
        makeChild({
          planning_center_id: 'almost',
          first_name: 'Almost',
          last_name: 'Five',
          checked_out_at: minutesAgo(4 + 59 / 60),
          location_group_id: 1
        })
      ]
    });

    await openBoard(page, testInfo, { state });
    await expect(visibleChildRows(page)).toHaveCount(2);
    await expect(page.locator('#overdue-badge')).toHaveText('1 overdue. Tap to view');

    await openOverdueSheet(page);
    expect(await overdueIds(page)).toEqual(['pc:five']);
  });

  test('keeps focus in the open sheet across a poll', async ({ page }, testInfo) => {
    await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);
    await openOverdueSheet(page);

    await overdueCheckbox(page, 'pc:overdue').focus();
    expect(await page.evaluate(() => document.activeElement?.dataset?.childId)).toBe(
      'pc:overdue'
    );

    await pollOnce(page);
    expect(await page.evaluate(() => document.activeElement?.dataset?.childId)).toBe(
      'pc:overdue'
    );
  });

  test('updates the drawer group label when location groups load late', async ({
    page
  }, testInfo) => {
    const state = createState();
    let releaseGroups;
    state.groupsGate = () =>
      new Promise((resolve) => {
        releaseGroups = resolve;
      });

    await openBoard(page, testInfo, { state });
    await expect(visibleChildRows(page)).toHaveCount(7);
    await expect.poll(() => typeof releaseGroups).toBe('function');

    await openOverdueSheet(page);
    await expect(overdueRow(page, 'pc:overdue')).toContainText('Group 2');

    releaseGroups();
    await expect(
      page.locator('#location-group-checkboxes input[data-lg-id]')
    ).toHaveCount(3);
    await page.clock.runFor(1000);
    await expect(overdueRow(page, 'pc:overdue')).toContainText('Elementary');
  });
});

test.describe('mobile viewport', () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test('keeps the overdue sheet and close button inside the viewport', async ({
    page
  }, testInfo) => {
    await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);
    await openOverdueSheet(page);

    await page.waitForFunction(() => {
      const sheet = document.getElementById('overdue-sheet');
      const rect = sheet.getBoundingClientRect();
      return Math.abs(rect.bottom - window.innerHeight) <= 1;
    });

    const viewport = page.viewportSize();
    const sheet = await page.locator('#overdue-sheet').boundingBox();
    const close = await page.locator('#overdue-sheet-close').boundingBox();
    expect(sheet).toBeTruthy();
    expect(close).toBeTruthy();
    expect(sheet.y).toBeGreaterThanOrEqual(0);
    expect(sheet.y + sheet.height).toBeLessThanOrEqual(viewport.height + 1);
    expect(close.y).toBeGreaterThanOrEqual(0);
    expect(close.y + close.height).toBeLessThanOrEqual(viewport.height + 1);
  });
});

test.describe('timezone', () => {
  test.use({ timezoneId: 'America/New_York' });

  test('shows the header in the browser timezone', async ({ page }, testInfo) => {
    await openBoard(page, testInfo);
    await expect(visibleChildRows(page)).toHaveCount(7);
    await expect(page.locator('#current-time')).toHaveText('07:00');
  });
});
