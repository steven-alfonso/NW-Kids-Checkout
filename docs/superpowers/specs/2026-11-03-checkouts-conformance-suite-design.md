# Checkouts conformance suite

Date: 2026-11-03
Status: Approved design

## Goal

Add a black-box browser test suite for the checkouts page that passes against
the current vanilla JavaScript implementation and against any replacement
implementation, without changing the test file.

This is a regression gate for the framework migration. It is not a replacement
for the existing Go tests or the current white-box Vitest suite; those stay in
place until the vanilla implementation is removed.

## Why the existing tests cannot serve this purpose

Before the spike, `internal/web/static/pages/checkoutsv1/checkouts.test.js`
read `checkouts.js` as text, evaluated it in jsdom, and exposed internals through a
test-only `window.__test` object. Those tests are coupled to the current file
layout, function names, and module-level state. Any Vue/Svelte/Alpine rewrite
breaks them by construction.

The conformance suite must instead drive the page the way a user does and
assert only observable behavior.

## Contract

The suite depends on this stable page contract. It may use any of these hooks,
but nothing below them.

### Board and clock

- `#children-list`
- `#current-time`

### Child identity and state

- `[data-child-id="pc:<planning_center_id>"]`
- `[data-child-id="manual:<public_id>"]`
- Child rows: a direct `div` child of `#children-list` containing
  `.child-time[data-child-id]`, `input.child-confirmed-checkbox[data-child-id]`,
  and `[data-confirmed-label]`
- `[data-confirmed-label][data-confirmed-state="confirmed"|"unconfirmed"]`
- `input.child-confirmed-checkbox` with an accessible name
  `Mark <full name> as confirmed`; the checkbox is wrapped in
  `[data-confirmed-label]`
- Manual marker: `img[alt="Manual checkin"]`
- `.child-time` with the displayed `N min ago` text

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

### Copy

The exact user-visible strings asserted by the suite are part of the contract
and are listed in `test-support/conformance/README.md`.

### Accessibility

- Badge `aria-label` carries the current overdue count.
- Sheet is a `role="dialog"` named `Overdue checkouts`, with
  `aria-hidden` and `inert` reflecting open/closed state.
- Search toggle exposes `aria-label`, `aria-controls`, and `aria-expanded`.
- Confirm controls expose `Mark <full name> as confirmed`.

No production `data-testid` attributes are required. The hooks above already
exist in the vanilla page.

### Explicit non-goals

- Exact Tailwind color classes, animation classes, and layout measurements
- Internal function names, module shape, or `window.__test`
- The Go server, authentication, database, or websocket path
- ETag/cache behavior
- Pixel-level mobile visual-viewport behavior

## Harness

```text
test-support/conformance/
  checkouts.spec.js       # the behavior contract
  fixtures.js             # groups, children, fixed base time
  fake-api.js             # page.route mocks, mutable server state, request log
  selectors.js            # selector helpers and contract documentation
  server.mjs              # static server for internal/web/static
  README.md               # contract, commands, migration workflow
  KNOWN-BEHAVIORS.md      # current behavior that looks questionable

playwright.config.js
```

### Static server

`server.mjs` serves `/static/*` from `internal/web/static`, so the real HTML,
JavaScript, CSS, and icons load. It returns a readable 503 JSON body for any
`/v1/*` request that Playwright did not intercept, making a missing mock
obvious. It sets `Cache-Control: no-store` so tests never see a stale script.

### Fake API

`fake-api.js` installs a single `/v1/**` route and dispatches by pathname:

- `GET /v1/location_groups`
- `GET /v1/checkins/checkouts/` (with optional query string)
- `PATCH /v1/checkins/:planning_center_id/checked_out_confirmed`
- `PATCH /v1/checkins/manual-checkins/:public_id/checked_out_confirmed`

The fake API owns a mutable fixture that behaves like the server:

- Confirmation PATCHes update the fixture by default.
- Tests can make confirmations return an error or skip applying the update.
- Tests can make the next poll fail, add/remove children between polls, or
  change confirmed state to simulate another device.
- Requests are recorded so tests can assert method, path, query, and JSON body.

The list endpoint applies location-group filtering when the request carries
those query parameters. This lets both client-side and server-side filtering
implementations satisfy the same tests.

### Clock

Each test installs `page.clock` at a fixed time and pauses it before
`page.goto`, then uses `page.clock.runFor(...)` to advance the 1s tick and 3s
poll deterministically. The Playwright browser uses `timezoneId: "UTC"` so
clock text is stable.

### Migration projects

`playwright.config.js` reads:

- `CHECKOUTS_PAGE` — defaults to `/static/pages/checkoutsv1/checkouts.html`
- `CHECKOUTS_PAGE_NEXT` — optional; when set, a second Playwright project runs
  the same spec against that page path

During the migration, run both projects and require both to pass. After
cutover, make the new path the default and remove the old project.

## Coverage

### Boot and render

- Loading state is replaced by fetched children.
- An empty initial payload shows `No children called yet` and no badge.
- Newest-first ordering, with a stable `data-child-id` tiebreak for equal
  timestamps. Tied rows keep the same UI order when the API returns them in a
  different order.
- Manual rows show `---` and the manual marker.
- Planning Center rows show their security code.
- Children without `checked_out_at`, or with an invalid timestamp, are not
  rendered.
