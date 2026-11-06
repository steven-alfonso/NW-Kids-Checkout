# Preact-local checkout regressions

These are implementation-local Playwright tests for behavior that the
cross-implementation conformance suite intentionally does not assert. They run
as the `preact` project from `npm run test:app` and load the real page with the
shared fake API.

Covered here:

- overdue sheet `aria-modal`, Tab/Shift+Tab trap, background `inert`, and
  focus return/fallback
- new-child flash lifecycle (`child-card-flash` for four seconds)
- badge jiggle when the overdue count rises
- expanded-search height re-measure when location groups arrive late
- collapsed search controls leave the tab order and accessibility tree, and
  return when expanded
- body scroll-position restoration after closing the sheet
- a poll held open past the next 3s interval is not aborted

Not ported from the deleted vanilla unit suite (and still uncovered):

- websocket update/reconnect behavior
- viewport/orientation changes while the sheet is open beyond the conformance
  viewport test
- the no-JS fallback (Playwright's `javaScriptEnabled: false` does not parse
  `<noscript>` markup in Chromium)
- the full timing of the badge jiggle animation and repeated re-triggers
- missing-id confirmation warning/error logging (the conformance suite asserts
  the visible revert, not the console output)
