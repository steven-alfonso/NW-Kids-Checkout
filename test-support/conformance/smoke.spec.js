import { test, expect } from '@playwright/test';

test('serves the checkouts page', async ({ page }, testInfo) => {
  await page.goto(testInfo.project.metadata.pagePath);
  await expect(page).toHaveTitle('NW Kids Checkout');
  await expect(page.locator('#children-list')).toBeVisible();
});
