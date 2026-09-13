# Alpine Checkouts POC — Review Findings & Fix Instructions

Branch: `alpine-checkouts-poc` (0bb64d5) vs `main`
Generated: 2026-09-13
Source: code-review of 10-file diff (+1622/-1870) — primary change is `checkouts.js` imperative+morphdom → Alpine `checkoutsBoardData` + keyed `x-for`. See `e2e/ab-validate/checkouts.mjs` harness too.

## How to use this file

- Each finding has a checkbox. Mark `- [x]` when done and verified.
- Findings are grouped into batches; each batch is dispatched to a subagent/worktree in parallel.
- **LLM instructions** per finding are prescriptive: what to change, exact files/lines, and how to verify.
- Do not batch unrelated fixes in one commit. One finding (or tightly-coupled group) = one commit.
- After fixing, run the verification command listed, then `make test` (or `npm test` for JS) before marking complete.
- If a finding is intentionally deferred, add `// deferred: reason` next to the checkbox and open an issue.
- Keep behavior parity with `main` unless the finding explicitly says to diverge (e.g., error-preserves-data).

---

## Batch A — Reactivity & Confirm Correctness (P0) — `checkouts.js`/`checkouts.html`

Handles findings 1–4. Worktree: `alpine-checkouts-poc` main checkout (no extra worktree needed, single branch). Coordinate via `internal/web/static/pages/checkoutsv1/checkouts.js`.

- [x] **A1 [critical] Map/Set mutations not reactive — confirm UI goes stale**
  File: `internal/web/static/pages/checkoutsv1/checkouts.js:289,379,440-450,701,710,724` + `checkouts.html:294,367`
  Problem: `confirmationOverrides: new Map()`, `confirmingIds: new Set()`, `overdueRetainedIds: new Set()` mutated in-place via `.set/.add/.delete`. Alpine 3 tracks property-reference changes, not Map/Set contents. Consequences: `:disabled="confirmingIds.has(...)"` never disables during PATCH, `:checked="isConfirmed(child)"` doesn't re-run on override add/expire, `sweepOverrides` deletes with no reassignment and `tick()` ignores return so TTL expiry is invisible until unrelated primitive changes. `flashIds`/`knownIds` are safe (reassigned at 530,535,537,453).
  LLM fix:
  1. Replace in-place mutations with copy-on-write reassignment for all three collections. Example pattern:
     ```js
     // before: this.confirmingIds.add(id)
     // after:
     const next = new Set(this.confirmingIds); next.add(id); this.confirmingIds = next;
     // same for Map: new Map(this.confirmationOverrides).set(id, {confirmed, timestamp})
     // and delete: new Map(...).delete(id)
     ```
     Or introduce a `stateVersion` counter bumped on every mutation and read inside `isConfirmed`/`pillClass`/`overdueChildren` so Alpine tracks it.
  2. Ensure `isConfirmed` reads from the new Map reference and that `tick`/`sweepOverrides` triggers reactivity.
  3. Verify: add/extend `checkouts.test.js` with: (a) set override then assert `:checked` flips, (b) wait TTL+1 then assert `isConfirmed` flips back without unrelated change, (c) `confirmingIds.add` then assert `input:disabled === true`.
  4. Run: `npm test -- internal/web/static/pages/checkoutsv1/checkouts.test.js`

- [x] **A2 [high] Confirm toggle races, allows double-PATCH**
  File: `internal/web/static/pages/checkoutsv1/checkouts.js:683-727`
  Problem: Guard depends on non-reactive `confirmingIds` (see A1). Second click before fetch resolves sees stale `false` and fires second PATCH with opposite `confirmed`. Last-write-wins, can invert vs server. Old code used sync `dataset.confirming` block.
  LLM fix:
  1. Apply A1 first.
  2. Add synchronous guard outside Alpine reactivity or make DOM disable synchronous: `event.target.disabled = true` immediately at top of `onConfirmToggle` before `await fetch`, or maintain a module-local `Set` outside the Alpine proxy (e.g., `const inflight = new Set()` at file top) checked synchronously.
  3. Optionally serialize per-child: if `inflight.has(id)` return early; do not queue opposite value.
  4. Verify: new test `boardWith` → mock `fetch` delayed 50ms → double `onConfirmToggle` → assert `fetch` called once, second call ignored, `confirmingIds` (or inflight) contains id until resolve.
  5. Run: `npm test`

