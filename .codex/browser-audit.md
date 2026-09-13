# Browser Test Audit — NW Kids Checkout
Date: 2026-09-13
Branch: alpine-checkouts-poc

## Current Coverage
- vitest jsdom: 15 files, 202 tests — covers logic (filter, overdue, confirm, etc.) but NOT browser engine (defer, CSS, inert, layout, scroll lock). Missed the `sheetOverdue` defer race.
- e2e/ab-validate: single A/B parity harness for checkouts only, requires Brave at hardcoded path, 2 apiservers, manual DB cp+restart, AB_PASSWORD. Not run in CI. No coverage for login/home/guest/admin.
- login/index.html: 0 tests (was gap)
- No real-browser smoke in CI

## Fixes Applied (this PR)
1. Defer race fixed: checkouts.html alpine defer + board sync, JS adds immediate Alpine check.
2. New portable harness e2e/browser-smoke.mjs: real Chromium via playwright-core, http static server, mocks /v1/*, checks 5 pages for pageerror/console error (especially Alpine Expression Error / sheetOverdue / checkoutsBoard), asserts selectors exist and __checkoutsBoard.sheetOverdue is array. Runs via `npm run test:browser` in <5s, no Go server needed.
3. ab-validate made portable: resolveBrowserLaunch() fallback to bundled chromium (no Brave required), uses os.tmpdir(), AB_BRAVE env.
4. package.json: added test:browser, e2e:smoke, test:e2e (unit + browser).

## Verification
- npm test: 15 files 202 passed
- npm run test:browser: 5/5 smoke pages passed (home, login, checkouts with __checkoutsBoard true, guest-checkin, manual-checkins)
- go test ./internal/web/static: PASS

## Remaining Gaps (not blocking)
- No admin pages in smoke (6 more html: metrics, locations, etc.) — jsdom covers them, but could add to PAGES.
- No visual/interaction tests (search toggle, sheet open) in real browser — covered in jsdom Alpine harness (loadBoardPage) with 51 checkouts tests, but smoke only checks boot.
- No CI browser install step yet — add `npx playwright install chromium` in CI and run `npm run test:e2e`.

## Sufficiency Verdict
Sufficient for this PR to prevent regression of the reported bug, and minimal viable for CI. Recommend expanding PAGES to all 15 html and adding `test:e2e` to Makefile `test` target and CI workflow.
