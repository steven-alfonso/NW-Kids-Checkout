const { defineConfig, devices } = require('@playwright/test');

const vanillaPage =
  process.env.CHECKOUTS_PAGE || '/static/pages/checkoutsv1/checkouts.html';
const nextPage = process.env.CHECKOUTS_PAGE_NEXT;

const projects = [
  {
    name: 'vanilla',
    metadata: { pagePath: vanillaPage },
    use: { ...devices['Desktop Chrome'] }
  }
];

if (nextPage) {
  projects.push({
    name: 'next',
    metadata: { pagePath: nextPage },
    use: { ...devices['Desktop Chrome'] }
  });
}

module.exports = defineConfig({
  testDir: './test-support/conformance',
  testMatch: '**/*.spec.js',
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
  projects
});
