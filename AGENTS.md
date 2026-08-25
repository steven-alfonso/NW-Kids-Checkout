# AGENTS.md

This file guides coding agents working in this repo. Keep changes small, follow existing patterns, and prefer clarity over cleverness.

## Build, run, lint, test

### Go app (Makefile-driven)
- List targets: `make` (help output from Makefile)
- Build binary: `make build`
- Build with embedded assets: `ASSET_BUILD=1 make build`
- Run API server: `make web`
- Run API server with live reload: `make web-lr`
- Run checkout fetcher worker: `make checkout-fetcher`

### Database tasks
- Reset DB: `make db-reset`
- Run migrations: `make db-migrate`
- Create migration: `make db-new-migration NAME=<migration_name>`
- Seed DB: `make db-seed`

### Tests
- Run all tests: `make test` (runs `godotenv go test ./...`)
- Run a single package: `godotenv go test ./internal/repo/checkin`
- Run a single test: `godotenv go test ./internal/repo/checkin -run Test_sqliteRepo_ListCheckins`
- Run a subtest: `godotenv go test ./internal/repo/checkin -run Test_sqliteRepo_ListCheckins/filter_by_location_ID`
- Alternative without env loading (if not needed): `go test ./internal/repo/checkin -run Test_sqliteRepo_ListCheckins`

### Frontend assets (Tailwind)
- Install deps: `npm install`
- Watch CSS: `npm run watch:css`
- Build CSS: `npm run build:css`

### Lint/format
- Lint: no dedicated lint config found.
- Format: use `gofmt` (Go standard). Use `go fmt ./...` before committing when editing Go code.

## Repo structure and key tech

- Language: Go 1.25 (see `go.mod`).
- Web framework: Fiber (`github.com/gofiber/fiber/v2`).
- CLI: `urfave/cli/v3`.
- Database: SQLite with `squirrel` query builder.
- Tests: `testify` (`assert`, `require`).
- Asset pipeline: Tailwind CLI; assets embedded by `cmd/assets`.

## Project layout

- Entry point: `main.go` calls `internal/cmd` to build the CLI.
- CLI commands: `internal/cmd/*` (e.g., `apiserver`, `checkout-fetcher`).
- HTTP controllers: `internal/controllers/*` (versioned packages like `checkinv1`).
- Repos and DB access: `internal/repo/*` using `squirrel` and `context.Context`.
- DB helpers: `internal/db/*` (test DB prep, DB init).
- Static web assets: `internal/web/static` (embedded FS via `cmd/assets`).
- Migrations: `db/migrations` and schema snapshots in `db/structure.sql`.
- Domain helpers/constants: `internal/static`.

## Coding conventions

### Imports
- Group imports in the standard Go order: stdlib, local `kids-checkin/...`, third-party.
- Keep `go fmt`/`gofmt` formatting; avoid manual alignment changes.

### Formatting
- Use `gofmt`-style formatting for all Go code.
- Keep lines readable; prefer wrapping complex calls rather than long single lines.

### Types and naming
- Use Go naming conventions (CamelCase for exported, lowerCamel for unexported).
- HTTP handlers are methods on controller structs (e.g., `Controller` in `internal/controllers/*`).
- Use explicit, descriptive names in tests (see `Test_sqliteRepo_*` patterns).
- Prefer `Filter` structs to pass optional query params (pattern used in repos).
- Name interfaces by role (e.g., `Repo`, `Storer`) and keep method sets minimal.
- Keep DTOs close to controllers; use conversion helpers for repo types.

### Error handling
- Wrap lower-level errors with context using `fmt.Errorf("...: %w", err)` in repos and helpers.
- Use sentinel errors for domain conditions (e.g., `repo.ErrNotFound`) and check with `errors.Is`.
- For HTTP endpoints, return `fiber.NewError(status, message)` for client-visible failures.
- Prefer early returns for error paths; keep happy path readable.
- Return `fiber.ErrInternalServerError` only when the handler cannot provide a better message.

### Context usage
- Pass `context.Context` to repo methods and DB queries (pattern in `internal/repo/*`).
- Use `t.Context()` in tests for DB operations.
- Avoid `context.Background()` inside request handlers unless you explicitly need to decouple from the request.

### Time handling
- Store timestamps in UTC when persisting to DB (`time.Now().UTC()`), as seen in repos.
- When converting optional times for DB, use `*time.Time` with UTC values.
- Keep time comparisons in UTC to avoid mixed-zone bugs.

### Database and repo patterns
- Repos use `squirrel` builders; keep query assembly readable and consistent.
- Favor `Filter` fields + conditional query additions rather than string concatenation.
- Avoid implicit joins; track joined tables when needed (see `joinedTables` pattern).
- Use `QueryContext`/`ExecContext` everywhere and close rows promptly.
- For nullable DB times, scan to `sql.NullTime` and convert to `time.Time`.

