# Guest Check-in Extra Fields — Implementation Plan
*Generated: 2026-08-30 | Branch: `guest-checkin-family-model` | Base: `20260825030013_add_guest_family_model` (in-place edit)*

## Instructions for Implementer (LLM)

Work through **Tasks 1–9 in order. Do not skip or reorder.** After completing a task and its verification, **check its checkbox** by changing `- [ ]` to `- [x]` in this file before proceeding. Follow `AGENTS.md` conventions (`gofmt`, `fmt.Errorf("...: %w")`, `errors.Is`, `fiber.NewError`, UTC timestamps, `testify` require/assert, `context.Context` to repos, `mime.ParseMediaType` checks). Keep diffs minimal. Do not commit; leave commits to the human. Run `go fmt ./...` before final verification.

**Confirmed product decisions (do not revisit — from user 2026-08-30):**
- Child `gender`: `Boy` | `Girl` only (required, per child).
- Child `relationship`: `Parent` | `Guardian` | `Grandparent` | `Other` (required, per child — child-level).
- Child `dietary_restrictions`, `special_needs`: free-text `TEXT`, optional.
- Parent phone: **required** (was `phone OR email`; now `phone <> ''`). Email becomes optional (keep `TEXT NOT NULL` column, allow `''`).
- Parent address: `address1`, `city`, `state`, `zip` required; `address2` optional. State `^[A-Za-z]{2}$` (stored upper), Zip `^\d{5}(-\d{4})?$`.
- Safety ack: column on `guest_submissions` + required checkbox on form with verbatim text `FOR SAFETY PURPOSES, I MUST PRESENT SAFETY CLAIM TAG ASSIGNED TO CHILD OR A VALID ID UPON CHECKOUT TO OBTAIN MY CHILD.`
- Backfill: **not needed** — new columns get `DEFAULT ''` / `DEFAULT 0` so existing rows survive `up` (Q8).

---

## 1. File Scope Tracker

- [ ] `db/migrations/20260825030013_add_guest_family_model.up.sqlite` *(edit in place)*
- [ ] `db/migrations/20260825030013_add_guest_family_model.down.sqlite` *(edit in place)*
- [ ] `db/structure.sql` *(regenerated via `make db-migrate`, do not hand-edit)*
- [ ] `internal/repo/guestsubmission/guestsubmission.go`
- [ ] `internal/controllers/guestcheckinv1/guest_submission.go`
- [ ] `internal/controllers/guestcheckinv1/guest_submission_response.go`
- [ ] `internal/web/static/pages/checkin/index.html`
- [ ] `internal/web/static/pages/checkin/checkin.js`
- [ ] `internal/web/static/pages/admin-guest-entries/admin-guest-entries.js`
- [ ] `internal/web/static/pages/admin-guest-entries/index.html` *(if label tweaks needed)*
- [ ] `internal/repo/guestsubmission/guestsubmission_test.go`
- [ ] `internal/controllers/guestcheckinv1/guest_submission_test.go`
- [ ] `internal/web/static/pages/checkin/checkin.test.js` *(or `__tests__` equivalent)*
- [ ] `cmd/random-data/main.go`
- [ ] `db/migration_test.go` *(extend round-trip assertions)*

---

## 2. Sequential Build Steps

### Task 1: Database migration (in-place edit)

**Goal:** Add 10 columns; tighten `phone` CHECK; add child CHECKs; add `safety_ack`.

