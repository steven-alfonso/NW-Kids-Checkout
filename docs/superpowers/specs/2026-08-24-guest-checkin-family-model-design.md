# Guest Check-in & Family Model — Design

Date: 2026-08-24
Status: Approved (pending spec review)

## Overview

Overhaul manual check-ins with three new pieces of functionality:

1. A **guest kiosk page** where a parent/guardian enters their contact info and
   one or more children's details. Submissions land in a pending state.
2. An **approval flow** on the existing staff manual-check-ins page. Approved
   submissions create manual check-ins for each child, which continue to appear
   on the manual-check-ins list and the checkout board exactly as today.
3. An **admin copy/paste page** for entering submitted data into Planning
   Center outside this app. Pending → Approved → Entered lifecycle.

The app gains a first-class family model (`parents`, `children`) and
`manual_checkins` gains a reference to `children`.

## Data model

New columns/tables (via migration, mirrored in `db/structure.sql`):

```
parents
─ id            INTEGER PK AUTOINCREMENT
─ first_name    TEXT NOT NULL
─ last_name     TEXT NOT NULL
─ phone         TEXT NOT NULL
─ email         TEXT NOT NULL
─ created_at    DATETIME NOT NULL

children
─ id            INTEGER PK AUTOINCREMENT
─ parent_id     INTEGER NOT NULL FK → parents.id
─ first_name    TEXT NOT NULL
─ last_name     TEXT NOT NULL
─ dob           DATE NOT NULL
─ grade         VARCHAR NOT NULL   -- e.g. "2", "Pre-K", "1st Grade"
─ created_at    DATETIME NOT NULL

guest_submissions
─ public_id     TEXT NOT NULL UNIQUE   -- uuid, for URL-safe identity
─ parent_id     INTEGER NOT NULL FK → parents
─ status        TEXT NOT NULL  -- 'pending' | 'approved' | 'rejected' | 'entered'
─ rejected_at    DATETIME NULL
─ approved_at   DATETIME NULL
─ entered_at    DATETIME NULL
─ created_at    DATETIME NOT NULL
```

Modified table `manual_checkins`:

- Add `child_id INTEGER NULL REFERENCES children(id)`.
- Existing rows keep `NULL`; name columns remain denormalized for display so
  the checkout board is untouched.

### Creation semantics

Each kiosk submission creates, in a single transaction:

- one new `parent`
- `N` new `children` (one per child row entered; `grade` per child)
- one `guest_submissions` row with status `pending`

No deduplication — returning families create fresh records each time. A
submission's children are all children whose `parent_id` equals the
submission's `parent_id`; no join table is needed.

## Pages & flows

### 1. Guest kiosk — `GET /checkin` (any authenticated staff)

- Form: parent first/last name, phone, email; a dynamically growing list of
  child rows (first/last name, DOB, grade). At least one child required.
