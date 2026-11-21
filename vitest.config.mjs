import { defineConfig, mergeConfig } from 'vitest/config';
import viteConfig from './vite.config.mjs';

// Reuse the Vue plugin/aliases from the build config so component tests can
// import .vue files, while keeping Playwright suites out of the Vitest run.
export default mergeConfig(
  viteConfig,
  defineConfig({
    test: {
      environment: 'jsdom',
      exclude: [
        '**/node_modules/**',
        '**/.git/**',
        '**/.worktrees/**',
        '**/worktrees/**',
        '.worktrees/**',
        'test-support/conformance/**',
        'test-support/app-contract/**',
        'test-results/**'
      ]
    }
  })
);