- [ ] Edit `db/migrations/20260825030013_add_guest_family_model.up.sqlite`:

  Parents (`parents`):
  ```sql
  -- add after existing CREATE TABLE parents block or via ALTER TABLE:
  -- If rebuilding parents table, recreate with:
  --   phone TEXT NOT NULL CHECK (phone <> ''),
  --   address1 TEXT NOT NULL DEFAULT '',
  --   address2 TEXT NOT NULL DEFAULT '',
  --   city TEXT NOT NULL DEFAULT '',
  --   state TEXT NOT NULL DEFAULT '',
  --   zip TEXT NOT NULL DEFAULT '',
  -- Simplest (SQLite ALTER TABLE - add columns): ALTER TABLE parents ADD COLUMN ...
  ALTER TABLE parents ADD COLUMN address1 TEXT NOT NULL DEFAULT '';
  ALTER TABLE parents ADD COLUMN address2 TEXT NOT NULL DEFAULT '';
  ALTER TABLE parents ADD COLUMN city TEXT NOT NULL DEFAULT '';
  ALTER TABLE parents ADD COLUMN state TEXT NOT NULL DEFAULT '';
  ALTER TABLE parents ADD COLUMN zip TEXT NOT NULL DEFAULT '';
  -- Tighten phone CHECK: requires rebuilding parents if CHECK is table-level.
  -- Preferred: rebuild parents table with CHECK(phone <> '') (follow manual_checkins_new rebuild pattern at up.sqlite:34-56).
  -- If using ALTER TABLE, drop/recreate CHECK via table rebuild.
  ```

  Children (`children`):
  ```sql
  ALTER TABLE children ADD COLUMN gender TEXT NOT NULL DEFAULT '' CHECK (gender IN ('Boy','Girl') OR gender = '');
  ALTER TABLE children ADD COLUMN dietary_restrictions TEXT NOT NULL DEFAULT '';
  ALTER TABLE children ADD COLUMN special_needs TEXT NOT NULL DEFAULT '';
  ALTER TABLE children ADD COLUMN relationship TEXT NOT NULL DEFAULT '' CHECK (relationship IN ('Parent','Guardian','Grandparent','Other') OR relationship = '');
  ```

  Guest submissions:
  ```sql
  ALTER TABLE guest_submissions ADD COLUMN safety_ack INTEGER NOT NULL DEFAULT 0 CHECK (safety_ack IN (0,1));
  ```

  **Note:** SQLite `ALTER TABLE ADD COLUMN ... CHECK` with `OR col=''` allows existing rows (`''`) to pass while new inserts must be a valid enum. Alternatively rebuild `children` table with strict `CHECK(gender IN ('Boy','Girl'))` and set `DEFAULT 'Boy'` — pick one, but keep `DEFAULT ''` per "no backfill" (Q8) so old rows don't violate.

- [ ] Edit `db/migrations/20260825030013_add_guest_family_model.down.sqlite` — reverse all above (DROP COLUMN or rebuild tables to pre-task shape). Down must be symmetric; remove the 5 parent cols, 4 child cols, 1 submission col and restore original `CHECK(phone <> '' OR email <> '')` on parents.

- [ ] Regenerate snapshot:
  ```sh
  make db-migrate
  # if local dev DB already applied old version:
  # make db-reset && make db-migrate
  ```
- [ ] Verify `db/structure.sql` contains new columns (`address1`, `gender`, `safety_ack`, etc.) and `CHECK` clauses.

**Verify:**
- [ ] `go test ./db -run TestMigration_GuestFamilyModel` passes (round-trip + blank-name backfill).
- [ ] `sqlite3` spot check: `SELECT sql FROM sqlite_master WHERE name='parents'` shows new cols + tightened CHECK.

---

### Task 2: Repo — structs + persistence

**File:** `internal/repo/guestsubmission/guestsubmission.go`

- [ ] Extend `Parent` struct (line ~24) with:
  ```go
  Address1 string
  Address2 string
  City     string
  State    string
  Zip      string
  ```
- [ ] Extend `Child` struct (line ~33) with:
  ```go
  Gender              string
  DietaryRestrictions string
  SpecialNeeds        string
  Relationship        string
  ```
- [ ] Extend `Submission` struct (line ~43) with:
  ```go
  SafetyAck bool // or SafetyAcknowledged bool
  ```
- [ ] `CreateSubmission` (line ~171):
  - Update `INSERT INTO parents` columns/args to include `address1, address2, city, state, zip`.
  - Update `INSERT INTO children` to include `gender, dietary_restrictions, special_needs, relationship`.
  - Update `INSERT INTO guest_submissions` to include `safety_ack` (bool→0/1).
  - Keep `time.Now().UTC()` handling, tx rollback, `uuid.New().String()` for `public_id`.
- [ ] `ListSubmissions` (line ~243):
  - `SELECT` parent cols: add `address1, address2, city, state, zip`; scan into `Parent`.
  - `SELECT` child cols: add `gender, dietary_restrictions, special_needs, relationship`; scan into `Child`.
  - `SELECT` submission cols: add `safety_ack`; scan `sql.NullInt64`/`bool` → `Submission.SafetyAck`.
- [ ] Keep error wrapping `fmt.Errorf("...: %w", err)`; no new sentinels needed.

**Verify:**
- [ ] `godotenv go test ./internal/repo/guestsubmission -run Test_sqliteRepo_CreateSubmission` (or full package) passes.

---

### Task 3: Controller — payload + validation

**File:** `internal/controllers/guestcheckinv1/guest_submission.go`

