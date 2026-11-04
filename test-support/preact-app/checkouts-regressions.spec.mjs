import { test, expect } from '@playwright/test';
import {
  BASE_TIME,
  makeChild,
  minutesAgo
} from '../conformance/fixtures.js';
import { createState, installFakeApi } from '../conformance/fake-api.js';

async function openBoard(
  page,
  testInfo,
  { state = createState(), query = '', count = 7 } = {}
) {
  const api = await installFakeApi(page, state);
  await page.clock.install({ time: BASE_TIME });
  await page.clock.pauseAt(BASE_TIME);
  await page.goto(`${testInfo.project.metadata.pagePath}${query}`);
  await expect(page.locator('#children-list .child-time')).toHaveCount(count);
  return api;
}

async function pollOnce(page) {
  const requestPromise = page.waitForRequest((request) =>
    request.url().includes('/v1/checkins/checkouts')
  );
  await page.clock.runFor(3000);
  const request = await requestPromise;
  await request.response();
}

test('marks the overdue sheet as modal, traps Tab, and inerts the background', async ({
  page
}, testInfo) => {
  await openBoard(page, testInfo);
  const badge = page.locator('#overdue-badge');
  await badge.click();
  await expect(page.locator('#overdue-sheet')).toHaveAttribute(
    'aria-hidden',
    'false'
  );
  await expect(page.locator('#overdue-sheet')).toHaveAttribute(
    'aria-modal',
    'true'
  );

  expect(await page.evaluate(() => document.activeElement?.id)).toBe(
    'overdue-sheet'
  );
  expect(
    await page.evaluate(() => ({
      header: document.querySelector('header').hasAttribute('inert'),
      board: document
        .getElementById('children-list')
        .parentElement.hasAttribute('inert'),
      badge: document.getElementById('overdue-badge').hasAttribute('inert')
    }))
  ).toEqual({ header: true, board: true, badge: true });

  await page.keyboard.press('Tab');
  expect(await page.evaluate(() => document.activeElement?.id)).toBe(
    'overdue-sheet-close'
  );

  await page.keyboard.press('Shift+Tab');
  expect(
    await page.evaluate(() => ({
      inSheet: document
        .getElementById('overdue-sheet')
        .contains(document.activeElement),
      childId: document.activeElement?.dataset?.childId
    }))
  ).toEqual({ inSheet: true, childId: 'pc:late' });

  await page.keyboard.press('Tab');
  expect(await page.evaluate(() => document.activeElement?.id)).toBe(
    'overdue-sheet-close'
  );

  await page.keyboard.press('Escape');
  await expect(page.locator('#overdue-sheet')).toHaveAttribute(
    'aria-hidden',
    'true'
  );
  await expect(badge).toBeFocused();
  expect(
    await page.evaluate(() => ({
      header: document.querySelector('header').hasAttribute('inert'),
      board: document
        .getElementById('children-list')
        .parentElement.hasAttribute('inert'),
      badge: document.getElementById('overdue-badge').hasAttribute('inert')
    }))
  ).toEqual({ header: false, board: false, badge: false });
});

test('focuses a visible fallback when the opener badge is hidden on close', async ({
  page
}, testInfo) => {
  const state = createState({
    children: [
      makeChild({
        planning_center_id: 'only-overdue',
        checked_out_at: minutesAgo(6)
      })
    ]
  });
  await openBoard(page, testInfo, { state, count: 1 });

  const badge = page.locator('#overdue-badge');
  await badge.click();
  await page.locator('#overdue-sheet-list [data-confirmed-label]').click();
  await expect(badge).toBeHidden();

  await page.locator('#overdue-sheet-close').click();
  await expect(page.locator('#overdue-sheet')).toHaveAttribute(
    'aria-hidden',
    'true'
  );
  await expect(page.locator('#search-toggle-button')).toBeFocused();
});

test('flashes newly polled rows and clears the flash after four seconds', async ({
  page
}, testInfo) => {
  const api = await openBoard(page, testInfo);
  api.addChild(
    makeChild({
      planning_center_id: 'flash-new',
      first_name: 'Flash',
      last_name: 'New',
      checked_out_at: minutesAgo(0.01)
    })
  );
  await pollOnce(page);

  const row = page.locator(
    '#children-list > div:has([data-child-id="pc:flash-new"])'
  );
  await expect(row).toHaveCount(1);
  await expect(row).toHaveClass(/child-card-flash/);

  await page.clock.runFor(4000);
  await expect(row).not.toHaveClass(/child-card-flash/);
});

