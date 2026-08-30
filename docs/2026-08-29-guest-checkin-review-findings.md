# Guest Check-in Family Model — Review Findings & Fix Plan

Branch: `guest-checkin-family-model` vs `main`
Review date: 2026-08-29 (6 parallel section reviews + integration pass)
Baseline at review time: all Go tests pass (`godotenv go test ./...`, 23 packages), all JS tests pass (`npx vitest run`, 146 tests), `gofmt` clean.

Grill pass 2026-08-29: every finding below was re-verified against HEAD `699eea1` (citations checked line-by-line, both suites re-run green, migration behavior verified empirically on disposable DBs). Corrections are folded into the items and marked "Corrected (grill)". One finding (#6) was refuted and replaced; two pre-existing broken down migrations were discovered while re-verifying it.

## Working instructions (for the LLM agent addressing these findings)

- Work through this file **top to bottom**. Decision **D1** is blocking — its outcome changes the fix for S1, #1, and the kiosk items.
- Read `AGENTS.md` first and follow its conventions: `fmt.Errorf("...: %w")` error wrapping, sentinel errors checked with `errors.Is`, `fiber.NewError(status, msg)` for client-visible HTTP failures, UTC for persisted timestamps, `testify` require/assert patterns, minimal diffs, no comments unless asked.
- Only tick a checkbox when the fix is implemented **and** its Verify step passes. If you deliberately skip or defer an item, move its text to the "Deferred" section at the bottom with a one-line reason.
- Keep the suites green after each finding: `godotenv go test ./...` and `npx vitest run` (or plain `go test` if env vars aren't needed). Run the targeted package test during the fix; run the full suite before ticking.
- Migration edits: this branch is **unmerged**, so edit the existing migration `db/migrations/20260825030013_add_guest_family_model.{up,down}.sqlite` in place rather than adding a new migration. After editing, refresh the snapshot with `make db-migrate` (it regenerates `db/structure.sql`) — or `make db-reset` locally if your dev DB already applied the old version. Both up and down files must stay symmetric. `db/migration_test.go` and `db/structure_test.go` enforce this. Item #6 originally additionally edits two pre-existing down migrations from `main` — safe for the same reason: golang-migrate records version numbers, not file contents. **Update 2026-08-30: per user direction, do NOT modify old migration files — only the most recent migration (`20260825030013`) may be edited. #6's old-down fixes are therefore deferred (see #6 and Deferred).**
- Tailwind: if any fix adds or changes utility classes in HTML, run `npm run build:css`.
- Do not commit. Let the user complete that action (per AGENTS.md).

## Verified clean — do not re-review or change

These were explicitly verified during review; don't spend cycles here:

- XSS posture: all guest-provided values render via `textContent` or are escaped (`checkin.js`, `admin-guest-entries.js`, `manual-checkins.js`, `menu.go` uses `html/template` escaping). Grill caveat: `escapeHtml` in `manual-checkins.js` does not escape quotes, so it is not attribute-safe in the quoted `data-*` contexts that use it (`:238, :241, :242`) — currently only the server-generated `public_id` flows there, so this stays clean unless the ID format ever becomes guest-influenced.
- Contract fidelity: every endpoint, query param, and field name across JS ↔ controllers ↔ repos ↔ migration matches.
- Menu: role gating matches login's session values (`"admin"`/`""`), placeholder `<!-- kebab-menu-links -->` present on all three menu pages and enforced by `menu_test.go`.
- Session alias wiring (`fibersession` vs internal `session`), route registration order in `server.go` (guestcheckinv1 after checkinv1/manualcheckinv1 — load-bearing for the noStore layer).
- `manual_count` removal from metrics fully coordinated across repo/controller/JS.
- `db/structure_test.go` drift detection genuinely catches structure.sql divergence (verified by perturbation).
- `ApproveSubmission` is transactional with per-child idempotency + rows-affected conflict guard; `CreateSubmission` transactional with zero-child guard.
- Kiosk double-submit guard exists (submit button disabled during flight, re-enabled in `finally`).

---

## Decision required first

- [x] **D1. [blocking] Decide the kiosk auth model**
  - **Decision: Option A**
  - Current code: both `GET /checkin` and `POST /v1/checkins/guest-submissions` sit behind `AuthRequired(sessionStore, "")` (`internal/controllers/guestcheckinv1/guest_submission.go:45,57`) — i.e. any logged-in session, any role, is required. The stated intent was "no auth on kiosk check-in", which does **not** match the code.
  - Option A — truly public kiosk:
    1. In `RegisterRoutes` (guest_submission.go:43-58), move the create route out of the authed group: register `group.Post("/guest-submissions", controller.CreateSubmission)` on a **new group without the `Use(AuthRequired)` line** (or directly on `app`). Keep the `noStoreCache` middleware on the new group — the current group applies it to the POST route and it must not be lost along with the auth line. Keep list/PATCH/checkins admin routes authed — they return PII and must stay behind `AuthRequired("")`/`AuthRequired("admin")`.
    2. Change `app.Get("/checkin", middleware.AuthRequired(...), controller.KioskPage)` to `app.Get("/checkin", controller.KioskPage)`.
    3. Corrected (grill): a public POST endpoint has no rate limiting anywhere in this app. If the kiosk is only reachable on a trusted LAN that may be an acceptable bound — confirm that assumption with the user; otherwise add minimal abuse mitigation (per-IP throttle or honeypot field) before shipping.
  - Option B — shared logged-in kiosk device (keep auth as-is): then the session-expiry UX fixes below (S1, #1, #7) become **required**, not optional. Corrected (grill): a kiosk tablet whose session expired does **not** lose the typed submission — `resetForm()` runs only on success (`checkin.js:231`), so the failure mode is a cryptic "Unexpected token" error with the form intact. Confusing, not destructive.
  - Either way, update `guest_submission_test.go` to assert the chosen behavior (unauthenticated POST → 201; or unauthenticated POST → 302 `/login`).
  - Verify: `go test ./internal/controllers/guestcheckinv1` + the applicable new tests.

## Fix-once systemic item

- [x] **S1. Shared fetch wrapper: redirect detection + error-envelope parsing**
  - Problem: session expiry makes the backend 302 to `/login`; `fetch` follows it, the response is 200 HTML, and every page mishandles it differently (silent success, cryptic "Unexpected token '<'"). Corrected (grill): no form data is ever lost to this — every page only clears state on success. Separately, the global error envelope is `{"sorry":"<msg>"}` (`server.go:88` ErrorHandler) but pages parse it inconsistently.
  - Fix: added `internal/web/static/js/api.js` exposing `window.fetchJson(url, options)` (script tags, no bundler — match how `kebab-menu.js` is included and consumed):
    1. After `fetch`, if `response.redirected` (or the response URL is `/login`, or content-type is HTML on a JSON call) → throw a `SessionExpiredError`; callers redirect to `/login?next=<current path>`.
    2. On non-2xx: try `response.json().catch(() => ({}))` and use `data.sorry || data.error || data.message || \`request failed (${response.status})\`` as the error message. Corrected (grill): include `data.error` — auth 403s return `{"error":"Forbidden..."}` (`middleware/auth.go:50`), a shape the original spec missed. Note `data.message` matches nothing this backend currently emits (it lives only in `location-groups.js`'s fallback chain); keep it last for compatibility.
  - Adopted in: `pages/checkin/checkin.js`, `pages/admin-guest-entries/admin-guest-entries.js`, `pages/manual-checkins/manual-checkins.js`, `pages/admin/metrics.js`. Keep behavior compatible with `pages/admin/location-groups.js:128` (already parses `data.message || data.sorry`).
  - Fold-in (grill): `server.go:88` builds `{"sorry":"%s"}` via `fmt.Sprintf` with no JSON escaping — currently safe only because every message is a quote-free constant; prefer `json.Marshal` or `c.JSON` next time the ErrorHandler is edited.
  - This wrapper is the fix vehicle for findings **#1, #7, #8, #22** — implemented, then those findings shrink to "adopt wrapper + add tests".
  - Verify: unit tests for the wrapper (`internal/web/static/js/__tests__/api.test.js` — 11 tests), plus per-page tests pass; `npx vitest run`.

---

## High

- [x] **1. [high] Admin "Mark entered" treats session expiry as success**
  - Where: `internal/web/static/pages/admin-guest-entries/admin-guest-entries.js:243-250` (`markEntered`), `:322-332`.
  - Problem: unauthenticated PATCH follows the 302 to `/login` (fetch follows redirects, so the PATCH lands as a GET on login HTML, 200) → `response.ok` is true → user sees "Entry marked as entered." while nothing was persisted. Corrected (grill): the follow-up `loadEntries` also follows the redirect and throws inside its own catch (blanking the container), but the catch swallows it, so the success status at `:327` overwrites the error and the button stays disabled — the visible symptom is a success message, a blank list, and a stuck button. There is no optimistic update anywhere in this flow (UI changes only after the awaited reload).
  - Fix: via S1 wrapper, or directly in `markEntered`/`loadEntries`: check `response.redirected` / non-JSON content-type before parsing; on session expiry redirect to `/login?next=/admin/guest-entries` or show an explicit "session expired" error and leave the button enabled.
  - Verify: add a vitest case mocking fetch to return `{ ok: true, redirected: true, url: '/login', ... }` (or 200 with `text/html`) and assert NO success status is shown, the list is not blanked by a swallowed `loadEntries` error, and the button is re-enabled (there is no optimistic update in this flow — don't test for one). `npx vitest run internal/web/static/pages/admin-guest-entries/admin-guest-entries.test.js`

## Medium — data lifecycle & migration

- [x] **2. [medium] Unbounded listing + false "default 100" comment; `without_manual_checkins` consumer relies on unbounded rows**
  - Where: `internal/repo/guestsubmission/guestsubmission.go:232-234` (LIMIT only when `filter.Limit > 0`), `internal/controllers/guestcheckinv1/guest_submission.go:370` (comment documents a default that doesn't exist), `internal/web/static/pages/manual-checkins/manual-checkins.js:252-253` (fetches `?status=entered&without_manual_checkins=true` with no limit and renders the full set).
  - Fix:
    1. Implement the documented behavior in the controller's `buildFilter`: default `limit=100` when unset, and fix or delete the stale `// limit capped at 200, default 100` comment (`guest_submission.go:370`). Corrected (grill): `limit=abc` → 400 already exists (`:366-368`) and the >200 clamp already exists (`:371-373`) — only the default-when-unset is missing; don't re-implement the other two.
    2. In `manual-checkins.js`, pass an explicit `limit=200` and handle the "more than 200 exist" case (show a "showing first 200" notice) — don't rely on the default.
    3. Alternatively (also acceptable): repo-level default cap of 200 when `filter.Limit <= 0`; pick one, delete the stale comment.
  - Verify: `go test ./internal/controllers/guestcheckinv1 ./internal/repo/guestsubmission` with new tests for default/cap/invalid; `npx vitest run internal/web/static/pages/manual-checkins/manual-checkins.test.js`.

- [x] **3. [medium] `without_manual_checkins=true` resurrects old families after cleanup; guest data has no retention**
  - Where: `internal/repo/guestsubmission/guestsubmission.go:550` — corrected (grill): the filter is actually `EXISTS (SELECT 1 FROM children ch WHERE ch.parent_id = guest_submissions.parent_id AND NOT EXISTS (SELECT 1 FROM manual_checkins mc WHERE mc.child_id = ch.id))` (per-child; the bare `NOT EXISTS` originally quoted is the child-level probe at `:553-555`), interacting with `internal/repo/manualcheckin/manualcheckin.go:300-315` (`RemoveOldManualCheckins` deletes only previously checked-out rows — `checked_out_at < cutoff`; never-checked-out rows are never deleted). Guest rows are never deleted (no production DELETE exists), so families whose checkins were cleaned up reappear in the staff "needs checkins" list forever. Note `guestsubmission_test.go:611-649` enshrines the *partial* reappearance case as intended (plus the sibling `..._PartialCoverage` test at `:554-609`) — both must be updated alongside.
  - Fix (recommended — state tracking): add a nullable `checkins_backfilled_at` timestamp column to `guest_submissions` (edit the existing migration per the instructions above; refresh `db/structure.sql`). Set it in `ApproveSubmission` and in the per-child insert path once every child has a checkin. Change the listing filter to `checkins_backfilled_at IS NULL` instead of the `NOT EXISTS` probe (keep the per-child `NOT EXISTS` probe inside the insert path — it guards idempotency). Verified sound (grill): child-linked `manual_checkins` rows are created only via the guestsubmission insert path — the manual-checkins API/JS never sets `child_id` — so no other writer can desync the flag.
  - Alternative (smaller — recency bound): filter the view to submissions created within N days (e.g. `created_at >= date('now', '-30 days')`). Less correct but no migration.
  - Verify: update the affected repo tests; `go test ./internal/repo/guestsubmission`; if migration edited, `make db-migrate` + `go test ./db`.

- [x] **4. [medium] Missing index on `manual_checkins(child_id)` (hot NOT EXISTS probe); secondary guest_submissions indexes**
  - Where: `db/migrations/20260825030013_add_guest_family_model.up.sqlite` — corrected (grill): the index section creates `idx_children_parent_id` (`:31`) and `idx_manual_checked_out_at` (`:56`) only; no index on `manual_checkins(child_id)` anywhere (confirmed against `db/structure.sql`, which also has no guest_submissions index beyond the implicit `public_id` UNIQUE autoindex). The original `EXPLAIN QUERY PLAN` run can't be re-verified from code, but the schema facts support it: the `NOT EXISTS` probe at `guestsubmission.go:550` scans `manual_checkins` per child with no `child_id` index, on every list with `without_manual_checkins=true` and every approval.
  - Fix (edit the existing migration in place — branch is unmerged):
    1. Up: `CREATE INDEX idx_manual_checkins_child_id ON manual_checkins(child_id);`
    2. Down: `DROP INDEX IF EXISTS idx_manual_checkins_child_id;`
    3. Consider also `guest_submissions(created_at)` (ORDER BY + metrics grouping) and `guest_submissions(parent_id)`.
    4. Refresh `db/structure.sql` via `make db-migrate` (or `make db-reset` then `make db-migrate` locally).
  - Verify: `go test ./db` (round-trip + drift tests); `go test ./internal/repo/guestsubmission`.

- [x] **5. [medium] Name backfill destroys partially-preserved names irreversibly**
  - Where: `db/migrations/20260825030013_add_guest_family_model.up.sqlite:47` — `SET first_name='Unknown', last_name='Guest' WHERE first_name='' OR last_name=''` turns `('', 'Doe')` into `('Unknown', 'Guest')`, losing the surviving `Doe`. Whitespace-only names also escape both the backfill and the CHECK (`' ' <> ''`). `db/migration_test.go:169-170` asserts the lossy behavior and must be updated with it.
  - Fix (edit the existing migration):
    1. Split into per-column backfills: `UPDATE manual_checkins SET first_name='Unknown' WHERE TRIM(first_name)='';` and `UPDATE manual_checkins SET last_name='Guest' WHERE TRIM(last_name)='';`
    2. In the rebuilt `manual_checkins` CHECK constraints, use TRIM-aware conditions (e.g. `CHECK (LENGTH(TRIM(first_name)) > 0)`).
    3. Update `migration_test.go` to assert `('', 'Doe')` → `('Unknown', 'Doe')` and that whitespace-only names get backfilled.
  - Verify: `go test ./db`; `make db-migrate` refreshes structure.sql.

- [x] **6. [medium] REFUTED as originally written — replaced: two pre-existing down migrations are broken under the real migration runner — PARTIALLY DEFERRED 2026-08-30 (old files not edited per user direction)**
  - Refuted (grill): migrations never run as autocommit multi-statement Exec in production. They run exclusively via the golang-migrate CLI (`Makefile:27-33`, Dockerfile, README; there is no in-app migration path), whose sqlite3 driver wraps each file in a transaction with rollback — a mid-file failure leaves the schema clean (golang-migrate just marks the version dirty, requiring `migrate force`). The autocommit `Exec` path exists only in the test harness (`db/migration_test.go:27`), against throwaway in-memory DBs. Do **not** add `BEGIN TRANSACTION; ... COMMIT;` to migration files — the driver's own wrap makes that fail with "cannot start a transaction within a transaction" and would break `make db-migrate` (verified empirically against the driver's behavior).
  - Real issues (pre-existing on `main`, discovered during the grill; empirically confirmed on a disposable DB):
    1. `20260329170123_manual-checkins-created-at.down.sqlite` is just `DROP TABLE manual_checkins_new;` — a table that no longer exists after the up's rename, so `migrate down` fails there first ("no such table: manual_checkins_new"). This blocks any down of 6+ steps from HEAD.
    2. `20260213191620_add_manual_checkins_public_id.down.sqlite:2,22` contains `BEGIN TRANSACTION;`/`COMMIT;` — the nested-BEGIN breakage above; it fails once (1) is fixed.
    3. The test suite can't catch either: the harness only Execs the ups plus the latest migration's own up/down (`applyMigrationsUpTo` + `RoundTrip`) — the older downs are never executed.
  - Fix (original — deferred for old files): repair the two old downs (editing old migrations is safe — golang-migrate records version numbers, not contents, and neither has ever successfully run via the CLI):
    1. `20260329170123` down: replace the bare `DROP` with a proper rebuild of the pre-`created_at` schema (copy the shape of `20260215180055_manual_checkins_checked_out_at_default_null.down.sqlite`, which recreates exactly the right column set + index).
    2. `20260213191620` down: delete only the `BEGIN TRANSACTION;` and `COMMIT;` lines (the `PRAGMA foreign_keys` lines are harmless and were verified to work under the CLI's wrap).
  - **Update 2026-08-30:** per user direction, old migration files must NOT be modified — only `20260825030013_add_guest_family_model.{up,down}.sqlite` may be edited. The two old-down repairs above were applied and then reverted (`git checkout HEAD -- db/migrations/20260213191620_add_manual_checkins_public_id.down.sqlite db/migrations/20260329170123_manual-checkins-created-at.down.sqlite`). Consequence: `migrate down 1` (this feature's rollback) still works; `migrate down 14` full-chain round-trip remains broken at those two versions — deferred to Deferred section. `go test ./db` stays green because the harness only tests the latest migration's round-trip.
  - Verify (when old files are fixed): disposable-DB CLI round trip, up then down to zero: `tmpdb=$(mktemp); migrate -source file://db/migrations -database "sqlite3://$tmpdb" up && migrate -source file://db/migrations -database "sqlite3://$tmpdb" down 14; rm -f "$tmpdb"` (14 = current migration count; both commands must complete with no error and list all 14 steps). Plus `go test ./db`. This exact sequence was run during the grill with the two downs patched in a temp copy: all 14 ups and all 14 downs complete cleanly. With old files reverted, expect failure at `202603...`/`202602...` as documented above.

## Medium — frontend behavior & tests

- [x] **7. [medium] Kiosk shows raw `{"sorry":...}` JSON and misclassifies the login redirect**
  - Where: `internal/web/static/pages/checkin/checkin.js:144-155` — non-2xx: the raw body text becomes the error message (rendered via `setKioskError` → `textContent`), showing guests literal `{"sorry":"..."}`; 2xx-HTML (login redirect when D1 = Option B): `response.json()` throws "Unexpected token...". Corrected (grill): the typed submission is **not** lost — `resetForm()` runs only on success (`:231`), so the form stays filled; the failure mode is a cryptic error, not data loss.
  - Fix: adopt the S1 wrapper; on SessionExpired show "Please ask a staff member to sign in" and keep form data intact; parse `data.sorry` with a friendly fallback for other errors.
  - Verify: add tests to `checkin.test.js` for (a) `!ok` JSON body → friendly `data.sorry` shown, (b) 200 HTML/redirected → session-expired message + form preserved, (c) network rejection, (d) second submit during in-flight POST is a no-op, (e) button re-enabled in `finally`. `npx vitest run internal/web/static/pages/checkin/checkin.test.js`

- [x] **8. [medium] Raw `{"sorry":...}` envelope shown verbatim in manual-checkins / admin pages**
  - Where: `internal/web/static/pages/manual-checkins/manual-checkins.js:70-80` (`fetchJson` throws the raw body → user sees `Failed to update: {"sorry":"invalid status transition"}`), `admin-guest-entries.js:249` (discards server message entirely, generic "Failed to mark entered").
  - Fix: adopt S1 wrapper (or parse `data.sorry` locally). The conflict message "submission status changed, please retry" must reach the user as-is.
  - Verify: `npx vitest run internal/web/static/pages/manual-checkins/manual-checkins.test.js internal/web/static/pages/admin-guest-entries/admin-guest-entries.test.js` with new failure-path cases.

- [x] **9. [medium] Auth enforcement tests cover 1 of 7 routes; no admin-role 403 test**
  - Where: `internal/controllers/guestcheckinv1/guest_submission_test.go:603-615` — only asserts unauthenticated GET of the staff list redirects to `/login`. Corrected (grill): there are 7 routes, not 6 (5 JSON APIs + 2 HTML pages, `guest_submission.go:43-59`); unauthenticated non-HTML requests get a 302 (`middleware/auth.go:24`), wrong-role gets a 403 (`:38-50`). Runtime auth is correctly enforced (verified); the gap is regression protection.
  - Fix: table-driven test hitting each staff/admin route unauthenticated (302 to `/login`) and with a non-admin session against the admin routes (`/v1/admin/guest-submissions`, `/admin/guest-entries` → 403), and non-admin staff against `/v1/checkins/guest-submissions*` staff routes per the chosen D1 model (Option A makes the POST + `/checkin` page public — those two become the asserted exceptions).
  - Verify: `go test ./internal/controllers/guestcheckinv1`.

- [x] **10. [medium] Kiosk fetch failure paths and double-submit prevention untested**
  - Where: `internal/web/static/pages/checkin/checkin.test.js:33-38` — the mocked fetch always returns `ok: true` with valid JSON.
  - Fix: add the cases listed in #7 (a)-(e). If D1 = Option A also test the public POST succeeds unauthenticated end-to-end.
  - Verify: `npx vitest run internal/web/static/pages/checkin/checkin.test.js`.

- [x] **11. [medium] `manual-checkins.test.js` rewrite dropped 5 previously-passing tests**
  - Where: whole-file rewrite on this branch removed exactly 5 previously-passing tests: `builds query params with defaults`, `builds query params from search string`, `formats checked out timestamps`, `renders a zero-state message`, `renders manual check-in rows with status and actions` (the 6th main test, the modal toggle, was kept). The covered code is still live (`manual-checkins.js:82-181, 331-338, 402-424`); the checkout/undo flow now has zero coverage. Corrected (grill): `window.*` exposures were never the issue — `main` had none either; the vitest harness evals the script, so top-level function declarations are reachable as `window.*` either way.
  - Fix: restore the old tests (retrieve from `main`: `git show main:internal/web/static/pages/manual-checkins/manual-checkins.test.js`) and merge them alongside the new approvals tests; they should pass as-is since the covered functions are still top-level declarations.
  - Verify: `npx vitest run internal/web/static/pages/manual-checkins/manual-checkins.test.js`.

- [x] **12. [medium] Merged "needs-entry" bucket breaks newest-first ordering and page-size invariants**
  - Where: `internal/web/static/pages/admin-guest-entries/admin-guest-entries.js:258-268` — fetches `pending&page=N` and `approved&page=N` independently and sorts only within the merged page; ordering holds per-status only and pages render 10-20 cards. Fold-in (grill): `totalPages` is `Math.max` of per-status page counts while `total` sums both statuses — 27 entries renders "Page 1 of 2" (no items are lost, but the pagination math is semantically inconsistent).
  - Fix options: (a) backend support for comma-separated `status=pending,approved` in `AdminListSubmissions`' filter so one paginated query serves the bucket. Corrected (grill): there is no `status` column and no `Eq`→`IN` — status is derived from timestamp nullity via `statusPredicate` (`guestsubmission.go:98-122`), so the change is a `squirrel.Or` of per-status predicates in `applyFilter` (shared by `ListSubmissions` and `CountSubmissions`, so counts stay consistent) plus Filter/controller validation/tests; then simplify the JS merge to a plain paginated list (which also fixes the `totalPages` math); or (b) document the limitation in the page (comment-free: a visible "newest first within each status" caption) and cap the merge. Prefer (a).
  - Verify: `go test ./internal/controllers/guestcheckinv1 ./internal/repo/guestsubmission`; `npx vitest run internal/web/static/pages/admin-guest-entries/admin-guest-entries.test.js`.

- [x] **13. [medium] Clipboard silently no-ops without a secure context (HTTP LAN)**
  - Where: `internal/web/static/pages/admin-guest-entries/admin-guest-entries.js:166-171` (`copyValue`), `:305-315` (tooltip still says "Copied"). Repo has no TLS config; `navigator.clipboard` requires HTTPS/localhost.
  - Fix: if `!navigator.clipboard`, fall back to the hidden-textarea + `execCommand('copy')` pattern, else show an explicit "copy unavailable" state; only show "Copied" on actual success. Fold-in (grill): also handle `writeText` rejection — today a present-but-failing clipboard API skips the tooltip and leaves an unhandled promise rejection.
  - Verify: new vitest cases for both paths; `npx vitest run internal/web/static/pages/admin-guest-entries/admin-guest-entries.test.js`.

- [x] **14. [medium] `/static` serves raw `pages/*.html` to anonymous clients, contradicting menu.go's claim**
  - Where: `internal/web/menu/menu.go:1-5` (package doc claims gated destinations "are never shipped to anonymous clients"), `internal/web/static/static.go:73-81` (`filteredFS` allows `.html`), `internal/controllers/server.go:171-175` (unauthenticated `/static`). Anyone can fetch `/static/pages/admin/index.html` and the new `/static/pages/admin-guest-entries/index.html` (route/UI disclosure only — APIs stay gated).
  - Fix: remove `.html` from the allowed extensions in `filteredFS`, or exclude the `pages/` prefix entirely (page shells are served by handlers with the menu placeholder substituted). Precondition verified (grill): no runtime JS references `/static/pages` (only test files resolve script paths via Node), and page handlers serve HTML via `EmbeddedFS` directly — the restriction is safe.
  - Verify: `go test ./internal/web/... ./internal/controllers`; add a test asserting `GET /static/pages/admin/index.html` 404s.

## Medium — repo interface

- [x] **15. [low — re-graded by grill; latent-only] `UpdateSubmissionStatus` can approve without creating manual checkins (latent bypass)**
  - Where: `internal/repo/guestsubmission/guestsubmission.go:339-347` — accepts `StatusApproved` and only flips `approved_at`; the controller routes `approved` → `ApproveSubmission` correctly today (verified at `guest_submission.go:291-295`), and the method is itself race-safe (guarded UPDATE + rows-affected check), so this is interface hygiene, not a live bug. The interface still permits the bypass and tests use it.
  - Fix: in `UpdateSubmissionStatus`, reject `StatusApproved` with a wrapped error pointing callers to `ApproveSubmission` (sentinel or `fmt.Errorf`; follow existing error conventions in the package). Corrected (grill): `UpdateSubmissionStatus(..., StatusApproved, ...)` is used at `guestsubmission_test.go:109, :291, :355, :407, :417` — update all five to `ApproveSubmission`, not just `:109`.
  - Verify: `go test ./internal/repo/guestsubmission ./internal/controllers/guestcheckinv1`.

---

## Low

- [x] **16. [low] DOB future-check mixes zones (UTC-parsed date vs local `time.Now()`)**
  - Where: `internal/controllers/guestcheckinv1/guest_submission.go:135-141` — `time.Parse` yields UTC midnight; the local-date fix landed only client-side (`checkin.js` uses `toLocaleDateString`). West-of-UTC servers accept tomorrow's local date in the evening; east-of-UTC servers reject born-today in the morning.
  - Fix: `dob, err := time.ParseInLocation("2006-01-02", child.DOB, time.Local)` and keep the `After(time.Now())` rejection.
  - Verify: new controller test with a DOB equal to today's local date (accepted) and tomorrow's (rejected). `go test ./internal/controllers/guestcheckinv1`.

- [x] **17. [low] No server-side length caps on text fields**
  - Where: `internal/controllers/guestcheckinv1/guest_submission.go:102-144` — names/phone/email/grade accept arbitrarily long strings (bounded only by Fiber's 4MB body limit); kiosk HTML has no `maxlength`.
  - Fix: cap server-side in the validation block (suggested: names ≤ 100, phone ≤ 30, email ≤ 254, grade must match the known option set, DOB exactly `YYYY-MM-DD` length) returning 400; add matching `maxlength` attributes to `internal/web/static/pages/checkin/index.html` inputs. Corrected (grill): the grade option set currently exists only client-side (`GRADE_OPTIONS` at `checkin.js:13`, rendered as a `<select>` — the normal UI is constrained but crafted requests are not); the fix must introduce the set in Go.
  - Verify: controller tests for over-cap fields → 400; `go test ./internal/controllers/guestcheckinv1`.

- [x] **18. [low] FK violation detected by brittle string match, dropping the error chain**
  - Where: `internal/repo/manualcheckin/manualcheckin.go:200-202` — `strings.Contains(err.Error(), "FOREIGN KEY")` couples to driver message text and wraps with `%v`.
  - Fix: map via the driver error type: `var sqliteErr sqlite3.Error; if errors.As(err, &sqliteErr) && sqliteErr.ExtendedCode == sqlite3.ErrConstraintForeignKey { return fmt.Errorf(...: %w, repo.ErrInvalidManualCheckin-ish sentinel) }` — reuse the existing invalid-manual-checkin sentinel from the pre-check at lines 179-191. Wrap the fallback with `%w`. Fold-in (grill): `ErrInvalidManualCheckin`'s message is name-specific ("manual checkin must provide first and last name"), so wrapping FK/child-not-found errors in it reads badly — reword the sentinel (e.g., "invalid manual checkin") while doing this.
  - Verify: repo test forcing an FK violation (drop the pre-check order via a concurrent delete, or unit-test the mapping with a constructed `sqlite3.Error`). `go test ./internal/repo/manualcheckin`.

- [x] **19. [low] Untested error branches (4 items)**
  - Where/fix — add tests:
    1. `guestsubmission_test.go`: `CreateSubmission` with `[]Child{}` asserts the zero-child error; empty phone+email asserts tx rollback (no orphan parent row). Corrected (grill): the repo itself doesn't validate phone/email — the parents-table CHECK (`phone <> '' OR email <> ''`, migration `:8`) is what trips inside the tx, which is exactly what makes this a genuine rollback test: assert the wrapped error and that no parent row persists.
    2. `manualcheckin_test.go`: `CreateManualCheckin` with a garbage `ChildID` asserts `ErrInvalidManualCheckin` (both the pre-check branch and, if practical, the FK-fallback branch).
    3. `manualcheckinv1` controller test: POST `/v1/checkins/manual-checkins` with `first_name: "   "` → 400 (guards the new `ErrInvalidManualCheckin`→400 mapping).
    4. `server_test.go` + page handlers: stub `session.Storer` whose `Get` errors → 500 for `/`, the checkouts page, and `/manual-checkins`. Fold-in (grill): on authed routes a failing session `Get` currently nil-panics — `middleware/auth.go:16` ignores the error — and surfaces as a recovered 500, not the clean "could not fetch session" branch; expect that if the stub also flows through `AuthRequired`.
  - Verify: `go test ./internal/repo/... ./internal/controllers/...` for the touched packages.

- [x] **20. [low] Concurrent approve can surface 500 instead of the conflict path (`SQLITE_BUSY` on the write upgrade)**
  - Where: `internal/repo/guestsubmission/guestsubmission.go:394-450` — deferred tx (`BeginTx(ctx, nil)`) takes a read snapshot, then upgrades on first write (the manual-checkins INSERT at `:535-541` precedes the guarded UPDATE); a concurrent commit in between yields busy, surfacing as a wrapped "inserting manual checkin" error → HTTP 500. Data stays correct; UX-only. Corrected (grill): the original title's `SQLITE_BUSY_SNAPSHOT` is WAL-specific and the app DB is not WAL (`db/pragmas.sqlite` only touches the throwaway snapshot DB used to regenerate structure.sql) — in rollback-journal mode this manifests as plain `SQLITE_BUSY`.
  - Fix: take the write lock up front — simplest is adding `_txlock=immediate` to the DSN in `internal/db/db.go` (affects all transactions; run the full suite), or issue an explicit `BEGIN IMMEDIATE` for this tx. Corrected (grill), two adjustments: (1) the rows-affected check then consistently yields `ErrConflict`, which the controller maps to **400** today (`guest_submission.go:300-302`), not 409 — change the mapping only if a 409 is separately wanted; (2) ordering dependency: without a DSN-level `_busy_timeout` the second tx still fails fast → 500, so land #23a's `_busy_timeout` DSN param together with this fix (today the 5s `busy_timeout` PRAGMA covers only a single pooled connection).
  - Verify: full `godotenv go test ./...` (DSN change is repo-wide); existing approve-conflict tests stay green.

- [x] **21. [low] Migration tests run with FK off; round-trip doesn't assert CHECK removal or index recreation**
  - Where: `db/migration_test.go:33, :92, :124` — all DSNs are `file::memory:?cache=shared`, no `_foreign_keys=on`; `RoundTrip` (`:32-89`) verifies counts/drops but not that the CHECK is gone after down (a leftover CHECK would reject legacy blank-name inserts post-rollback) nor that `idx_manual_checked_out_at` is recreated.
  - Fix: append `&_foreign_keys=on` to the migration-test DSNs; add assertions for post-down CHECK absence (blank-name insert succeeds) and index recreation (and, after #4, `idx_manual_checkins_child_id` drop/recreate) via `sqlite_master` queries.
  - Verify: `go test ./db`.

- [x] **22. [low] Session-expired GETs surface as cryptic JSON parse errors**
  - Where: `admin-guest-entries.js:259-263`, `manual-checkins.js:70-80, 248-262`, `metrics.js:3-10, 12-19, 21-28` (all three metrics loaders hit the same pattern) — expired-session fetches follow the 302, get 200 HTML, `r.json()` throws, user sees "Unexpected token '<'".
  - Fix: adopt the S1 wrapper across these pages; on SessionExpired redirect to `/login?next=<path>`.
  - Verify: per-page vitest cases with a redirected/HTML fetch mock. `npx vitest run`.

## Low — misc (independent small items)

- [x] **23a. [low] Misleading one-shot PRAGMA block in `InitDB`** — `internal/db/db.go:34-41` (and `internal/db/prepare_test_db.go:17-19`): `Exec("PRAGMA foreign_keys=ON ...")` runs on one pooled connection only; the `_foreign_keys=on` DSN param is the load-bearing fix (confirmed: the DSN carries only `_foreign_keys=on`, `db.go:21-27`). Move per-connection settings to DSN params (`_busy_timeout`, `_journal_mode`) or a connect hook; at minimum the code must not invite "simplifying away" the DSN param. Land the `_busy_timeout` DSN param together with #20 — its `_txlock=immediate` fix depends on it. Verify: full `godotenv go test ./...` (FK enforcement is load-bearing).
- [x] **23b. [low] Triple session fetch per checkouts HTML request** — on `GET /v1/checkins/checkouts`: `AuthRequired` (`middleware/auth.go:16`) + `Checkouts` (`checkinv1/checkin.go:67`) + the HTML branch (`:99`) each call `sessionStore.Get`, and each `Get` re-fetches/decodes from storage (fiber v2.52.9 session store). Reuse the middleware's session via `c.Locals` or pass values into `checkoutsWeb`. Verify: `go test ./internal/controllers/checkinv1 ./internal/controllers`.
- [x] **23c. [low] Stale DOB `max` attribute on long-lived kiosk pages** — `internal/web/static/pages/checkin/checkin.js:31` (max is baked per row creation; the initial row is created at page load, so it effectively bakes at load) vs `:208-215` + `:219` (fresh validation). After midnight the native `rangeOverflow` message shows yesterday's date until reload. Refresh each row's `max` during validation or drop the attribute and rely on the JS check. Verify: new vitest case with a frozen-then-advanced clock; `npx vitest run internal/web/static/pages/checkin/checkin.test.js`.
- [x] **23d. [low] Status tone classes accumulate in admin-guest-entries** — `admin-guest-entries.js:58-64`: `text-red-700`/`text-emerald-700` added but never removed; after an error, later messages render wrong. Copy the remove-then-add pattern from `manual-checkins.js:18-30`. Verify: `npx vitest run internal/web/static/pages/admin-guest-entries/admin-guest-entries.test.js` with a tone-switching case.
- [x] **23e. [low] `loadPendingFamilies` lacks abort/out-of-order protection despite 5s polling** — `manual-checkins.js:248-262`: `loadManualCheckins` uses an AbortController (lines 183-211); the new poller doesn't — it even contains a dead `AbortError` check (`:259`) with no signal ever passed — so a slow earlier response can overwrite a newer render, and errors wipe all cards until the next poll (`:260`). Mirror the existing abort pattern. Verify: `npx vitest run internal/web/static/pages/manual-checkins/manual-checkins.test.js`.
- [x] **23f. [low] `loadGuestMetrics` reads `data.message`, which never matches any server error shape** — `internal/web/static/pages/admin/metrics.js:7, 16, 25` (all three loaders): server messages always discarded in favor of the generic fallback. Fix via S1 wrapper or parse `data.sorry`. Fold-in (grill): non-`fiber.Error` 500s send `{"sorry":""}` (empty message) anyway — the wrapper's `||`-fallback ordering handles that; 403s send `{"error":...}`, covered by the S1 amendment. Verify: `npx vitest run internal/web/static/pages/admin/metrics.test.js`.
- [x] **23g. [low] `admin-guest-entries.test.js` has no failure-path coverage** — no tests for non-2xx load (error status shown), `markEntered` failure (button re-enabled, error shown), or the login-HTML/session-expiry response (overlaps #1 — same test can cover both once #1 is fixed). Verify: `npx vitest run internal/web/static/pages/admin-guest-entries/admin-guest-entries.test.js`.

---

## Deferred

- **6 (partial). Two pre-existing down migrations broken** — `20260329170123_manual-checkins-created-at.down.sqlite` (`DROP TABLE manual_checkins_new`) and `20260213191620_add_manual_checkins_public_id.down.sqlite` (`BEGIN`/`COMMIT` nested-tx) — fix deferred per 2026-08-30 user direction to not modify old migration files; only `20260825030013` may be edited. See #6 for repair steps. Impact: deep `migrate down 14` fails; `migrate down 1` and `go test ./db` unaffected.

## Sign-off checklist

- [ ] D1 decision recorded in this file (which option was chosen)
- [ ] `godotenv go test ./...` green
- [ ] `npx vitest run` green (146+ tests)
- [ ] `gofmt` clean (`gofmt -l .` empty)
- [ ] If any migration was edited: `make db-migrate` refreshed `db/structure.sql`, up/down stay symmetric
- [ ] If any migration was edited: disposable-DB CLI round trip clean (`migrate up` + `migrate down 14` on a temp DB — see #6's Verify)
- [ ] If any HTML utility classes changed: `npm run build:css` run