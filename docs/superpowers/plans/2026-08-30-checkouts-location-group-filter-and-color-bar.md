# Checkouts Location Group Filter + Color Bar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add location-group multi-select (with Select All) under page settings on `/v1/checkins/checkouts` and a deterministic left color bar per checkout using Paul Tol's muted palette (gray reserved for unassigned), consistent by `location_group.id`, with URL as source of truth (Option A: clear = all).

**Architecture:** Extend checkouts JSON (`internal/controllers/checkinv1/checkin.go:144`) to include `location_group_id` per checkin via LEFT JOIN `locations` in `internal/repo/checkin/checkin.go:61`. Frontend `internal/web/static/pages/checkoutsv1/checkouts.js:152` syncs checkbox state to URL (`?location_group_id=&include_unassigned=1`), mutates URL via `history.replaceState`, and renders left bar via deterministic `locationGroupId -> color` function. Server supports multi-group filtering (`location_group_id` repeated/comma + `include_unassigned`).

**Tech Stack:** Go 1.25, Fiber v2, squirrel, SQLite, Tailwind CSS, vanilla JS + morphdom, Vitest + jsdom.

**Spec:** User prompt 2026-08-30 + `docs/superpowers/specs/2026-08-24-guest-checkin-family-model-design.md:103` (left-bar reference)

## Global Constraints

- `go fmt ./...` before commit
- `make test` must pass (godotenv go test ./...)
- `make build` green, assets via `cmd/assets/main.go:1`
- Use `context.Context` for repo, wrap errors `fmt.Errorf("...: %w", err)`, HTTP via `fiber.NewError`
- Store times UTC (`time.Now().UTC()`), `sql.NullTime` for nullable scans
- Follow existing `Filter` pattern (`internal/repo/checkin/checkin.go:14`)
- No new lint config; follow existing style

---

## File Structure

**Modified:**
- `internal/repo/checkin/checkin.go:30` — add `LocationGroupID *int64` to Checkin + LEFT JOIN select
- `internal/controllers/checkinv1/checkin.go:451` — add `LocationGroupID *int64` to DTO, handle multi-ids + include_unassigned
- `internal/web/static/pages/checkoutsv1/checkouts.html:144` — add location-group filter UI inside `#search-controls`
- `internal/web/static/pages/checkoutsv1/checkouts.js:1` — palette, color fn, URL sync, checkbox rendering, bar rendering
- `internal/web/dev-assets/preview.js:1` — seed location_group_id demos

**Tests:**
- `internal/repo/checkin/checkin_test.go`
- `internal/controllers/checkinv1/checkin_test.go`
- `internal/web/static/pages/checkoutsv1/checkouts.test.js`
- `internal/web/static/pages/checkoutsv1/preview.test.js`

---

### Task 1: Backend — expose `location_group_id` + multi-filter + include_unassigned

**Files:**
- Modify: `internal/repo/checkin/checkin.go:14-41,61-188`
- Modify: `internal/controllers/checkinv1/checkin.go:272-353,451-461`
- Test: `internal/repo/checkin/checkin_test.go`, `internal/controllers/checkinv1/checkin_test.go`

**Interfaces:**
- Consumes: `locations` join
- Produces: `checkin.Checkin.LocationGroupID *int64`, `Checkin.LocationGroupID *int64` JSON `location_group_id`, `Filter.LocationGroupIDs []int64`, `Filter.IncludeUnassigned bool`

- [ ] **Step 1: Write failing repo test**

```go
func Test_sqliteRepo_ListCheckins_includes_location_group_id(t *testing.T) {
    db := db.PrepareTestDB(t)
    // insert location_groups id=10, locations id=1 with group 10, checkin with location_id=1
    // ListCheckins -> assert checkin.LocationGroupID != nil && *id==10
    // unassigned: location_group_id NULL -> LocationGroupID == nil
}
func Test_sqliteRepo_ListCheckins_filter_by_multiple_location_group_ids(t *testing.T) {
    // insert 2 groups, 2 locations, 2 checkins
    // Filter{LocationGroupIDs: []int64{10,20}} -> 2 results
    // Filter{IncludeUnassigned:true} -> unassigned rows
}
```

Run: `godotenv go test ./internal/repo/checkin -run Test_sqliteRepo_ListCheckins_includes_location_group_id -v`
Expected: FAIL compile `LocationGroupID undefined`

- [ ] **Step 2: Add field to repo struct**

