// Real-browser checks that the cross-implementation conformance suite cannot
// own: focus trapping, inert enforcement, and tab order. jsdom reflects the
// attributes but implements neither inert nor native Tab, so these behaviors
// need a browser even though they are vanilla-implementation details.
const { defineConfig, devices } = require('@playwright/test');

const pagePath =
  process.env.CHECKOUTS_PAGE || '/static/pages/checkoutsv1/checkouts.html';

module.exports = defineConfig({
  testDir: './test-support/vanilla-app',
  testMatch: '**/*.spec.js',
  // Separate from the conformance run's test-results/ so CI can upload both.
  outputDir: 'test-results-app',
  timeout: 20_000,
  expect: { timeout: 5_000 },
  fullyParallel: false,
  workers: 1,
  reporter: process.env.CI ? 'github' : 'list',
  use: {
    baseURL: 'http://127.0.0.1:4173',
    timezoneId: 'UTC',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'off'
  },
  webServer: {
    command: 'node test-support/conformance/server.mjs',
    url: 'http://127.0.0.1:4173/health',
    reuseExistingServer: !process.env.CI,
    timeout: 10_000
  },
  projects: [
    {
      name: 'vanilla-app',
      metadata: { pagePath },
      use: { ...devices['Desktop Chrome'] }
    }
  ]
});
