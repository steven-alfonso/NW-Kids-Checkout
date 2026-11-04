# Checkouts conformance suite

This is the black-box contract for the checkouts page. It is meant to keep
passing when the vanilla implementation is replaced by Vue, Svelte, or
anything else.

The suite does **not** read `checkouts.js`, call `window.__test`, or import
page modules. It loads the real HTML and scripts, mocks the `/v1` API with
Playwright, and asserts only the DOM behavior below.

## Run it

First run:

```sh
npx playwright install chromium
```

Then:

```sh
npm run test:conformance
# or
make test-conformance
```

Useful variants:

```sh
# Only one spec
npx playwright test test-support/conformance/checkouts.spec.js

# Only one test
npx playwright test --grep "overdue"

# Run against a replacement page as well as the vanilla page
CHECKOUTS_PAGE_NEXT=/static/pages/checkoutsv1-next/checkouts.html \
  npx playwright test

# Point the vanilla project somewhere else
CHECKOUTS_PAGE=/static/pages/other/checkouts.html npx playwright test
```

`CHECKOUTS_PAGE` defaults to
`/static/pages/checkoutsv1/checkouts.html`. `CHECKOUTS_PAGE_NEXT` is optional;
when set, the same spec runs under a second project named `next`. CI runs the
vanilla project only until a replacement page exists; set `CHECKOUTS_PAGE_NEXT`
in the conformance job when it does.

## How it works

- `server.mjs` serves `internal/web/static` at `/static/*`. It never
  implements `/v1`; an unhandled API request returns a readable 503.
- `fake-api.js` installs a `page.route` mock for `/v1/**` with a mutable
  fixture. It records requests, updates confirmation state like the server,
  applies group filters when the request carries them, and can force poll or
  confirmation failures.
- `checkouts.spec.js` installs `page.clock` at `BASE_TIME`, pauses it before
  navigating, and advances it explicitly, so the 1s tick, 3s poll, and
  `N min ago` labels are deterministic.
- `selectors.js` centralizes the repeated child, time, group, and overdue row
  locators. The single-element IDs in the contract below may be used directly
  in the spec.

`test-support/` is excluded from the Docker build, so none of this ships in
the binary.

The Go handler also injects the auth-aware kebab menu into the page. This suite
serves the static page directly, so those menu links are absent; menu rendering
is covered by the Go tests, not this contract.

## Page contract

The suite may depend on these hooks. If a replacement changes one, change the
contract deliberately and update `selectors.js` — do not add implementation
specific selectors to the spec.

### Board and clock

- `#children-list`
- `#current-time`

### Children

- A child row is a direct `div` child of `#children-list`. Each row contains:
  - `.child-time[data-child-id]`
  - `input.child-confirmed-checkbox[data-child-id]`
  - `[data-confirmed-label][data-confirmed-state="confirmed"|"unconfirmed"]`
- Planning Center identity: `[data-child-id="pc:<planning_center_id>"]`
- Manual identity: `[data-child-id="manual:<public_id>"]`
- Confirm control: the checkbox is wrapped in `[data-confirmed-label]`, and
  its accessible name is `Mark <full name> as confirmed`
- Manual marker: `img[alt="Manual checkin"]`
- Time label: `.child-time` with `N min ago` text

### Filters

- `#search-input`
- `#hide-confirmed-toggle`, wrapped in a `label`
- `#search-toggle-button`
- `#search-controls`
- `#location-group-checkboxes`
- `input[data-lg-id]`
- `#location-group-select-all`

### Overdue

- A row is a direct `div` child of `#overdue-sheet-list` with the same
  `.child-time[data-child-id]`, checkbox, and `[data-confirmed-label]` hooks
  as a main-list row.
- `#overdue-badge`
- `#overdue-sheet`
- `#overdue-sheet-title`
- `#overdue-sheet-list`
- `#overdue-sheet-count`
- `#overdue-sheet-close`
- `#overdue-sheet-backdrop`

### Accessibility

- Badge: `aria-label="<count> overdue checkouts, tap to view"`.
- Sheet: `role="dialog"`, `aria-label="Overdue checkouts"`,
  `aria-hidden="true"` when closed and `"false"` when open, and `inert` while
  closed.
- Search toggle: `aria-label="Toggle search"`,
  `aria-controls="search-controls"`, and `aria-expanded="true|false"`.
- Confirm control: accessible name `Mark <full name> as confirmed`.

### Copy

These strings are asserted exactly and are part of the contract:

- Empty board: `No children called yet`
- No search matches: `No matching children`
- Hide-confirmed empty: `No unconfirmed children`
- Poll error: `Error loading data. Please try again.`
- Badge: `<count> overdue. Tap to view`
- Sheet title: `Overdue >5 min`
- Sheet count: `<count> overdue` / `No overdue`
- Empty sheet: `No overdue checkouts`
- Group select-all: `Select all` / `Deselect all`
- Time label: `N min ago`
- Header/time format: `HH:MM`
- Manual pickup code: `---`

## API contract

The fake API emulates these endpoints:

- `GET /v1/location_groups`
- `GET /v1/checkins/checkouts/` (reads `limit`, `checked_out_after`,
  `location_group_id`, `location_group_name`, `include_unassigned`)
- `PATCH /v1/checkins/:planning_center_id/checked_out_confirmed`
  with `{"confirmed": true|false}`
- `PATCH /v1/checkins/manual-checkins/:public_id/checked_out_confirmed`
  with `{"confirmed": true|false}`

A replacement may choose to filter on the client or send group filters to the
server; the fake API supports both.

## Current behavior that is intentional

`KNOWN-BEHAVIORS.md` lists behaviors that look questionable but are encoded by
this suite. If you decide to change one, change the vanilla page, the
replacement page, and the test in the same commit.

## Adding coverage

Add behavior to `checkouts.spec.js` only when it is observable through the
contract above. Do not assert Tailwind class names, animation classes, internal
function names, or `window.__test`. If a behavior needs a new hook, add the
hook to `selectors.js` and document it here first.