- [ ] Extend `parentPayload` (line ~91) with `Address1, Address2, City, State, Zip string` (json tags `address1`, `address2`, `city`, `state`, `zip`).
- [ ] Extend `childPayload` (line ~98) with `Gender, DietaryRestrictions, SpecialNeeds, Relationship string` (json `gender`, `dietary_restrictions`, `special_needs`, `relationship`).
- [ ] Extend `createSubmissionPayload` (line ~86) with `SafetyAck bool` (json `safety_ack`).
- [ ] Update `validateCreateSubmissionPayload` (line ~123):
  - Parent: `phone` required (`TrimSpace != ""` else `errors.New("parent phone is required")`); keep `len<=30` and 7-digit check; email optional (only validate `@` and `len<=254` if non-empty); `address1` required `TrimSpace != ""` + `len<=200`; `city` required `len<=100`; `state` required `len==2` and `regexp.MustCompile("^[A-Za-z]{2}$")` else `state must be 2-letter code`; `zip` required `len<=10` and `regexp.MustCompile("^[0-9]{5}(-[0-9]{4})?$")`; `address2` optional `len<=200`; `address1/city/state/zip` max checks before regex.
  - Child loop: require `gender` in `{"Boy","Girl"}` else `child %d: gender must be Boy or Girl`; require `relationship` in `{"Parent","Guardian","Grandparent","Other"}`; `dietary_restrictions`/`special_needs` optional but `len<=500` each.
  - Top-level: `if !p.SafetyAck { return errors.New("safety acknowledgement is required") }` (covers `FOR SAFETY...` checkbox).
  - Keep existing: parent names `<=100`, phone `<=30`, child names `<=100`, `dob` `len==10` + `time.ParseInLocation("2006-01-02", ..., time.Local)` + future check, `grade` enum, children `1..10`.
- [ ] Update `CreateSubmission` handler (line ~209-223) to map new payload fields into `guestsubmission.Parent`/`Child` and pass `SafetyAck` through to `CreateSubmission` (or set on submission after).

**File:** `internal/controllers/guestcheckinv1/guest_submission_response.go`
- [ ] Extend response `Parent`/`Child`/`Submission` DTOs to include new fields (mirror repo structs; JSON tags match payload). `Submission` gains `SafetyAck bool` (or omit from summary if privacy, but include in full `Submission`).

**Verify:**
- [ ] `godotenv go test ./internal/controllers/guestcheckinv1 -run TestController_CreateSubmission` passes (add cases for phone missing, safety false, bad gender, bad zip).
- [ ] `go fmt ./...` clean.

---

### Task 4: Kiosk UI — form + JS

**File:** `internal/web/static/pages/checkin/index.html` (Parent section ~48-71, Children ~74-90, submit)

- [ ] Parent block: add 5 inputs under phone/email grid:
  - `address1` (required, `maxlength=200`), `address2` (`maxlength=200`), `city` (required `maxlength=100`), `state` (required `maxlength=2` `pattern="[A-Za-z]{2}"`), `zip` (required `maxlength=10` `pattern="\d{5}(-\d{4})?"`). Keep `kiosk-field` + Tailwind `inputClass` styling; grid `sm:grid-cols-2` with `address1` spanning 2 cols (`sm:col-span-2`).
  - Mark required fields with `required` attr.
- [ ] Child row template (`checkin.js:18-42` generates it): add `Gender` select (options `Boy`, `Girl`), `Relationship` select (4 options), `Dietary restrictions` textarea, `Special needs` textarea — all with `maxlength` 500 for textareas, `required` for selects.
- [ ] Before submit button (`~92`): add safety checkbox block:
  ```html
  <label class="flex gap-3 rounded-md border border-slate-200 bg-slate-50 p-3">
    <input id="safety-ack" type="checkbox" required class="mt-1">
    <span class="text-xs font-semibold text-slate-700">FOR SAFETY PURPOSES, I MUST PRESENT SAFETY CLAIM TAG ASSIGNED TO CHILD OR A VALID ID UPON CHECKOUT TO OBTAIN MY CHILD.</span>
  </label>
  ```

**File:** `internal/web/static/pages/checkin/checkin.js`

- [ ] `childRowTemplate()` (line ~18): extend `row.innerHTML` with new controls (gender/relationship selects + 2 textareas). Wire `maxlength`.
- [ ] `buildPayload()` (line ~76): include `parent.address1/2/city/state/zip` (trimmed, state upper-cased), per-child `gender/dietary_restrictions/special_needs/relationship` (trimmed), top-level `safety_ack: document.getElementById('safety-ack')?.checked ?? false`.
- [ ] `validateForm()` (line ~166): mirror server rules — phone required + digits, address1/city/state/zip required + state/zip regex, per-child gender/relationship required, dietary/special `len<=500`, safety checkbox checked else `setKioskError("You must acknowledge the safety policy")` and return false. Keep existing `dob` future check and `dob.max = today` refresh.
- [ ] `resetForm()` (line ~97): reset checkbox + new selects/textareas.