- A future timestamp renders `0 min ago`, never a negative value.
- Child names, security codes, and location-group names are escaped, not
  injected as HTML.
- The header, badge, search toggle, and dialog expose their expected
  accessible names and roles.

### Clock

- `#current-time` tracks the fake clock in `HH:MM` form, in the browser's
  timezone.
- Time pills advance each second and cross minute boundaries.
- Exactly five minutes is overdue; 4:59 is not.

### Search, hide-confirmed, and groups

- Search matches full name, last name, and security code, case-insensitively
  and after trimming whitespace.
- No-match state appears; clearing restores the full list.
- Hide-confirmed hides confirmed rows and shows the current empty state when
  nothing remains.
- Search, hide-confirmed, and group filters combine and survive a poll.
- Group checkboxes include/exclude groups; manual rows remain visible when a
  group filter is active.
- Unassigned can be included independently.
- Select-all/deselect-all behavior matches the current page.
- Group changes update the URL with `history.replaceState`.
- URL prefiltering works for `location_group_id`, `location_group_name`,
  `include_unassigned`, and the empty `?location_group_id=` state.
- Search and hide-confirmed are not written to the URL.
- The search controls expand and collapse, focus the input on open, and keep
  the active filter across a poll.

### Polling

- A child added by a poll appears; a removed child disappears.
- Ordering remains stable across identical polls.
- Focus inside a child row survives a poll.
- The main list scroll position survives a poll, including when a new child
  arrives.
- A failed poll shows the current error state; a later successful poll recovers.
- Malformed JSON and an aborted request take the same error path and recover.
- A failed location-groups request does not block the board.
- The initial request carries `limit=100`.
- `limit` from the URL is forwarded; `checked_out_after` is forwarded.
- Window parameters survive a group-filter change.
- At most 100 rows are rendered; the current page has no truncation indicator.

### Confirmation

- Checking a Planning Center row sends
  `PATCH /v1/checkins/<id>/checked_out_confirmed` with
  `{"confirmed":true}`.
- Checking a manual row uses the manual endpoint, with a URL-encoded
  `public_id`.
- A manual row without a `public_id`, or a row with an unknown `source`, sends
  no request and reverts the checkbox.
- The checkbox and `data-confirmed-state` update optimistically before the
  response.
- A failed PATCH reverts the checkbox and state, for both checking and
  unchecking.
- A poll that returns confirmed state keeps the row checked.
- A poll that returns unconfirmed state during the 15s override keeps the row
  checked; after the override expires, the next poll clears it.
- Unchecking sends `{"confirmed":false}`.
- A confirmation made elsewhere (fixture changed directly) appears on the next
  poll.
- Double-clicking the confirm control while a PATCH is in flight sends exactly
  one PATCH.

### Overdue

- The badge appears with the current count/text and hides at zero.
- Opening the sheet shows newest-first overdue rows, group labels, and count.
  If the group list loads after the sheet opens, the labels update on the next
  tick.
- Close by button, backdrop, and Escape.
- Focus returns to the opener; `aria-hidden` and `inert` toggle.
- Focus inside an open sheet survives a poll.
- Body scroll lock is intentionally not part of this contract: the vanilla page
  locks it with inline fixed positioning, but that is an implementation detail.
  Check it manually after the migration; the existing unit suite covers it
  today.
- Confirming a row while the sheet is open keeps the row visible until close.
- A child crossing the threshold while the sheet is open is not inserted until
  close/reopen.
- A failed poll leaves the open sheet's rows intact.
- A successful empty poll empties the open sheet and zeroes the counts.
- The badge and sheet are not filtered by group selection.
- Confirmed overdue children are excluded from the badge and sheet.
- At a phone viewport, the sheet and its close button stay inside the viewport.

## Known behavior list

These are encoded as current behavior and documented in
`test-support/conformance/KNOWN-BEHAVIORS.md`. They are not changed by this
work:

- A failed poll clears the main list and shows an error; an open overdue sheet
  retains its rows.
- Polling does not pause when the tab is hidden.
- The 100-child cap is silent.
- Search and hide-confirmed are not stored in the URL.
- Manual children ignore group filters.
- Deselecting every group navigates to `?location_group_id=`, which renders an
  empty board.
- Pill color thresholds are not exposed semantically.
- `location_group_name` URL filtering can briefly render the unfiltered list
  until the next poll, because the group list resolves asynchronously.

If any of these should change, change both implementations and the conformance
test in the same commit.

## Tooling and CI

- Add `@playwright/test` pinned to an exact version.
- Commit `package-lock.json` and remove it from `.gitignore`; reproducible
  Playwright and Tailwind versions depend on it.
- Add `npm run test:conformance` → `playwright test`.
- Add a CI `conformance` job:
  - Node 22
  - `npm ci`
  - `npx playwright install --with-deps chromium`
  - `npm run test:conformance`
- Keep `make test` as the fast Go + Vitest suite.
- Add `make test-conformance`.
- Update `README.md` and `AGENTS.md`.

No Docker change: Playwright is test-only.

## Acceptance criteria

- `npm run test:conformance` passes against the current vanilla page.
- The same spec passes against a second page path by changing
  `CHECKOUTS_PAGE`, with no spec edits.
- `npm test`, the Go suites, `gofmt`, and `go vet` remain green.
- No production behavior changes and no production `data-testid` attributes.
- The contract and migration workflow are documented.
