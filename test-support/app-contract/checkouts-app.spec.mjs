import { test, expect } from '@playwright/test';
import { BASE_TIME, defaultChildren } from '../conformance/fixtures.js';
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
  await expect(page.locator('#checkouts-menu-source')).toHaveCount(0);

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

test('keeps Select all inert when location groups fail', async ({
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
