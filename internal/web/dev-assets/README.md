# Dev assets

Files in this directory are developer-only debug tools. They are **never
served, referenced, or embedded in production**.

## How it works

- Any file here is served at `GET /static/dev/<filename>`.
- The `internal/web/static` package exposes three helpers:

  - `IsDev()` — true only when the `ENVIRONMENT` env var is `dev`.
  - `DevAssetsDir` — the absolute path to this directory, resolved from the
    package source so it works regardless of the process working directory.
  - `ReadDevAsset(filename)` — reads one file from this directory and guards
    against path traversal (empty, `..`, or nested paths return not-found).

- In `internal/controllers/server.go`, the `/static` middleware handles
  `/static/dev/*`:
  - When `IsDev()` is true, it reads the file via `ReadDevAsset` and serves
    it with `Cache-Control: no-store` (so edits appear on refresh with no
    cache-busting query string). Content type is inferred from the extension.
  - When `IsDev()` is false, every `/static/dev/*` request returns `404`.

- These files live **outside** `internal/web/static/`, so the
  `//go:embed *` directive that builds the production asset bundle does not
  include them. They are absent from the production binary entirely.

## Adding a new dev tool

Two steps, no route registration:

1. Drop the file here, e.g. `debug-panel.js`.
2. Reference it from a page handler. In the page's HTML handler, read the
   embedded HTML, and when `static.IsDev()` is true, insert the `<script>`
   tag just before `</body>`:

   ```go
   // internal/controllers/.../page.go
   html := string(content)
   if static.IsDev() {
       html = strings.Replace(html, "</body>",
           `<script src="/static/dev/debug-panel.js"></script></body>`, 1)
   }
   ```

   Mirror the existing pattern in `checkoutsWeb` in
   `internal/controllers/checkinv1/checkin.go` (which injects
   `/static/dev/preview.js`).

That's it — no new routes, no per-file code. In production the tag is never
injected and the asset 404s.

## Built-in tool: preview.js

`loadPreviewData()` seeds demo checkouts so you can visually validate the
pill colors and the confirm checkbox without waiting for real time.

Usage: open the checkouts page in dev, then run this in the browser console:

```js
loadPreviewData()
```

It:

- blocks the 3-second auto-refresh from overwriting the demo data
  (`API_CALL_BLOCKS.fetchChildrenData = true`),
- seeds four checkouts: green (0 min), a yellow step at 3.9 min, a red step
  at 7.9 min (both step to the next color within ~1s so the `duration-1000`
  transition is visible), and a confirmed one (gray),
- re-renders the list.

Refresh the page to restore real data.

### CLI equivalent: `checkins seed-preview`

The same demo data can be seeded directly in SQLite (useful for API/manual-checkin tests or when not using the browser):

```sh
godotenv ./bin/kids-checkin checkins seed-preview --force
godotenv ./bin/kids-checkin checkins seed-preview --force --db-file database/kids-checkin.db
```

This uses the `checkin`/`manualcheckin` Repos (`DeleteAllCheckins`/`DeleteAllManualCheckins` + `CreateCheckin`/`CreateManualCheckin`) to delete all rows in `checkins` and `manual_checkins` and insert 10 preview rows (5 `demo-*` + 5 `demo-m*`) at the same time offsets as `preview.js`. Requires `--force`. See `README.md` for details.

## Verifying

Dev (`ENVIRONMENT=dev`):

```sh
curl -i http://localhost:3000/static/dev/preview.js
# HTTP/1.1 200 OK
# Content-Type: text/javascript
# Cache-Control: no-store
```

Production (`ENVIRONMENT=production`):

```sh
curl -i http://localhost:3000/static/dev/preview.js
# HTTP/1.1 404 Not Found
```