test('jiggles the badge when the overdue count rises', async ({
  page
}, testInfo) => {
  const state = createState({
    children: [
      makeChild({
        planning_center_id: 'not-yet-overdue',
        checked_out_at: minutesAgo(0.5)
      })
    ]
  });
  const api = await openBoard(page, testInfo, { state, count: 1 });
  const badge = page.locator('#overdue-badge');
  await expect(badge).toBeHidden();

  api.state.children[0].checked_out_at = minutesAgo(6);
  await pollOnce(page);

  await expect(badge).toBeVisible();
  await expect(badge).toHaveText('1 overdue. Tap to view');
  await expect(badge).toHaveClass(/overdue-badge-jiggle/);
});

test('re-measures the expanded search when location groups arrive late', async ({
  page
}, testInfo) => {
  const state = createState();
  let releaseGroups;
  state.groupsGate = () =>
    new Promise((resolve) => {
      releaseGroups = resolve;
    });

  await openBoard(page, testInfo, { state });
  await expect.poll(() => typeof releaseGroups).toBe('function');

  await page.locator('#search-toggle-button').click();
  await expect(page.locator('#search-input')).toBeVisible();
  await expect(page.locator('#location-group-checkboxes input')).toHaveCount(0);

  releaseGroups();
  const groupInputs = page.locator('#location-group-checkboxes input');
  await expect(groupInputs).toHaveCount(3);
  await expect(
    page.locator('#location-group-checkboxes input[data-lg-id="unassigned"]')
  ).toBeVisible();

  await expect
    .poll(() =>
      page.locator('#search-controls').evaluate((element) => {
        const height = Number.parseFloat(element.style.height);
        return Number.isFinite(height) && height + 1 >= element.scrollHeight;
      })
    )
    .toBe(true);
});

test('removes collapsed search controls from the tab order and accessibility tree', async ({
  page
}, testInfo) => {
  await openBoard(page, testInfo);
  const controls = page.locator('#search-controls');
  const input = page.locator('#search-input');
  const toggle = page.locator('#search-toggle-button');

  await expect(controls).toHaveAttribute('inert', '');
  await expect(controls).toHaveAttribute('aria-hidden', 'true');
  await expect(input).toBeHidden();

  await toggle.focus();
  await page.keyboard.press('Tab');
  expect(
    await page.evaluate(() =>
      document
        .getElementById('search-controls')
        .contains(document.activeElement)
    )
  ).toBe(false);

  await toggle.click();
  await expect(controls).not.toHaveAttribute('inert', '');
  await expect(controls).toHaveAttribute('aria-hidden', 'false');
  await expect(input).toBeVisible();
  await expect(input).toBeFocused();

  await page.keyboard.press('Tab');
  expect(await page.evaluate(() => document.activeElement?.id)).toBe(
    'hide-confirmed-toggle'
  );

  await toggle.click();
  await expect(controls).toHaveAttribute('inert', '');
  await expect(controls).toHaveAttribute('aria-hidden', 'true');
  await expect(input).toBeHidden();
});

test('restores the body scroll position after closing the sheet', async ({
  page
}, testInfo) => {
  await page.setViewportSize({ width: 390, height: 600 });
  await openBoard(page, testInfo);

  await page.evaluate(() => window.scrollTo(0, 140));
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBeGreaterThan(
    0
  );
  const before = await page.evaluate(() => window.scrollY);

  await page.locator('#overdue-badge').click();
  await expect(page.locator('#overdue-sheet')).toHaveAttribute(
    'aria-hidden',
    'false'
  );
  expect(await page.evaluate(() => getComputedStyle(document.body).position)).toBe(
    'fixed'
  );

  await page.locator('#overdue-sheet-close').click();
  await expect(page.locator('#overdue-sheet')).toHaveAttribute(
    'aria-hidden',
    'true'
  );
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(before);
});

test('lets an in-flight poll finish instead of aborting it on the next tick', async ({
  page
}, testInfo) => {
  const api = await openBoard(page, testInfo);
  const checkoutRequests = () =>
    api.requests.filter((request) => request.path === '/v1/checkins/checkouts')
      .length;
  const before = checkoutRequests();

  let held = false;
  let release;
  let routeHits = 0;
  await page.route('**/v1/checkins/checkouts/**', async (route) => {
    routeHits += 1;
    if (!held) {
      held = true;
      await new Promise((resolve) => {
        release = resolve;
      });
    }
    await route.fallback();
  });

  api.addChild(
    makeChild({
      planning_center_id: 'slow-poll',
      first_name: 'Slow',
      last_name: 'Poll',
      checked_out_at: minutesAgo(0.01)
    })
  );

  await page.clock.runFor(3000);
  await expect.poll(() => held).toBe(true);
  await page.clock.runFor(3000);
  await page.waitForTimeout(200);
  expect(routeHits).toBe(1);

  release();
  await expect(
    page.locator('#children-list > div:has([data-child-id="pc:slow-poll"])')
  ).toHaveCount(1);
  expect(checkoutRequests()).toBe(before + 1);
});
