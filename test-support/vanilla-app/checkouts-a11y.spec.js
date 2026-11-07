// Behaviors the conformance suite deliberately does not pin because they are
// implementation details of the vanilla page. jsdom cannot model either inert
// or native Tab, so these run against real Chromium instead.
import { test, expect } from '@playwright/test';
import { BASE_TIME } from '../conformance/fixtures.js';
import { createState, installFakeApi } from '../conformance/fake-api.js';

async function openBoard(page, testInfo) {
  await installFakeApi(page, createState());
  await page.clock.install({ time: BASE_TIME });
  await page.clock.pauseAt(BASE_TIME);
  await page.goto(testInfo.project.metadata.pagePath);
  await expect(page.locator('#children-list .child-time')).toHaveCount(7);
}

function activeElement(page) {
  return page.evaluate(() => ({
    id: document.activeElement?.id,
    childId: document.activeElement?.dataset?.childId,
    inSheet: Boolean(document.activeElement?.closest('#overdue-sheet')),
    inSearch: Boolean(document.activeElement?.closest('#search-controls'))
  }));
}

test('traps Tab in the modal overdue sheet and inerts the page behind it', async ({
  page
}, testInfo) => {
  await openBoard(page, testInfo);
  const badge = page.locator('#overdue-badge');
  const sheet = page.locator('#overdue-sheet');

  await badge.click();
  await expect(sheet).toHaveAttribute('aria-hidden', 'false');
  await expect(sheet).toHaveAttribute('aria-modal', 'true');

  // Opening moves focus off the occluded badge and into the dialog.
  expect((await activeElement(page)).id).toBe('overdue-sheet');

  await page.keyboard.press('Tab');
  expect((await activeElement(page)).id).toBe('overdue-sheet-close');

  // One inert attribute removes the background from the tab ring and the
  // accessibility tree, not just from view.
  expect(
    await page.evaluate(() => ({
      header: document.getElementById('page-header').hasAttribute('inert'),
      board: document.getElementById('board-content').hasAttribute('inert'),
      badge: document.getElementById('overdue-badge').hasAttribute('inert')
    }))
  ).toEqual({ header: true, board: true, badge: true });

  // Shift+Tab from the first control wraps to the last row checkbox.
  await page.keyboard.press('Shift+Tab');
  expect(await activeElement(page)).toMatchObject({
    inSheet: true,
    childId: 'pc:late'
  });

  // Tab from the last control wraps back to the first.
  await page.keyboard.press('Tab');
  expect((await activeElement(page)).id).toBe('overdue-sheet-close');

  await page.keyboard.press('Escape');
  await expect(sheet).toHaveAttribute('aria-hidden', 'true');
  await expect(badge).toBeFocused();
  expect(
    await page.evaluate(() =>
      document.getElementById('page-header').hasAttribute('inert')
    )
  ).toBe(false);
});

test('keeps the collapsed search panel out of the tab order', async ({
  page
}, testInfo) => {
  await openBoard(page, testInfo);
  const toggle = page.locator('#search-toggle-button');
  const controls = page.locator('#search-controls');

  // The panel ships collapsed: height/opacity hide it visually only.
  await expect(controls).toHaveAttribute('inert', '');
  await expect(controls).toHaveAttribute('aria-hidden', 'true');

  await toggle.focus();
  await page.keyboard.press('Tab');
  expect((await activeElement(page)).inSearch).toBe(false);

  await toggle.click();
  await expect(controls).not.toHaveAttribute('inert', '');
  await expect(controls).toHaveAttribute('aria-hidden', 'false');
  await expect(page.locator('#search-input')).toBeFocused();

  // Collapsing around a focused control hands focus back to the toggle instead
  // of letting inert drop it on <body>.
  await toggle.click();
  await expect(toggle).toBeFocused();
  await expect(controls).toHaveAttribute('inert', '');
  await expect(controls).toHaveAttribute('aria-hidden', 'true');
});

test('keeps the decorative background out of the mobile keyboard viewport', async ({
  page
}, testInfo) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await openBoard(page, testInfo);
  const background = page.locator('#page-background');
  await expect(background).toHaveCSS('opacity', '1');

  // Focus lands before the keyboard animates in, so the artwork fades before
  // a viewport-resizing browser can drag it upward.
  await page.locator('#search-toggle-button').click();
  await expect(page.locator('#search-input')).toBeFocused();
  await expect(background).toHaveCSS('opacity', '0');

  // Simulate a browser that shrinks the layout viewport for the keyboard.
  await page.setViewportSize({ width: 390, height: 500 });
  await expect(background).toHaveCSS('opacity', '0');

  await page.setViewportSize({ width: 390, height: 844 });
  await page.locator('#search-input').evaluate((input) => input.blur());
  await expect(background).toHaveCSS('opacity', '1');
});