```go
type Filter struct {
    ID int64
    PlanningCenterID string
    LocationID int64
    EventID int64
    LocationName string
    LocationGroupID int64
    LocationGroupIDs []int64
    IncludeUnassigned bool
    LocationGroupName string
    FirstName string
    LastName string
    CheckedOutAtBefore time.Time
    CheckedOutAtAfter time.Time
    Limit int
    Recent bool
}
type Checkin struct {
    ID int64
    PlanningCenterID string
    LocationID int64
    EventID int64
    FirstName string
    LastName string
    SecurityCode string
    CheckedOutAt time.Time
    FetchedAt time.Time
    CheckedOutConfirmedAt time.Time
    LocationGroupID *int64
}
```

- [ ] **Step 3: Update ListCheckins to LEFT JOIN and select location_group_id**

```go
builder := squirrel.Select(
    "checkins.id", "checkins.planning_center_id", "checkins.location_id",
    "checkins.first_name","checkins.last_name","checkins.security_code",
    "checkins.checked_out_at","checkins.fetched_at","checkins.checked_out_confirmed_at",
    "locations.location_group_id",
).From("checkins").LeftJoin("locations ON locations.id = checkins.location_id")
```

Handle existing joinedTables logic: if `!joinedTables["locations"]` then LeftJoin, else skip. Add filter branches:
```go
if len(filter.LocationGroupIDs) > 0 && filter.IncludeUnassigned {
    builder = builder.Where(squirrel.Or{
        squirrel.Eq{"locations.location_group_id": filter.LocationGroupIDs},
        squirrel.Eq{"locations.location_group_id": nil},
    })
} else if len(filter.LocationGroupIDs) > 0 {
    builder = builder.Where(squirrel.Eq{"locations.location_group_id": filter.LocationGroupIDs})
} else if filter.IncludeUnassigned {
    builder = builder.Where(squirrel.Eq{"locations.location_group_id": nil})
} else if filter.LocationGroupID > 0 {
    if !joinedTables["locations"] { builder = builder.LeftJoin(...) }
    builder = builder.Where(squirrel.Eq{"locations.location_group_id": filter.LocationGroupID})
}
```
Scan: `var lgID sql.NullInt64` then `if lgID.Valid { checkin.LocationGroupID = &lgID.Int64 }`

- [ ] **Step 4: Update controller DTO and buildFilter**

```go
type Checkin struct {
    PlanningCenterID string `json:"planning_center_id"`
    LocationID int64 `json:"location_id"`
    LocationGroupID *int64 `json:"location_group_id"`
    PublicID string `json:"public_id"`
    FirstName string `json:"first_name"`
    LastName string `json:"last_name"`
    SecurityCode string `json:"security_code"`
    CheckedOutAt *time.Time `json:"checked_out_at"`
    CheckedOutConfirmedAt *time.Time `json:"checked_out_confirmed_at"`
    Source string `json:"source"`
}
func repoCheckinToOutput(c checkin.Checkin) Checkin {
    return Checkin{ PlanningCenterID: c.PlanningCenterID, LocationID: c.LocationID, LocationGroupID: c.LocationGroupID, ... }
}
```

Update `buildFilter`:
```go
if c.Query("include_unassigned") == "1" || c.Query("include_unassigned") == "true" {
    filter.IncludeUnassigned = true
}
// parse repeated/comma location_group_id
var ids []int64
// use c.Request().URI().QueryArgs() or c.Context().QueryArgs() to get all
// fallback: single c.Query with split
for _, v := range splitCommaOrGetAll(c, "location_group_id") {
    parsed, err := strconv.ParseInt(strings.TrimSpace(v),10,64); if err==nil && parsed>0 { ids = append(ids, parsed) }
}
if len(ids)==1 { filter.LocationGroupID = ids[0] }
filter.LocationGroupIDs = ids
```

- [ ] **Step 5: Verify**

Run: `godotenv go test ./internal/repo/checkin ./internal/controllers/checkinv1 -v`

- [ ] **Step 6: Commit**

```bash
git add internal/repo/checkin/checkin.go internal/controllers/checkinv1/checkin.go internal/repo/checkin/checkin_test.go internal/controllers/checkinv1/checkin_test.go
git commit -m "feat: expose location_group_id and multi-filter with include_unassigned"
```

---

### Task 2: Frontend palette + deterministic color mapping

