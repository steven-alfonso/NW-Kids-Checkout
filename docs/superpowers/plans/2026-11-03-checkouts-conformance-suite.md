# Checkouts Conformance Suite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Add a black-box Playwright suite that passes against the current vanilla checkouts page and any future replacement without editing the spec.

**Architecture:** A static server serves the real `internal/web/static` page and assets; Playwright routes mock the `/v1` API; `page.clock` makes the 1s tick and 3s poll deterministic. The spec reads a page path from `CHECKOUTS_PAGE`, so a second project can run the same tests against a rewritten page.

**Tech Stack:** Playwright Test 1.64.1, Node 22+, the existing Vitest/jsdom suite (unchanged), GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-11-03-checkouts-conformance-suite-design.md`

## Global Constraints

- The suite must not read implementation source, call `window.__test`, or import page modules.
- The only allowed hooks are the ones listed in the spec's Contract section.
- Current behavior is the contract; do not change production behavior.
- No production `data-testid` attributes.
- `make test` stays fast. Conformance runs via `make test-conformance` and CI.
- Pin `@playwright/test` to `1.64.1`.
- Commit `package-lock.json`; it is currently gitignored.

## Review Focus

- A failed poll with the overdue sheet open must leave the sheet's rows intact.
- A confirmation made elsewhere must reconcile on the next poll without the local 15s override fighting it forever.
- Tied, future, and missing `checked_out_at` values must not reorder rows unpredictably or crash rendering.
- Group filters must keep manual rows visible, honor unassigned, and handle the empty `?location_group_id=` sentinel.
- Focus inside a child row must survive a poll, and focus must return to the badge when the sheet closes.

---

### Task 1: Playwright foundation

**Files:**
- Modify: `.gitignore`
- Modify: `package.json`
- Create: `package-lock.json` (from `npm install`)
- Create: `playwright.config.js`
- Create: `test-support/conformance/server.mjs`
- Create: `test-support/conformance/smoke.spec.js`

**Interfaces:**
- `playwright.config.js` exports a project whose `metadata.pagePath` is `process.env.CHECKOUTS_PAGE || '/static/pages/checkoutsv1/checkouts.html'`.
- `server.mjs` listens on `127.0.0.1:4173` and serves `/static/*` from `internal/web/static`.

- [x] **Step 1: Write the failing smoke test**

```js
import { test, expect } from '@playwright/test';

test('serves the checkouts page', async ({ page }, testInfo) => {
  await page.goto(testInfo.project.metadata.pagePath);
  await expect(page).toHaveTitle('NW Kids Checkout');
  await expect(page.locator('#children-list')).toContainText('Loading children data');
});
```

- [x] **Step 2: Run it to verify it fails**

Run: `npx playwright test test-support/conformance/smoke.spec.js`
Expected: FAIL because no config/server/dependency exists.

- [x] **Step 3: Add Playwright, lockfile, static server, and config**

- Remove `package-lock.json` from `.gitignore`.
- Add `"test:conformance": "playwright test"` and `"@playwright/test": "1.64.1"` to `package.json`.
- Run `npm install` to produce `package-lock.json`.
- Implement `server.mjs` with MIME types, path-traversal protection, `Cache-Control: no-store`, `/static/*`, and a 503 JSON body for unhandled `/v1/*`.
- Implement `playwright.config.js` with `baseURL: http://127.0.0.1:4173`, `timezoneId: 'UTC'`, `webServer`, `workers: 1`, and the `vanilla` project.

- [x] **Step 4: Run the smoke test**

Run: `npm run test:conformance -- test-support/conformance/smoke.spec.js`
Expected: PASS.

### Task 2: Fixtures, fake API, selectors, boot tests

**Files:**
- Create: `test-support/conformance/fixtures.js`
- Create: `test-support/conformance/fake-api.js`
- Create: `test-support/conformance/selectors.js`
- Create: `test-support/conformance/checkouts.spec.js`

**Interfaces:**
- `fixtures.js` exports `BASE_TIME`, `GROUPS`, `minutesAgo(minutes)`, `makeChild(overrides)`, `defaultChildren()`.
- `fake-api.js` exports `installFakeApi(page, state)` returning helpers `addChild`, `removeChild`, `setConfirmed`, `failNextPoll`, `setConfirmStatus`, `requests`.
- `selectors.js` exports `childCard(page, id)`, `childIds(page)`, `childCheckbox(page, id)`, `timeText(page, id)`.

- [x] **Step 1: Write the failing boot test**

```js
test('renders children newest-first with manual and confirmed states', async ({ page }, testInfo) => {
  const api = await installFakeApi(page, { children: defaultChildren() });
  await page.clock.install({ time: BASE_TIME });
  await page.goto(testInfo.project.metadata.pagePath);

  await expect(page.locator('#children-list .child-time')).toHaveCount(7);
  expect(await childIds(page)).toEqual([
    'pc:fresh', 'manual:manu', 'pc:unassigned', 'pc:done',
    'pc:edge', 'pc:overdue', 'pc:late',
  ]);
  await expect(childCard(page, 'manual:manu')).toContainText('---');
  await expect(childCard(page, 'manual:manu').locator('img[alt="Manual checkin"]')).toBeVisible();
  await expect(childCard(page, 'pc:done').locator('[data-confirmed-label]')).toHaveAttribute('data-confirmed-state', 'confirmed');
});
```

- [x] **Step 2: Run it to verify it fails**

Run: `npm run test:conformance -- --grep "renders children"`
Expected: FAIL because the fixtures and helpers do not exist.

- [x] **Step 3: Implement fixtures, fake API, and selectors**

Fixtures use a fixed `BASE_TIME` and children at 0.1, 0.2, 1, 2, 3.9, 6, and 7.9 minutes ago. The fake API dispatches by pathname, records requests, applies group filters when present, updates confirmations by default, and returns `{checkins, manual_checkins}`.

- [x] **Step 4: Run the boot test**

Run: `npm run test:conformance -- --grep "renders children"`
Expected: PASS.

### Task 3: Clock and time tests

- [x] Write tests for `#current-time` at `12:00`, advancing 60s to `12:01`, and time pills crossing minute boundaries.
- [x] Add the future-timestamp test (`0 min ago`) and the missing-timestamp test (not rendered).
- [x] Run the group: `npm run test:conformance -- --grep "clock|timestamp"`.

### Task 4: Filter and URL tests

- [x] Write tests for search by name and security code, no-match state, and clearing.
- [x] Write tests for hide-confirmed and its empty state.
- [x] Write tests for group checkboxes, manual visibility, unassigned, select-all/deselect-all, and URL replacement.
- [x] Write URL prefilter tests for `location_group_id`, `location_group_name`, `include_unassigned`, and `?location_group_id=`.
- [x] Run the group: `npm run test:conformance -- --grep "filter|search|URL"`.

### Task 5: Polling tests

- [x] Write tests for add/remove between polls, stable tied ordering, focus survival, error state, recovery, `limit=100`, and `checked_out_after` forwarding.
- [x] Run the group: `npm run test:conformance -- --grep "poll"`.

### Task 6: Confirmation tests

- [x] Write tests for PC and manual PATCH paths/bodies, optimistic state, failure revert, poll reconciliation, 15s override expiry, uncheck, and cross-device confirmation.
- [x] Run the group: `npm run test:conformance -- --grep "confirm"`.

### Task 7: Overdue tests

- [x] Write tests for badge count/text, sheet rows and group labels, close by button/backdrop/Escape, focus return and focus stability, `aria-hidden`/`inert`, confirmation retention, new-overdue suppression while open, failed-poll retention, successful-empty clearing, and the 5-minute boundary. Body scroll lock and visualViewport geometry are intentionally excluded.
- [x] Run the group: `npm run test:conformance -- --grep "overdue"`.

### Task 8: CI, Makefile, docs, known behaviors

**Files:**
- Modify: `.github/workflows/ci.yml`
- Modify: `Makefile`
- Modify: `README.md`
- Modify: `AGENTS.md`
- Create: `test-support/conformance/README.md`
- Create: `test-support/conformance/KNOWN-BEHAVIORS.md`

- [x] Add the CI `conformance` job with `npm ci`, `npx playwright install --with-deps chromium`, and `npm run test:conformance`.
- [x] Add `make test-conformance`.
- [x] Document the contract, commands, and two-project migration workflow.
- [x] Run `npm run test:conformance` and confirm the full suite is green.

### Task 9: Portability verification

- [x] Run `CHECKOUTS_PAGE_NEXT=/static/pages/checkoutsv1/checkouts.html npx playwright test` and confirm both `vanilla` and `next` projects pass.
- [x] Confirm `CHECKOUTS_PAGE_NEXT` unset still runs only `vanilla`.
- [x] Record the actual parity run against the rewritten page in the migration PR.
