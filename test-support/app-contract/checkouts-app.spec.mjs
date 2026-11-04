import { test, expect } from '@playwright/test';
import {
  BASE_TIME,
  defaultChildren,
  makeChild,
  minutesAgo
} from '../conformance/fixtures.js';
import { createState, installFakeApi } from '../conformance/fake-api.js';

async function openBoard(page, testInfo, { state = createState(), query = '' } = {}) {
  const api = await installFakeApi(page, state);
  await page.clock.install({ time: BASE_TIME });
  await page.clock.pauseAt(BASE_TIME);
  await page.goto(`${testInfo.project.metadata.pagePath}${query}`);
  await expect(page.locator('#children-list .child-time')).toHaveCount(
    state.children.filter((child) => child.checked_out_at).length
  );
  return api;
}

test('locks and restores body scroll while the overdue sheet is open', async ({
  page
}, testInfo) => {
  await openBoard(page, testInfo);
  await expect(page.locator('#overdue-badge')).toBeVisible();

  await page.locator('#overdue-badge').click();
  await expect(page.locator('#overdue-sheet')).toHaveAttribute('aria-hidden', 'false');
  expect(await page.evaluate(() => getComputedStyle(document.body).position)).toBe(
    'fixed'
  );

  await page.locator('#overdue-sheet-close').click();
  await expect(page.locator('#overdue-sheet')).toHaveAttribute('aria-hidden', 'true');
  expect(await page.evaluate(() => getComputedStyle(document.body).position)).not.toBe(
    'fixed'
  );
});

test('inerts the sheet background while open and restores it on close', async ({
  page
}, testInfo) => {
  await openBoard(page, testInfo);
  const badge = page.locator('#overdue-badge');
  await badge.click();
  await expect(page.locator('#overdue-sheet')).toHaveAttribute(
    'aria-hidden',
    'false'
  );

  const readInert = () =>
    page.evaluate(() => ({
      header: document.querySelector('header').hasAttribute('inert'),
      board: document
        .getElementById('children-list')
        .parentElement.hasAttribute('inert'),
      badge: document.getElementById('overdue-badge').hasAttribute('inert'),
      sheet: document.getElementById('overdue-sheet').hasAttribute('inert')
    }));

  expect(await readInert()).toEqual({
    header: true,
    board: true,
    badge: true,
    sheet: false
  });

  // The sheet and its controls must stay interactive while the background
  // behind it is inert.
  await page.locator('#overdue-sheet-close').focus();
  expect(await page.evaluate(() => document.activeElement?.id)).toBe(
    'overdue-sheet-close'
  );
  await page.locator('#overdue-sheet-close').click();
  await expect(page.locator('#overdue-sheet')).toHaveAttribute(
    'aria-hidden',
    'true'
  );
  await expect(badge).toBeFocused();

  expect(await readInert()).toEqual({
    header: false,
    board: false,
    badge: false,
    sheet: true
  });
});