- [x] **A3 [high] Checkbox rollback via direct DOM fights Alpine**
  File: `internal/web/static/pages/checkoutsv1/checkouts.js:687,706,723`
  Problem: `checkbox.checked = previous` mutates DOM owned by `:checked`. Next Alpine flush overwrites it; rollback flickers or never sticks.
  LLM fix:
  1. Remove all direct `checkbox.checked = ...` writes.
  2. On failure: `delete override` via reassignment (A1 pattern) + bump version; rely on `:checked="isConfirmed(child)"` to recompute. Do not touch DOM. On `!endpoint` early return, same.
  3. Keep `event` param only for reading `checked` via `event.target.checked` at entry, not for writing.
  4. Verify: test PATCH failure path → assert override removed and `:checked` returns to `previous` value (check `board.isConfirmed(child) === previous`), no direct DOM assertion.
  5. Run: `npm test`

- [x] **A4 [high] Fetch error wipes board on transient blip**
  File: `internal/web/static/pages/checkoutsv1/checkouts.js:558-562`
  Current: `catch { this.children=[]; this.loadError=true }`. Clears cards/scroll/overdue on one failed 3s poll, then restores. Wall flickers board→error→board, worst during outage. Old code did same, but rewrite is chance to fix.
  LLM fix:
  1. Change catch to preserve `this.children`: remove `this.children = []`. Set `this.loadError = true` and optionally `this.lastErrorAt = Date.now()` + `this.consecutiveErrors++`. On success, reset `consecutiveErrors=0` and `loadError=false`.
  2. Update `visibleChildren`/`overdueChildren` to keep showing last good data while `loadError` true (they already derive from `children`, so preserving `children` suffices). Ensure `refreshOverdue()` still called from error path or not — decide: keep badge from last good data (don't call refresh that would zero it).
  3. Update test at `internal/web/static/pages/checkoutsv1/checkouts.test.js:365` which currently encodes wipe — change expectation to `expect(board.children.length).toBe(prevLen)` and `board.loadError === true`.
  4. Verify: mock `fetch` to reject once then resolve → assert children preserved across failure, error div shows, next poll restores.
  5. Run: `npm test`

---

## Batch B — Polling, Lifecycle & Init Ordering (P1)

- [x] **B1 [medium] Polling has no visibility pause/backoff, dead AbortController**
  File: `internal/web/static/pages/checkoutsv1/checkouts.js:408-415,493,498-499,555-560`
  Problem: `setInterval(fetch,3000)+setInterval(tick,1000)` run in background tabs; every failure `console.error` spams. `fetchBlocked` short-circuits before `abort()` reachable (dead code).
  LLM fix:
  1. Add `document.addEventListener('visibilitychange', () => { if (document.hidden) pause else resume })` — on hidden, `clearInterval(pollFetchId)` and on visible, `fetchChildrenData()` + restart interval. Alternatively skip fetch when `document.hidden` inside `fetchChildrenData`.
  2. Add exponential backoff: `consecutiveErrors` counter, `backoffMs = min(30000, 3000 * 2^errors)` and skip/delay next fetch accordingly, or use `setTimeout` chain instead of `setInterval`.
  3. Make AbortController reachable: either remove `fetchBlocked` guard and instead `if (fetchController) fetchController.abort()` unconditionally at top, or document throttle intent and remove controller. Prefer abort-previous pattern (no dropped polls).
  4. Throttle `console.error` to once per backoff window or behind `DEBUG`.
  5. Verify: manual test — open tab, switch away, assert no fetches in Network panel; kill API, assert backoff spacing. Add unit test for `consecutiveErrors` logic if extracted to helper.
  6. Run: `npm test`, manual visibility check.

- [x] **B2 [medium] `previewChildren` permanently kills polling**
  File: `internal/web/static/pages/checkoutsv1/checkouts.js:808-815` + `internal/web/dev-assets/preview.js:9`
  Problem: sets `fetchBlocked=true` forever. `loadPreviewData()` globally reachable, can silently stop live prod board with zero UI indication.
  LLM fix:
  1. Save prior `fetchBlocked` value and provide unblock: either `this._prevBlocked = this.fetchBlocked; this.fetchBlocked=true` + add `unpreview()` that restores, or auto-unblock after N seconds (`setTimeout(()=> this.fetchBlocked=false, 30000)`), or allow `fetchChildrenData({force:true})` to ignore block.
  2. Surface UI indication: set `this.previewMode = true` and show a small banner/badge when true (check `checkouts.html` for a place, e.g., near clock).
  3. Update `internal/web/static/pages/checkoutsv1/preview.test.js:19` — keep asserting blocked, but also assert that a second `fetchChildrenData()` is no-op while blocked and succeeds after unblock.
  4. Verify: call `previewChildren` then assert `fetchBlocked===true`, wait or call unblock, assert next fetch succeeds.
  5. Run: `npm test`

- [x] **B3 [medium] Script order fragile: Alpine `defer` + checkouts sync**
  File: `internal/web/static/pages/checkoutsv1/checkouts.html:379-380` + `checkouts.js:826-832,867-872`
  Problem: relies on `alpine:init` firing after sync `checkouts.js` but before deferred `alpine.min.js`. Bundling or adding `defer` to checkouts breaks boot to blank board.
  LLM fix:
  1. Make both scripts `defer` in correct order (Alpine first, then checkouts): `<script src="/static/js/alpine.min.js" defer></script>` then `<script src="/static/pages/checkoutsv1/checkouts.js" defer></script>`.
  2. Keep `alpine:init` listener, but also handle race where Alpine already initialized: inside `DOMContentLoaded`, if `window.Alpine && !window.__checkoutsBoard` then manually `Alpine.data(...)` and `Alpine.initTree(document.body)` or at least show fallback immediately without racing the script load.
  3. Ensure `cmd/assets` hashing still works with `defer` (it regexes `src="..."`, order-agnostic — confirm).
  4. Verify: throttling + cache test — hard refresh with slow Alpine (Chrome DevTools throttling) → board still boots. Add smoke test asserting script tag order in HTML.
  5. Run: `npm test`, manual slow-network test.

- [x] **B4 [medium] `applyURL()` runs before groups load, flashes unfiltered board**
  File: `internal/web/static/pages/checkoutsv1/checkouts.js:408-413,588-616,98-128`
  Problem: `init()` calls `applyURL()` with `locationGroups=[]`, so `?location_group_name=Grace` resolves to zero ids, `filterActive=false` shows everything until `fetchLocationGroups→setGroups→applyURL` corrects + extra fetch.
  LLM fix:
  1. Gate first `fetchChildrenData` on groups resolution: remove the immediate `fetchChildrenData()` from `init()`, instead chain `fetchLocationGroups().then(()=> fetchChildrenData())`, or add a `groupsReady` flag and `if (!groupsReady) filterEmpty=true` default so first render shows empty/filtered until groups resolve.
  2. Preserve no-filter case: when URL has no params, `filterActive` should remain false after groups load (all selected).
  3. Verify: integration test with `locationGroups` mock delayed → assert `visibleChildren` is correctly filtered on first fetch after groups, not flashing unfiltered. Test `?location_group_name=Grace` case explicitly.
  4. Run: `npm test`

---

## Batch C — Rendering, A11y & HTML (P1)

- [x] **C1 [medium] Hidden overdue sheet stays tab-focusable**
  File: `internal/web/static/pages/checkoutsv1/checkouts.html:320-322`
  Problem: hides via `:class="{translate-y-full}"` + `aria-hidden`, no `x-show`/`inert`. Close button + sheet checkboxes remain in tab order when closed — `aria-hidden=true` with focusables is a11y violation.
  LLM fix:
  1. Add `x-show="sheetOpen"` to sheet root OR `:inert="!sheetOpen"` (preferred for keeping animation: use `inert` + `x-transition` combo; check Alpine 3 supports `:inert`). If using `x-show`, ensure transition still works (wrap content or use `x-transition` on backdrop already there).
  2. Trap focus when open, return focus to badge/button on close (optional but recommended: add `x-trap="sheetOpen"` or manual focus return in `closeOverdueSheet`).
  3. Verify: with sheet closed, `document.activeElement` tab sequence skips `#overdue-sheet-close` and sheet checkboxes; with sheet open, they are reachable. axe audit passes.
  4. Run: `npm test`, manual a11y check.

- [x] **C2 [low] Badge aria/text drift, `badgeAria` dead code**
  File: `internal/web/static/pages/checkoutsv1/checkouts.html:312` + `checkouts.js:355,359`
  Problem: HTML binds inline `` `${badgeCount} overdue checkouts...` `` while `badgeText` is `"${count} overdue. Tap to view"` (period) and `badgeAria` getter unused. Future edit diverges.
  LLM fix:
  1. Bind `:aria-label="badgeAria"` (already defined) and `x-text="badgeText"` consistently. Change line 312 from inline template to ` :aria-label="badgeAria"`.
  2. Assert both in A/B harness or unit test.
  3. Run: `npm test`

- [x] **C3 [low] Collapsed search panel still exposed to AT + sheet missing `x-cloak`**
  File: `internal/web/static/pages/checkoutsv1/checkouts.html:215,184,311,318,320,343`
  Problem: collapse is height class only, no `aria-hidden`; panel readable when collapsed. Sheet root lacks `x-cloak` (badge/backdrop have it), crawlers/AT see `No overdue` pre-boot.
  LLM fix:
  1. Add `:aria-hidden="String(!searchOpen)"` to `#search-controls` div (line ~215).
  2. Add `x-cloak` to sheet root div `#overdue-sheet` (line ~320) for consistency with `[x-cloak]` CSS at :150.
  3. Verify: with `searchOpen=false`, `search-controls` has `aria-hidden="true"` and is hidden from AT; slow Alpine load shows no flash.
  4. Run: `npm test`

- [x] **C4 [low] Scroll clamp lost, silent 100-card truncation, CSP note**
  File: `internal/web/static/pages/checkoutsv1/checkouts.js:324-336,460-470` + `alpine.min.js:5`
  Problem: old `clampChildrenListScroll` every render; new only on `resize`. Deep scroll + filter shrink can strand. `.slice(0,100)` silently drops kids from board+badge. No CSP today but Alpine uses `new Function`/`with`.
  LLM fix:
  1. Add clamp in reactive effect: `x-effect` on `visibleChildren.length` or after `children` assignment call `onResize()` / clamp logic via `$refs.childrenList`. Simplest: add `this.$nextTick(()=> this.onResize())` after `this.children = combined` in `fetchChildrenData`.
  2. Expose truncation: add `get truncatedCount()` or compute in `visibleChildren` caller; render footer `"Showing 100 of ${total}"` when `children.length > 100` after filtering (or total length). Consider raising limit or server-side filter passthrough — at minimum document.
  3. Document CSP requirement (`unsafe-eval`) in README/AGENTS.md if CSP added later; no code change needed now beyond comment.
  4. Verify: manual scroll-deep then `hideConfirmed=true` → scrollTop clamps; >100 kids show footer message.
  5. Run: `npm test`

---

## Batch D — Infra, Vendoring & Build (P1)

- [x] **D1 [medium] `morphdom` removal incomplete, still ships in binary**
  File: `internal/web/static/js/morphdom-umd.min.js:1` + `internal/web/static/static.go:13,73-81` + `checkouts.html:379-380`
  Problem: HTML correctly swaps to `alpine.min.js`, but stale 12K file remains. `//go:embed *` + `.js` allowed means it still embeds/serves at `/static/js/morphdom-umd.min.js`.
  LLM fix:
  1. `git rm internal/web/static/js/morphdom-umd.min.js`.
  2. Confirm `grep -r morphdom` returns 0 hits after removal.
  3. Verify: `make build` then check binary no longer serves `morphdom` (or just check file deleted and `static_test.go` would 404).
  4. Run: `go test ./internal/web/static -run TestFilteredFS` + `make build`

- [x] **D2 [medium] Vendored Alpine has no version/provenance pin**
  File: `internal/web/static/js/alpine.min.js:1-21` (54K, v3.17.2 buried)
  Problem: can't audit/update, Dependabot-blind, no SRI.
  LLM fix:
  1. Add header comment to `alpine.min.js` first line: `/*! Alpine.js v3.17.2 | https://cdn.jsdelivr.net/npm/alpinejs@3.17.2/dist/cdn.min.js | vendored 2026-09-13 | SRI: <hash> */` (compute hash or at least note provenance).
  2. Optionally pin same version in `package.json` or document in `docs/vendors.md` / README.
  3. Extend `internal/web/static/static_test.go:111-118` to assert version string `3.17.2` appears in file content (already checks NotEmpty, add `assert.Contains(t, string(content), "3.17.2")`).
  4. Verify: `go test ./internal/web/static -run TestFilteredFSBlocksHTML` (or similar) and manual file header check.
  5. Run: `go test ./internal/web/static`

- [x] **D3 [medium] Stale Tailwind pipeline, new utilities may not build**
  File: `tailwind.config.js:2` + `package.json:8` + `internal/web/static/css/tailwind.css:1-2` + `checkouts.html:150`
  Problem: content globs `./*.html`, `./*.js` / `"."` don't cover `internal/web/static/pages/**`. `[x-cloak]{display:none}` hand-written masks risk but other new utilities may be missing.
  LLM fix:
  1. Update `tailwind.config.js` content to `["./internal/web/static/**/*.{html,js}"]` (keep existing if needed, but add this pattern).
  2. OR update `package.json` `build:css`/`watch:css` `--content` flag to same pattern (currently `--content "."`).
  3. Run `npm run build:css` and commit regenerated `internal/web/static/css/tailwind.css`.
  4. Verify: `grep -q "x-cloak" internal/web/static/css/tailwind.css` no longer needed (it's hand-written CSS, but check new utilities like `rotate-180` appear).
  5. Run: `npm run build:css`, visual diff HTML still styled.

---

## Batch E — E2E Harness (P2)

- [x] **E1 [medium] E2E uses fixed sleeps, races 3s poll**
  File: `e2e/ab-validate/checkouts.mjs:121-130 + ~12x sleep(500)`
  Problem: `settled()` 4x `sleep(3600)` = ~15s/side minimum, still flaky under load.
  LLM fix:
  1. Replace `sleep(500)` after filter/search/confirm with deterministic waits: `await page.waitForFunction(()=> document.querySelectorAll(CARD_SEL).length === expected, ...)` or `waitForResponse` on the checkouts fetch, or `waitForTimeout` only as fallback with shorter duration.
  2. Replace `settled()` loop with `waitForFunction` on stable card count across one poll interval (e.g., poll for count stable for 3.5s via `waitForFunction` + `setTimeout`).
  3. Keep one `waitForRequest` for confirm PATCH (already at :236) and reuse pattern for other flows.
  4. Verify: run `AB_PASSWORD=... node e2e/ab-validate/checkouts.mjs` locally 3 times — all pass, total time <30s, no flake under `stress --cpu 4` or throttling.

- [x] **E2 [low] E2E hardcoded `/tmp`, fixture-coupled literals, no runner script**
  File: `e2e/ab-validate/checkouts.mjs:6,18,164,183,228` + `package.json:17`
  Problem: `/tmp/ab-fixture.db`, `/tmp/ab-validate` non-portable, collides on shared runners, setup `cp+restart` comments-only so stale DB easy. `'Group A'/'Fresh Kid'` literals break on fixture change. `playwright-core ^1.63.0` caret drift, no `e2e:ab` script.
  LLM fix:
  1. Use `import os from 'node:os'` + `path.join(os.tmpdir(), 'ab-validate')` instead of hardcoded `/tmp`. Allow overrides via `process.env.AB_SHOTS`/`AB_FIXTURE`.
  2. Derive expected groups/names from API rows (`filterLabels` already does, but hardcode at 164,183,228 should be replaced with `rows.map` derived values where possible; keep `'Fresh Kid'` only with `if (!fresh) skip-with-warning`).
  3. Add `package.json` script: `"e2e:ab": "node e2e/ab-validate/checkouts.mjs"`.
  4. Consider `package-lock.json` exact pin for `playwright-core` (caret currently `^1.63.0` — pin to exact or document).
  5. Add fail-fast if `validate-main.db`/`validate-alpine.db` mtimes are too old (warn if setup not run).
  6. Verify: `npm run e2e:ab` works on Linux and macOS; env override tested.

---

## Batch F — Missing Test Coverage (P1, depends on A)

Depends on Batch A. Do not start until A1–A4 land.

- [x] **F1 [high] Restore/search/jiggle/backdrop/sheet-sync/error-DOM tests**
  File: `internal/web/static/pages/checkoutsv1/checkouts.test.js:92-538` + `checkouts.html:269,303,305` + `checkouts.js:732,773,798`
  Coverage lost vs old suite: search toggle expand/focus/resize, XSS, jiggle-on-increase-only, backdrop click + scroll-lock, sheet→main pill sync, error/loading/empty DOM states, polling/clock/tick, `previewChildren` semantics, brittle harness.
  LLM fix (add these tests to `checkouts.test.js` and `preview.test.js`):
  1. **Search toggle**: click `#search-toggle-button` → assert `search-controls` has `is-expanded`, `aria-expanded=="true"`, `.rotate-180` on icon, `document.activeElement === searchInput`; `onResize()` while open preserves clamped height.
  2. **XSS**: create child with `first_name: '<img src=x onerror=alert(1)>'` and `last_name: '"><svg'`, render via board, assert `innerHTML` contains no executable markup, `textContent` preserves literal, and grep `checkouts.html` for zero `x-html` occurrences (static assertion).
  3. **Jiggle**: set `overdueChildren` increase 0→2 → assert `overdueBadge` has `overdue-badge-jiggle` class (or animation); decrease 2→1 → assert no jiggle restart.
  4. **Backdrop/scroll-lock**: `openOverdueSheet()` → assert `document.body.style.position==="fixed"` and backdrop visible; click `#overdue-sheet-backdrop` → assert `sheetOpen===false` and `body.style.position===""`.
  5. **Sheet sync**: confirm via sheet checkbox (`#overdue-sheet-list .child-confirmed-checkbox`) → assert both `#children-list .child-time` and `#overdue-sheet-list .child-time` flip to `bg-gray-400`.
  6. **Error/loading/empty DOM**: mock fetch to reject → assert `board.loadError===true` AND `x-show="loadError"` div visible, `loading` div hidden; with zero children assert `emptyMessage` DOM shows correct variant.
  7. **Polling/clock/tick**: unit-test `tick()/tickClock()/sweepOverrides()` directly (call with fake `Date.now`); assert `nowMs` advances, overrides swept, no `setInterval` stub hides them. Use fake timers or inject `Date.now` mock.
  8. **Preview semantics**: assert `previewChildren` preserves authored order (not sorted), and `fetchBlocked` blocks subsequent `fetchChildrenData()` while set.
  9. Replace `await 100ms` / `flush 20ms` with `waitFor` or fake timers where feasible; route one test through `board.init()` / real `Alpine.start()` if possible; remove unused `beforeEach,afterEach` import.
  10. Verify: `npm test` all new tests pass, coverage of new Alpine paths > old parity.

---

## Completion Checklist

- [x] All A-batch items checked and `npm test` green
- [x] All B-batch items checked and manual visibility/slow-network checks pass
- [x] All C-batch items checked and axe/manual a11y pass
- [x] All D-batch items checked and `make build` + `ASSET_BUILD=1 make build` pass, `go test ./internal/web/static` green
- [x] All E-batch items checked and `npm run e2e:ab` (or direct node) 3/3 green
- [x] All F-batch items checked and `npm test` green with new coverage
- [x] `make test` (full suite) passes
- [x] This file moved/archived or left with all boxes checked

