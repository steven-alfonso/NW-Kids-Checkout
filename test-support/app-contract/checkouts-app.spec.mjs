import { test, expect } from '@playwright/test';
import fs from 'node:fs';
import path from 'node:path';
import {
  BASE_TIME,
  defaultChildren,
  makeChild,
  minutesAgo
} from '../conformance/fixtures.js';
import { createState, installFakeApi } from '../conformance/fake-api.js';
import {
  childCard,
  groupCheckbox,
  visibleChildRows
} from '../conformance/selectors.js';

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

async function pollOnce(page) {
  const requestPromise = page.waitForRequest((request) =>
    request.url().includes('/v1/checkins/checkouts')
  );
  await page.clock.runFor(3000);
  const request = await requestPromise;
  await request.response();
}

// Adds a route in front of the fake API so a single poll can be held open.
// fallback() keeps the shared fake API responsible for the payload.
async function holdCheckoutPolls(page) {
  let releaseGate;
  const gate = new Promise((resolve) => {
    releaseGate = resolve;
  });
  let holdNext = false;
  let seen = 0;
  let held = 0;

  await page.route('**/v1/checkins/checkouts**', async (route) => {
    seen += 1;
    if (!holdNext) {
      await route.fallback();
      return;
    }
    holdNext = false;
    held += 1;
    await gate;
    try {
      await route.fallback();
    } catch {
      // A filter refresh aborts the held request; there is nothing to fulfill.
    }
  });

  return {
    arm() {
      holdNext = true;
    },
    release() {
      releaseGate();
    },
    seen: () => seen,
    held: () => held
  };
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

test('keeps the board unfiltered when Select all is clicked without groups', async ({
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

test('loads the real dev preview fixture through the dynamic import', async ({
  page
}, testInfo) => {
  const previewData = fs.readFileSync(
    path.resolve(process.cwd(), 'internal/web/dev-assets/preview-data.js'),
    'utf8'
  );
  await page.route('**/static/dev/preview-data.js', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'text/javascript',
      body: previewData
    })
  );

  await openBoard(page, testInfo);
  await page.evaluate(() => {
    window.dispatchEvent(new CustomEvent('checkouts-preview'));
  });

  await expect(page.locator('#children-list .child-time')).toHaveCount(10);
  await page.clock.runFor(3100);
  await expect(page.locator('#children-list .child-time')).toHaveCount(10);
});

test('does not cancel a poll that is still in flight', async ({ page }, testInfo) => {
  const api = await openBoard(page, testInfo);
  const polls = await holdCheckoutPolls(page);

  polls.arm();
  const requestPromise = page.waitForRequest((request) =>
    request.url().includes('/v1/checkins/checkouts')
  );
  await page.clock.runFor(3000);
  const request = await requestPromise;
  await expect.poll(polls.held).toBe(1);

  api.state.children.find(
    (child) => child.planning_center_id === 'fresh'
  ).first_name = 'Slow';
  await page.clock.runFor(3000);
  expect(polls.seen()).toBe(1);

  polls.release();
  const response = await request.response();
  expect(response?.status()).toBe(200);
  await expect(childCard(page, 'pc:fresh')).toContainText('Slow Kid');
});

test('aborts an in-flight poll when filters change', async ({ page }, testInfo) => {
  await openBoard(page, testInfo);
  const polls = await holdCheckoutPolls(page);

  polls.arm();
  const requestPromise = page.waitForRequest((request) =>
    request.url().includes('/v1/checkins/checkouts')
  );
  await page.clock.runFor(3000);
  await requestPromise;
  await expect.poll(polls.held).toBe(1);

  await page.locator('#search-toggle-button').click();
  await expect(page.locator('#location-group-checkboxes input[data-lg-id]')).toHaveCount(
    3
  );
  await groupCheckbox(page, 2).uncheck();

  await expect(visibleChildRows(page)).toHaveCount(5);
  await expect(page.locator('#children-list')).not.toContainText(
    'Error loading data'
  );
  expect(polls.seen()).toBe(2);

  // Playwright cannot report a failure for a request held in a route handler
  // after the page aborts it, so assert the observable outcome: releasing the
  // stale response leaves the filtered board intact.
  polls.release();
  await page.clock.runFor(100);
  await expect(visibleChildRows(page)).toHaveCount(5);
});

test('keeps the collapsed search panel out of the tab order', async ({
  page
}, testInfo) => {
  await openBoard(page, testInfo);
  const controls = page.locator('#search-controls');
  await expect(controls).toHaveAttribute('inert', '');
  await expect(controls).toHaveAttribute('aria-hidden', 'true');

  await page.locator('#search-toggle-button').focus();
  for (let index = 0; index < 8; index += 1) {
    await page.keyboard.press('Tab');
    expect(
      await page.evaluate(() =>
        document.getElementById('search-controls').contains(document.activeElement)
      )
    ).toBe(false);
  }

  await page.locator('#search-toggle-button').click();
  await expect(controls).not.toHaveAttribute('inert', '');
  await expect(controls).toHaveAttribute('aria-hidden', 'false');
  await expect(page.locator('#search-input')).toBeFocused();
});

test('traps Tab inside the open overdue sheet and inerts the board', async ({
  page
}, testInfo) => {
  await openBoard(page, testInfo);
  await page.locator('#overdue-badge').click();
  await expect(page.locator('#overdue-sheet')).toHaveAttribute(
    'aria-hidden',
    'false'
  );
  expect(await page.evaluate(() => document.querySelector('header').inert)).toBe(
    true
  );
  expect(
    await page.evaluate(
      () => document.getElementById('children-list').closest('[inert]') !== null
    )
  ).toBe(true);

  const lastControl = page
    .locator('#overdue-sheet-list input.child-confirmed-checkbox')
    .last();
  await lastControl.focus();
  await page.keyboard.press('Tab');
  expect(
    await page.evaluate(() =>
      document.getElementById('overdue-sheet').contains(document.activeElement)
    )
  ).toBe(true);

  await page.locator('#overdue-sheet-close').focus();
  await page.keyboard.press('Shift+Tab');
  expect(
    await page.evaluate(() =>
      document.getElementById('overdue-sheet').contains(document.activeElement)
    )
  ).toBe(true);
});

test('restores focus to the search toggle when the badge is hidden', async ({
  page
}, testInfo) => {
  const state = createState({
    children: [
      makeChild({
        planning_center_id: 'only-overdue',
        first_name: 'Only',
        last_name: 'Late',
        checked_out_at: minutesAgo(6),
        location_group_id: 1
      }),
      makeChild({
        planning_center_id: 'still-fresh',
        first_name: 'Still',
        last_name: 'Fresh',
        checked_out_at: minutesAgo(0.5),
        location_group_id: 1
      })
    ]
  });
  await openBoard(page, testInfo, { state });
  await expect(page.locator('#overdue-badge')).toHaveText(
    '1 overdue. Tap to view'
  );

  await page.locator('#overdue-badge').click();
  await expect(page.locator('#overdue-sheet')).toHaveAttribute(
    'aria-hidden',
    'false'
  );

  const patch = page.waitForRequest(
    (request) =>
      request.method() === 'PATCH' &&
      request.url().includes('/checked_out_confirmed')
  );
  await page
    .locator(
      '#overdue-sheet-list [data-confirmed-label]:has(input[data-child-id="pc:only-overdue"])'
    )
    .click();
  await patch;
  await expect(page.locator('#overdue-badge')).toBeHidden();

  await page.locator('#overdue-sheet-close').click();
  await expect(page.locator('#overdue-sheet')).toHaveAttribute(
    'aria-hidden',
    'true'
  );
  await expect(page.locator('#search-toggle-button')).toBeFocused();
});

test('flashes a newly polled child card and clears after the window', async ({
  page
}, testInfo) => {
  const api = await openBoard(page, testInfo);
  api.addChild(
    makeChild({
      planning_center_id: 'flash-new',
      first_name: 'Flash',
      last_name: 'Kid',
      checked_out_at: minutesAgo(0.5),
      location_group_id: 1
    })
  );
  await pollOnce(page);

  const card = childCard(page, 'pc:flash-new');
  await expect(card).toHaveClass(/child-card-flash/);
  expect(
    await card.evaluate((element) => getComputedStyle(element).animationName)
  ).toBe('new-child-flash');

  await page.clock.runFor(4000);
  await expect(card).not.toHaveClass(/child-card-flash/);
});

test('jiggles the overdue badge only when the overdue count increases', async ({
  page
}, testInfo) => {
  const api = await openBoard(page, testInfo);
  const badge = page.locator('#overdue-badge');

  await expect(badge).toHaveClass(/overdue-badge-jiggle/);
  expect(
    await badge.evaluate((element) => getComputedStyle(element).animationName)
  ).toBe('overdue-badge-jiggle');

  await page.clock.runFor(1000);
  await expect(badge).not.toHaveClass(/overdue-badge-jiggle/);

  api.addChild(
    makeChild({
      planning_center_id: 'extra-overdue',
      first_name: 'Extra',
      last_name: 'Late',
      checked_out_at: minutesAgo(10),
      location_group_id: 1
    })
  );
  await pollOnce(page);
  await expect(badge).toHaveText('3 overdue. Tap to view');
  await expect(badge).toHaveClass(/overdue-badge-jiggle/);
});

test('re-measures the expanded search panel when groups load late', async ({
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

  releaseGroups();
  await expect(
    page.locator('#location-group-checkboxes input[data-lg-id]')
  ).toHaveCount(3);
  await expect
    .poll(() =>
      page.locator('#search-controls').evaluate((element) => {
        const last = element.querySelector('input[data-lg-id="unassigned"]');
        return (
          element.clientHeight >= element.scrollHeight - 1 &&
          last.getBoundingClientRect().bottom <=
            element.getBoundingClientRect().bottom + 1
        );
      })
    )
    .toBe(true);
});

test('shows only the no-JS notice when scripting is disabled', async ({
  browser
}, testInfo) => {
  const context = await browser.newContext({ javaScriptEnabled: false });
  const page = await context.newPage();
  await page.goto(testInfo.project.metadata.pagePath);
  const fallback = await page.evaluate(() => ({
    text: document.body.innerText.trim(),
    loadingDisplay: getComputedStyle(
      document.getElementById('checkouts-loading')
    ).display
  }));
  expect(fallback.text).toBe('This page requires JavaScript.');
  expect(fallback.loadingDisplay).toBe('none');
  await context.close();
});