**Files:**
- Modify: `internal/web/static/pages/checkoutsv1/checkouts.js:1`
- Test: `internal/web/static/pages/checkoutsv1/checkouts.test.js:1`

**Interfaces:**
- Consumes: `child.location_group_id` (number|null)
- Produces: `getLocationGroupColor(locationGroupId)` -> hex, `PAUL_TOL_MUTED`, `GRAY_UNASSIGNED`

- [ ] **Step 1: Write failing JS test**

```js
it('maps location_group_id deterministically to Paul Tol muted, gray for null', () => {
  const w = loadWindow();
  expect(w.getLocationGroupColor(null)).toBe('#9CA3AF');
  expect(w.getLocationGroupColor(undefined)).toBe('#9CA3AF');
  const c1 = w.getLocationGroupColor(1);
  expect(c1).toBe(w.getLocationGroupColor(1));
  expect(w.PAUL_TOL_MUTED).not.toContain('#9CA3AF');
  expect(w.getLocationGroupColor(2)).not.toBe(c1);
});
```

Run: `npm test -- checkouts.test.js` Expected: FAIL not defined

- [ ] **Step 2: Implement palette**

```js
const GRAY_UNASSIGNED = '#9CA3AF';
const PAUL_TOL_MUTED = ['#332288','#117733','#44AA99','#88CCEE','#DDCC77','#CC6677','#AA4499','#882255'];
function getLocationGroupColor(locationGroupId) {
  if (locationGroupId == null) return GRAY_UNASSIGNED;
  const id = Number(locationGroupId);
  if (!Number.isFinite(id)) return GRAY_UNASSIGNED;
  const idx = Math.abs(id - 1) % PAUL_TOL_MUTED.length;
  return PAUL_TOL_MUTED[idx];
}
```

Expose in `__test` or global.

- [ ] **Step 3: Run test PASS**

Run: `npm test -- internal/web/static/pages/checkoutsv1/checkouts.test.js -v`

- [ ] **Step 4: Commit**

```bash
git add internal/web/static/pages/checkoutsv1/checkouts.js internal/web/static/pages/checkoutsv1/checkouts.test.js
git commit -m "feat: add Paul Tol muted color mapping for location groups"
```

---

### Task 3: Page settings UI — URL-synced multi-select + Select All (Option A)

**Files:**
- Modify: `internal/web/static/pages/checkoutsv1/checkouts.html:144-172`
- Modify: `internal/web/static/pages/checkoutsv1/checkouts.js:6-21,152-180,349-444,618-692`

**Interfaces:**
- Consumes: `GET /v1/location_groups`, URLSearchParams `location_group_id` + `include_unassigned`
- Produces: `fetchLocationGroups()`, `renderLocationGroupSettings()`, `getSelectedFromURL()`, `pushURLFromSelection()`, `syncLocationGroupUIFromURL()`

- [ ] **Step 1: Write failing JS tests**

```js
it('syncs checkbox state from URL and Select All clears URL', () => {
  const w = loadWindow({url:'http://localhost/?location_group_id=1&include_unassigned=1'});
  // after fetchLocationGroups mocked, checkboxes for 1 checked, unassigned checked
  // click Select All -> URL has no location_group_id nor include_unassigned
});
```

- [ ] **Step 2: HTML addition**

```html
<div id="location-group-filter" class="mt-3 border-t border-slate-200 pt-3">
  <div class="flex items-center justify-between mb-2">
    <span class="text-sm font-medium text-slate-700">Location groups</span>
    <button id="location-group-select-all" type="button" class="text-xs font-semibold text-blue-600 hover:text-blue-700">Select all</button>
  </div>
  <div id="location-group-checkboxes" class="flex flex-wrap gap-2"></div>
</div>
```

Inside `.search-controls-panel` after hide-confirmed label (`checkouts.html:168`).

- [ ] **Step 3: JS state + URL helpers + rendering + wiring**