**Verify:**
- [ ] `npx vitest run internal/web/static/pages/checkin/checkin.test.js` passes (add/update tests for new fields, safety unchecked blocks submit, phone empty blocks).

---

### Task 5: Admin view — display new data

**File:** `internal/web/static/pages/admin-guest-entries/admin-guest-entries.js` (renderEntry ~175-238)

- [ ] `renderEntry`: parent chips: add `address1, address2, city, state, zip` (after phone/email). Child rows: display `gender, relationship, dietary_restrictions, special_needs` (with `formatDob` for dob, chip fallback `—` for empty dietary/special). Safety ack: show badge `Safety ack: Yes/No` or checkmark.
- [ ] Keep `textContent` rendering (XSS-safe per review findings). No API change needed if DTOs now carry fields.

**Verify:**
- [ ] `npx vitest run internal/web/static/pages/admin-guest-entries/admin-guest-entries.test.js` passes.

---

### Task 6: Seed & random data

**File:** `cmd/random-data/main.go` (seedGuestSubmissions ~256-325)

- [ ] Generate `address1` (e.g. `fmt.Sprintf("%d %s St", rand.Intn(9999), randomLastName())`), `city` (pick from list), `state` (2-letter sample), `zip` (`%05d`), `gender` (`Boy`/`Girl`), `relationship` (sample 4), `dietary_restrictions`/`special_needs` (empty 70% else lorem). Ensure phone always set (no more email-only branch).

**Verify:**
- [ ] `go run ./cmd/random-data --count 5` writes rows then `sqlite3 database/kids-checkin.db "select phone,state,zip from parents limit 3"` shows new cols.

---

### Task 7: Repo & controller tests

- [ ] `internal/repo/guestsubmission/guestsubmission_test.go`: extend all `CreateSubmission` calls with new fields; add `t.Run` cases: phone empty → error contains `phone` and no orphan parent row; `safety_ack` false → error; bad gender/relationship → CHECK or validation error.
- [ ] `internal/controllers/guestcheckinv1/guest_submission_test.go`: extend `TestController_CreateSubmissionValidation` table (`~148`) with: missing phone, missing address1/city/state/zip, bad state (`"ZZZ"`), bad zip (`"abc"`), bad gender (`"Other"`), bad relationship (`"Cousin"`), `safety_ack:false`, `dietary_restrictions` >500. Add happy path asserting 201 persists new fields. Update existing tests that previously sent `phone:""` to now send valid phone (or they should correctly 400).

**Verify:**
- [ ] `godotenv go test ./internal/repo/guestsubmission ./internal/controllers/guestcheckinv1` green.

---

### Task 8: Migration harness tests

**File:** `db/migration_test.go`

- [ ] Extend `TestMigration_GuestFamilyModel_RoundTrip`: after `up`, assert new columns exist (`pragma_table_info`), `CHECK` enumerations enforced (insert `gender='Other'` → error, `phone=''` → error, `safety_ack=2` → error), and after `down` those columns/indexes/CHECKS gone and blank-name inserts allowed again. Follow existing pattern (~76-91 index checks).

**Verify:**
- [ ] `go test ./db` passes.

---

### Task 9: Full verification

- [ ] `go fmt ./...` clean (`gofmt -l .` empty)
- [ ] `godotenv go test ./...` green (23 packages)
- [ ] `npx vitest run` green (146+ tests)
- [ ] `make db-migrate` re-run shows `db/structure.sql` stable (no diff on second run)
- [ ] Manual spot: `curl -X POST /v1/checkins/guest-submissions` with missing phone → 400; with `safety_ack:false` → 400; with valid payload → 201; `GET /v1/admin/guest-submissions` returns new fields (verify with admin session).

---

## Final Checklist

- [ ] All 9 tasks checked above
- [ ] `db/structure.sql` contains `address1, address2, city, state, zip, gender, dietary_restrictions, special_needs, relationship, safety_ack`
- [ ] `gofmt` clean, both test suites green
- [ ] No new `BEGIN TRANSACTION` in migration files (driver wraps tx)
- [ ] Checkbox text verbatim on form and required server-side

*Load this file in Build Mode via `@docs/2026-08-30-guest-checkin-extra-fields-plan.md` to begin execution.*
