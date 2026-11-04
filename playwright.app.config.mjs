import { defineConfig, devices } from '@playwright/test';

const pagePath =
  process.env.CHECKOUTS_PAGE || '/static/pages/checkoutsv1/checkouts.html';

export default defineConfig({
  testDir: './test-support/app-contract',
  testMatch: '**/*.spec.mjs',
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
      name: 'app',
      metadata: { pagePath },
      use: { ...devices['Desktop Chrome'] }
    }
  ]
});
