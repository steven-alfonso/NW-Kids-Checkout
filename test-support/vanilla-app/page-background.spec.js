// The fixed bottom artwork must not chase a keyboard-shrunken viewport. The
// checkouts page has its own regression in checkouts-a11y.spec.js; this covers
// the login page, the other page with a control that opens the keyboard.
import { test, expect } from '@playwright/test';

test('keeps the login artwork out of the mobile keyboard viewport', async ({
  page
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/static/pages/login/index.html');
  const background = page.locator('#page-background');
  await expect(background).toHaveCSS('opacity', '1');

  // Focus lands before the keyboard animates in, so the artwork hides before
  // a viewport-resizing browser can drag it upward.
  await page.locator('#password').focus();
  await expect(background).toHaveCSS('opacity', '0');

  // Simulate a browser that shrinks the layout viewport for the keyboard.
  await page.setViewportSize({ width: 390, height: 500 });
  await expect(background).toHaveCSS('opacity', '0');

  await page.setViewportSize({ width: 390, height: 844 });
  await page.locator('#password').evaluate((input) => input.blur());
  await expect(background).toHaveCSS('opacity', '1');
});