- Submit: `POST` guest submission → success screen with warm, welcoming copy
  for new parents (e.g., "Welcome! We're so glad you're here. Our team will
  take it from here."). Form then resets to a blank state for the next family.
- **Privacy:** input fields must not show previous guests' entries. Set
  `autocomplete="off"` on the form and each input (and the surrounding
  `form novalidate` semantics), and reset field values explicitly on submit
  so browser/autofill or the app never repopulates a prior family's data.
- **Cache:** the kiosk page and its JSON responses are served with
  `Cache-Control: no-store` so no family data is recoverable from the
  browser (back-forward cache, disk cache, pull-to-refresh).

### 2. Staff manual-checkins page — `/manual-checkins` (existing route)

- **Removed:** the "Add checkin" button and modal.
- **Added:** a "Pending families" section listing pending submissions (parent
  name, children names, submitted time) with **Approve** and **Reject**
  actions. Approving creates one `manual_checkins` row per child
  (`child_id` set, names copied) and sets submission `approved`. Rejecting
  sets the submission `rejected` (kept for record). All existing table,
  check-out, and undo behaviors are unchanged.
- **Least privilege:** this staff list returns **names only** — no parent
  phone/email, no child DOB/grade. Contact and DOB/grade details are
  admin-only (see below).

### 3. Admin copy-in page — `GET /admin/guest-entries` (admin role)

- Lists submissions grouped by status, most actionable first: **Approved
  (needs PC entry)** → **Pending** → **Entered/Rejected** (last two collapsed
  or grayed). Populated from an admin-only endpoint that returns full detail
  (parent contact + child DOB/grade).
- Each entry expands to show the parent block (first, last, phone, email) and
  per-child blocks (first, last, DOB, grade). **Every field value is a
  click-to-copy chip** — clicking copies just that single value to the
  clipboard with visual confirmation.
- "Mark entered" sets status `entered` once PC entry is done.

## API

All guest-submission routes live under the existing `/v1/checkins` group (the
same group as `/v1/checkins/manual-checkins`), following existing controller
conventions.

| Method & path | Auth | Purpose |
|---|---|---|
| `POST /v1/checkins/guest-submissions` | staff | create parent + children + submission (transaction) |
| `GET /v1/checkins/guest-submissions?status=...` | staff | list submissions, **names only** (no contact/DOB/grade) |
| `PATCH /v1/checkins/guest-submissions/:public_id/status` | staff / admin | role-aware transitions (below) |
| `GET /v1/admin/guest-submissions?status=...` | admin | list with full parent contact + child DOB/grade |
| `POST /v1/checkins/manual-checkins` | — | **deleted at the very end** once nothing calls it |

**Role-aware status transitions:**

- staff: `pending → approved`, `pending → rejected`
- admin: everything above plus `approved → entered`
- invalid transitions return `400`.

Request/response shape (snake_case, matching current DTOs):

```json
// POST
{
  "parent": { "first_name": "John", "last_name": "Smith", "phone": "555", "email": "a@b.com" },
  "children": [
    { "first_name": "Timmy", "last_name": "Smith", "dob": "2020-01-01", "grade": "1st Grade" }
  ]
}
```

### Validation & errors

- All fields required; at least one child.
- `dob` valid date, not in the future; `grade` non-empty string.
- Basic email/phone format checks.
- Errors are `fiber.NewError(400, "...")` for payload problems; 404 for
  unknown `public_id`; 400 for invalid status transitions.
- Strict `application/json` content-type check on `PATCH` via
  `mime.ParseMediaType`, matching existing handlers.
- Repos wrap errors with `fmt.Errorf("...: %w", err)`.

## Privacy & security

- **Role-based minimization:** staff endpoints and the approvals page expose
  names only. Parent phone/email and child DOB/grade are only returned by
  `GET /v1/admin/guest-submissions` and only rendered on the admin page
  (`admin` role). URLs use uuid `public_id` (non-enumerable, no DB ids).
- **No personal data in logging:** the existing access logger records
  method/path/status/IP/user-agent only — never request bodies or query
  strings — so submitted PII never reaches logs. Do not add body/query
  logging to the new routes.
- **No browser retention on kiosk:** kiosk page + JSON served with
  `Cache-Control: no-store`; form inputs `autocomplete="off"` and explicitly
  reset after submit.
- **Out of scope (deliberately not included):** CSRF middleware, data
  retention/cleanup jobs, and TLS configuration. Existing JSON content-type
  enforcement plus SameSite=Lax cookies mitigate CSRF today; retention and
  TLS remain deployment/ops concerns for a follow-up.

## Code layout

- `internal/controllers/guestcheckinv1/` — kiosk page + guest-submission API
  (staff list / admin full list / status transitions)
- `internal/repo/parent/` — parent repo
- `internal/repo/child/` — child repo
- `internal/repo/guestsubmission/` — submission repo (create with parent+children
  transactionally; list with nested children; update status)
- `internal/repo/manualcheckin/` — add `child_id` support for the approve write path
- Pages: `internal/web/static/pages/checkin/` (kiosk) and
  `internal/web/static/pages/admin-guest-entries/`
- Wire routes in `internal/controllers/server.go` `registerRoutes`

## Testing

- Repo tests for `parent`, `child`, `guestsubmission` using `db.PrepareTestDB()`
  and testify (`require` for setup, `assert` for values).
- Controller tests: create/approve/reject flows, role-aware transition
  validation (staff cannot mark `entered`; admin can), not-found, auth gating,
  field minimization on the staff list (no contact/DOB/grade).
- JS unit tests (vitest) for the kiosk add/remove-child behavior, post-submit
  form blanking (no carried-over values), and the click-to-copy chips on the
  admin page.
- `make test` and `make build` green. gofmt applied.

## Work sequencing

1. Migration (new tables + `manual_checkins.child_id`)
2. New repos with tests
3. Guest-submission API (staff + admin list, role-aware status) + kiosk page
4. Approval on manual-checkins page + removal of create button
5. Admin copy-in page (full-detail endpoint)
6. Wrap up: run full tests, gofmt
7. Delete now-unused `POST /v1/checkins/manual-checkins` endpoint