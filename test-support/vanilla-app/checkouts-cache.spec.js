// Conditional-poll behavior is a vanilla-page implementation detail: the
// conformance contract does not require a replacement page to send
// If-None-Match or reuse the previous DOM nodes. Exercise it in a real browser
// without imposing it on every replacement implementation.
import { test, expect } from '@playwright/test';
import { BASE_TIME } from '../conformance/fixtures.js';
import { createState, installFakeApi } from '../conformance/fake-api.js';

test('revalidates an unchanged poll without re-rendering rows', async ({
  page
}, testInfo) => {
  await installFakeApi(page, createState());
  await page.clock.install({ time: BASE_TIME });
  await page.clock.pauseAt(BASE_TIME);
  await page.goto(testInfo.project.metadata.pagePath);
  await expect(page.locator('#children-list .child-time')).toHaveCount(7);

  await page.evaluate(() => {
    const row = document.querySelector('#children-list > div');
    if (!row) throw new Error('no child row to track');
    window.__childBeforePoll = row;
  });

  const responsePromise = page.waitForResponse(
    (response) =>
      response.url().includes('/v1/checkins/checkouts') && response.status() === 304
  );
  await page.clock.runFor(3000);
  const response = await responsePromise;

  expect(response.request().headers()['if-none-match']).toBeTruthy();
  await expect(page.locator('#children-list .child-time')).toHaveCount(7);
  expect(
    await page.evaluate(
      () =>
        window.__childBeforePoll ===
        document.querySelector('#children-list > div')
    )
  ).toBe(true);
});