test('removes collapsed search controls from the tab order', async ({
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
  await openBoard(page, testInfo, { state });

  const badge = page.locator('#overdue-badge');
  await expect(badge).toBeVisible();
  await badge.click();
  await expect(page.locator('#overdue-sheet')).toHaveAttribute(
    'aria-hidden',
    'false'
  );

  await page.locator('#overdue-sheet-list [data-confirmed-label]').click();
  await expect(badge).toBeHidden();

  await page.locator('#overdue-sheet-close').click();
  await expect(page.locator('#overdue-sheet')).toHaveAttribute(
    'aria-hidden',
    'true'
  );
  await expect(page.locator('#search-toggle-button')).toBeFocused();
});

test('keeps drawer rows through the close transition and refreshes on reopen', async ({
  page
}, testInfo) => {
  const api = await openBoard(page, testInfo);
  await expect(page.locator('#overdue-badge')).toBeVisible();

  await page.locator('#overdue-badge').click();
  await expect(page.locator('#overdue-sheet')).toHaveAttribute('aria-hidden', 'false');
  await expect(page.locator('#overdue-sheet-list .child-time')).toHaveCount(2);

  await page.locator('#overdue-sheet-close').click();
  await expect(page.locator('#overdue-sheet')).toHaveAttribute('aria-hidden', 'true');
  // The sheet is still sliding out; its rows must not be cleared underneath it.
  await expect(page.locator('#overdue-sheet-list .child-time')).toHaveCount(2);

  api.setConfirmed('pc:overdue', true);
  const requestPromise = page.waitForRequest((request) =>
    request.url().includes('/v1/checkins/checkouts')
  );
  await page.clock.runFor(3000);
  const request = await requestPromise;
  await request.response();

  await page.locator('#overdue-badge').click();
  await expect(page.locator('#overdue-sheet')).toHaveAttribute('aria-hidden', 'false');
  await expect(page.locator('#overdue-sheet-list .child-time')).toHaveCount(1);
  await expect(page.locator('#overdue-sheet-list')).toContainText('Late Kid');
});

test('shows only the no-JS message when scripting is disabled', async ({
  browser
}, testInfo) => {
  const context = await browser.newContext({
    javaScriptEnabled: false,
    baseURL: testInfo.project.use.baseURL
  });
  const page = await context.newPage();
  await page.goto(testInfo.project.metadata.pagePath);

  // Locator visibility relies on page scripting; read the computed state
  // through the test harness instead so this works with JS disabled.
  const state = await page.evaluate(() => {
    const message = document.querySelector('noscript div');
    const loading = document.getElementById('checkouts-loading');
    return {
      message: message?.textContent.trim(),
      messageHeight: message?.getBoundingClientRect().height ?? 0,
      loadingDisplay: loading ? getComputedStyle(loading).display : null
    };
  });
  expect(state.message).toBe('This page requires JavaScript.');
  expect(state.messageHeight).toBeGreaterThan(0);
  expect(state.loadingDisplay).toBe('none');

  await context.close();
});

test('moves the server-rendered menu into the kebab menu exactly once', async ({
  page
}, testInfo) => {
  await page.route(
    /\/static\/pages\/checkoutsv1\/checkouts\.html(\?.*)?$/,
    async (route) => {
      const response = await route.fetch();
      const body = (await response.text()).replace(
        '<!-- kebab-menu-links -->',
        '<a id="admin-link" href="/admin">Admin</a>'
      );
      await route.fulfill({ response, body });
    }
  );

  await openBoard(page, testInfo);
  await expect(page.locator('#kebab-menu #admin-link')).toHaveCount(1);

  await page.locator('#menu-button').click();
  await expect(page.locator('#kebab-menu')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.locator('#kebab-menu')).toBeHidden();
});

test('hides the overdue badge when a poll fails', async ({ page }, testInfo) => {
  const api = await openBoard(page, testInfo);
  await expect(page.locator('#overdue-badge')).toHaveText('2 overdue. Tap to view');

  const requestPromise = page.waitForRequest((request) =>
    request.url().includes('/v1/checkins/checkouts')
  );
  api.failNextPoll();
  await page.clock.runFor(3000);
  const request = await requestPromise;
  await request.response();

  await expect(page.locator('#children-list')).toContainText(
    'Error loading data. Please try again.'
  );
  await expect(page.locator('#overdue-badge')).toBeHidden();
});

test('leaves the board unfiltered when Select all is clicked with no groups', async ({
  page
}, testInfo) => {
  await openBoard(page, testInfo, { state: createState({ groupsStatus: 500 }) });
  await page.locator('#search-toggle-button').click();
  await expect(page.locator('#search-input')).toBeVisible();
  await expect(page.locator('#location-group-checkboxes input')).toHaveCount(0);
  await expect(page.locator('#location-group-select-all')).toHaveText('Select all');

  await page.locator('#location-group-select-all').click();
  await expect(page.locator('#children-list .child-time')).toHaveCount(7);
  expect(new URL(page.url()).searchParams.has('location_group_id')).toBe(false);
});

test('accepts dev preview rows and stops polling', async ({ page }, testInfo) => {
  await openBoard(page, testInfo);
  const preview = defaultChildren().slice(0, 3);

  await page.evaluate((children) => {
    window.dispatchEvent(
      new CustomEvent('checkouts-preview', { detail: children })
    );
  }, preview);

  await expect(page.locator('#children-list .child-time')).toHaveCount(3);
  await page.clock.runFor(3100);
  await expect(page.locator('#children-list .child-time')).toHaveCount(3);
});
