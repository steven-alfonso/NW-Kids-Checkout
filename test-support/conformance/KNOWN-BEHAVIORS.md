# Current checkouts behavior encoded by the conformance suite

These behaviors are what the vanilla page does today. The conformance suite
pins them so the framework migration does not change behavior accidentally.

Each one is a candidate for a deliberate change. If you change one, change the
vanilla page, the replacement page, and the conformance test in the same
commit.

- **A failed poll clears the main list.** The page replaces `#children-list`
  with `Error loading data. Please try again.` and drops `childrenData`. The
  next successful poll rebuilds the list.
- **An open overdue sheet survives a failed poll.** The sheet keeps its rows
  while the main list is showing the error state. This is deliberate: the
  drawer must not empty under the reader on a transient blip.
- **A successful empty poll empties the open sheet.** An empty payload is
  evidence that the rows are gone, unlike a failed poll, so the drawer clears
  and the counts go to zero.
- **The overdue badge and sheet ignore the group filter.** Filtering the board
  to no groups does not hide overdue children from the badge or the drawer.
- **Exactly five minutes is overdue.** The cutoff is `>= 5 minutes`; 4:59 is
  not overdue.
- **Double-clicking a confirmation sends one PATCH.** The in-flight guard
  prevents a second request. The label may briefly reflect the second click
  until the next poll reconciles it.
- **Some rows cannot be confirmed.** A manual check-in without a `public_id`,
  or a row whose `source` is neither `planning_center` nor `manual`, sends no
  PATCH; the checkbox reverts and an error/warning is logged.
- **Polling does not pause when the tab is hidden.** The 3s interval keeps
  running. A replacement may choose to pause on `visibilitychange`, but that
  would be a behavior change and needs its own decision.
- **The 100-child cap is silent.** The page requests `limit=100` and renders at
  most 100 children, with no “showing the first 100” indicator.
- **Search and hide-confirmed are not stored in the URL.** Only location-group
  selection is written to the query string. Reloading loses search and
  hide-confirmed state.
- **Manual children ignore group filters.** When a group filter is active,
  manual check-ins are always included, even though they have no
  `location_group_id`.
- **“Deselect all” writes `?location_group_id=`.** The empty value is the
  sentinel for “no groups selected”; it renders an empty board, not “all
  groups”. The same sentinel appears after the select-all button is used to
  deselect everything.
- **Unassigned is selected by default.** The initial “all groups” state checks
  the unassigned box along with every location group, so unassigned children
  are visible without an explicit filter.
- **Pill colors are not exposed semantically.** The suite asserts the
  `N min ago` text and `data-confirmed-state`, not `bg-green-500` /
  `bg-yellow-500` / `bg-red-500` / `bg-gray-400`. Color thresholds are
  therefore not part of the cross-implementation contract.
- **Body scroll lock is not part of the conformance contract.** The vanilla
  page locks the background with inline fixed positioning; a replacement may
  use a class or another mechanism. Verify it manually after the migration.
  The existing unit suite covers the vanilla implementation.
- **`location_group_name` can render unfiltered for one poll.** The name is
  resolved against asynchronously loaded groups. On a cold load with only
  `?location_group_name=...`, the first render can show the full list; the
  next poll applies the filter. Tests wait for the filtered state.
- **The local confirmation override lasts 15 seconds.** A successful PATCH is
  reflected immediately, but an unconfirmed poll response is ignored for 15
  seconds so a slow server cannot visibly undo the user's click.
- **The overdue drawer freezes its snapshot.** While open, it does not insert
  children who newly cross the 5-minute threshold. It does keep already-shown
  rows that become confirmed, and drops rows that leave the polled window.