### Web/HTTP patterns
- Controllers register routes in `RegisterRoutes` methods.
- Use `session.Store` for auth middleware and session state.
- For JSON endpoints, validate content type with `mime.ParseMediaType` and `fiber.MIMEApplicationJSON`.
- Prefer `c.JSON(...)` for API responses and `c.SendStream(...)` for embedded HTML.
- Websocket endpoints use `github.com/gofiber/contrib/websocket` and log connect/read/write issues.

### Logging
- Prefer structured logging (`log/slog`) where present; include key fields (IDs, params).
- For CLI errors, return the error and let `main` handle exit messaging.

### Tests
- Use `testify/require` for setup failures and `assert` for value checks.
- Prefer subtests with `t.Run` for variants.
- Use `db.PrepareTestDB()` helper for DB-backed tests and register cleanup with `t.Cleanup`.
- Repo tests may use `TestMain` to initialize a shared test DB.
- Keep test data setup explicit; avoid hidden fixtures.

### Database migrations
- Generate migrations via `make db-new-migration NAME=<migration_name>`.
- Update both up/down files and keep changes small and reversible.
- Run `make db-migrate` to apply migrations and refresh `db/structure.sql`.

### Frontend conventions
- HTML templates live under `internal/web/static/pages`.
- Page JS lives alongside the HTML in `internal/web/static/pages`.
- Shared JS libraries live in `internal/web/static/js`.
- Images and icons live in `internal/web/static/img`.
- Regenerate Tailwind output when UI changes touch utility classes.

### Assets and static files
- Static pages are loaded from `internal/web/static` and embedded FS.
- When adding assets, check the embedding pipeline in `cmd/assets`.
- Tailwind output lives at `internal/web/static/css/tailwind.css`.

### Dev-only assets (debug tooling)
- Dev/debug tools live in `internal/web/dev-assets/` and are served at `/static/dev/*` **only when `ENVIRONMENT=dev`** (via `static.IsDev()`); in production they 404 and are not embedded into the binary. See `internal/web/dev-assets/README.md`.
- Add a new tool by dropping the file in `internal/web/dev-assets/` and referencing it from a page handler (inject the `<script>` tag in the page's HTML handler when `static.IsDev()`, mirroring the `checkoutsWeb` preview.js pattern).
- Helpers: `static.DevAssetsDir`, `static.ReadDevAsset(filename)`, `static.IsDev()`.

## Environment and secrets

- Local configuration is loaded via `godotenv` in Makefile targets; keep `.env` out of commits.
- Do not log or commit secrets; use `.env` and `godotenv` when running locally.
- Runtime configuration is typically via env vars (see `internal/cmd/*` flags).

### OpenTelemetry (tracing + metrics)
- Setup lives in `internal/telemetry` (`Setup`, used by both the API server and the checkout-fetcher worker).
- Disabled (no-op, zero overhead) unless `OTEL_EXPORTER_OTLP_ENDPOINT` is set; otherwise traces and metrics export via OTLP gRPC. Standard `OTEL_*` env vars control endpoint, headers, sampling, timeout.
- HTTP tracing uses `github.com/gofiber/contrib/otelfiber` (the v1 module targets Fiber v2); per-request HTTP metrics are in `internal/controllers/middleware/otelmetrics.go` and are the single source of HTTP metrics (otelfiber's own meter is pointed at a reader-less provider in `registerCoreMiddleware`). Middleware order matters: recover runs innermost so panics become 500s that the tracing/metrics/access-log middleware can observe.
- DB connections run through an OTel-instrumented sqlite driver (`github.com/XSAM/otelsql`). When telemetry is enabled, open the DB with `db.InitDBInstrumented(dsn, tel.TracerProvider, tel.MeterProvider)` — it registers the driver and opens in one step, so ordering cannot get silently wrong; plain `db.InitDB` always uses the raw driver. Driver registration is process-global (`sync.Once`): the first registration wins. The test helper (`db.PrepareTestDB`) and the Fiber session store use unwrapped raw drivers.
- Logs correlate with traces via `logger.NewTraceHandler` in `internal/logger` — every slog record written with an active span context gets `trace_id`/`span_id` attrs.

## Cursor/Copilot rules

- No Cursor rules found in `.cursor/rules/` or `.cursorrules`.
- No Copilot instructions found in `.github/copilot-instructions.md`.

## Agent workflow tips

- Prefer minimal, focused diffs; avoid refactors unless requested.
- Keep existing behavior stable; match surrounding style.
- If adding new commands or scripts, update this file.
- Do not commit code. Let the user complete the action.