Implement:
- `getSelectedFromURL() -> {ids:Set<number>, includeUnassigned:boolean}`
- `pushURLFromSelection(idsSet, includeUnassigned)` -> rebuild URLSearchParams preserving `checked_out_after`,`limit`, delete old group keys, append new, `history.replaceState(null,'', '?' + params)`, then `fetchChildrenData()` if server filtering OR `updateUI()` if client filtering.
- `renderLocationGroupSettings(groups)` -> innerHTML with `<label><input type=checkbox data-lg-id="..."> <span style="background:color" class="inline-block h-3 w-3 rounded-sm"> Name`
- `fetchLocationGroups()` -> fetch `/v1/location_groups`, call render, sync from URL, disable Select All when already showing all (no group params).
- On `DOMContentLoaded`, call `fetchLocationGroups()`.
- Wire `change` on `data-lg-id` checkboxes -> collect checked ids + unassigned, call `pushURLFromSelection`.
- Wire `click` on `#location-group-select-all` -> clear group params (remove all `location_group_id` and `include_unassigned`) -> replaceState -> check all boxes visually -> fetchChildrenData.
- Update `getChildSignature` to include `location_group_id`, `lastListSignature` includes `window.location.search`.
- Update `fetchChildrenData` (`checkouts.js:370`) to forward `include_unassigned` param as well as repeated/comma `location_group_id` (already forwards single, add `getAll` handling + `include_unassigned`).

- [ ] **Step 4: Run tests PASS**

Run: `npm test -- checkouts.test.js`

- [ ] **Step 5: Commit**

```bash
git add internal/web/static/pages/checkoutsv1/checkouts.html internal/web/static/pages/checkoutsv1/checkouts.js internal/web/static/pages/checkoutsv1/checkouts.test.js
git commit -m "feat: add location group filter UI synced to URL with Select All"
```

---

### Task 4: Left color bar on each checkout card + preview update

**Files:**
- Modify: `internal/web/static/pages/checkoutsv1/checkouts.js:494-516`
- Modify: `internal/web/static/pages/checkoutsv1/checkouts.html:17` (optional CSS)
- Modify: `internal/web/dev-assets/preview.js:1`
- Test: `internal/web/static/pages/checkoutsv1/checkouts.test.js`, `internal/web/static/pages/checkoutsv1/preview.test.js`

**Interfaces:**
- Consumes: `getLocationGroupColor`
- Produces: bar markup inside `renderChildren`

- [ ] **Step 1: Failing tests**

```js
it('renders color bar matching location_group_id', () => {
  const w = loadWindow();
  const html = w.renderChildren([{first_name:'A',last_name:'B', location_group_id:1, planning_center_id:'1', checked_out_at_ms: Date.now()}], Date.now());
  expect(html).toContain('background-color:'+w.getLocationGroupColor(1));
  const htmlNull = w.renderChildren([{first_name:'C',last_name:'D', location_group_id:null, planning_center_id:'2', checked_out_at_ms: Date.now()}], Date.now());
  expect(htmlNull).toContain('#9CA3AF');
});
it('preview seeds location_group_id', () => {
  const w = loadWindow(); w.loadPreviewData(); expect(w.__test.getChildrenData()[0].location_group_id).toBeDefined();
});
```

- [ ] **Step 2: Implement bar**

```js
const barColor = getLocationGroupColor(child.location_group_id);
return `
  <div class="bg-white rounded-lg shadow-[0_0_10px_rgba(0,0,0,0.25)] flex overflow-hidden${flashClass}">
    <div style="background-color:${barColor}; width:6px; flex-shrink:0" aria-hidden="true"></div>
    <div class="flex-1 py-2.5 px-4 flex flex-col justify-center">
      <div class="font-bold text-gray-800 text-2xl mb-0">${name}${starMarkup}</div>
      <div class="flex justify-between items-center">...</div>
    </div>
  </div>`;
```

Ensure flash animation still on outer flex container, time pill logic unchanged.

- [ ] **Step 3: Update preview.js**

Seed `location_group_id` on demo data: pattern 1,2,1,null across 8 entries so bars visible with gray.

- [ ] **Step 4: Run tests**

Run: `npm test -- checkouts.test.js preview.test.js` and `godotenv go test ./...`

- [ ] **Step 5: Commit**

```bash
git add internal/web/static/pages/checkoutsv1/checkouts.js internal/web/static/pages/checkoutsv1/checkouts.html internal/web/dev-assets/preview.js internal/web/static/pages/checkoutsv1/checkouts.test.js internal/web/static/pages/checkoutsv1/preview.test.js
git commit -m "feat: add left color bar per location group with preview"
```

---

### Task 5: Integration verification

- Run `go fmt ./...`, `npm run build:css` if needed, `make test`, `make build`.
- Manual verify: `make web` open `/v1/checkins/checkouts`, gear → groups + Select All + URL sync, bar colors stable on reload, gray for unassigned.

