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
- Reset DB (empty schema): `make db-reset`
- Build dev DB (schema + Planning Center reference topology): `make db-init`
- Run migrations: `make db-migrate`
- Create migration: `make db-new-migration NAME=<migration_name>`
- Add random per-visit check-in data: `make random-data`

### Tests
- Run all tests: `make test` (runs `godotenv go test ./...`, then the same with
  `-tags dev`, then `npm test`)
- Run all tests under `-trimpath`: `make test-trimpath`
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

- Language: Go 1.27 (see `go.mod`).
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
- Two packages are named `db`: the Go package `internal/db/*` (DB init, the
  shared `--db-file` flag, schema snapshot reader, test DB prep) and the
  `kids-checkin/db` package made of `db/*_test.go`, which is where the snapshot
  drift test lives. The `db/` directory is otherwise data: migrations and
  `structure.sql`.
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

### Database paths
- The database file path is defined **once**, as `db.DefaultDBFile`, and must
  match `KIDS_CHECKIN_DB_FILE` in the Makefile. Commands get it from
  `db.DBFileFlag()`; never spell out a path or define your own `db-file` flag.
- Precedence is `--db-file` > `$DB_FILE` > `db.DefaultDBFile`. Do not set
  `DB_FILE` in `.env` — the default already matches the Makefile, and an extra
  copy is what let the two drift apart before. The Dockerfile's
  `DB_FILE=/data/kids-checkin.db` is a deliberate override for the container
  volume.
- Five tests in `internal/cmd/dbfile_test.go` guard this. Keep all five honest:
  the first originally asserted nothing at all, and the third could not see a
  whole class of regressions:
  - `TestDefaultDBFileMatchesMakefile` ties the constant to the Makefile.
  - `TestEnvExampleDoesNotPinDBFile` keeps `.env.example` free of `DB_FILE`.
    `.env` itself is gitignored, so a stale `DB_FILE` there is still invisible;
    only the tracked example can be checked.
  - `TestEveryDBCommandUsesTheSharedFlag` walks the live command tree, so it
    cannot see `cmd/random-data` (a separate `main`) or a command that opens the
    database without declaring a flag at all.
  - `TestDbFileFlagIsNeverRedefined` and `TestDBInitCallSiteDoesNotHardcodePath`
    are the structural backstops: an AST rule forbidding any `StringFlag{Name:
    "db-file"}` literal, and any `db.InitDB("literal")`. They need no list of
    commands, which is what makes them complete where the tree walk is not.
  - When adding a guard on AST or source text, check that it actually fires.
    `assert.NotRegexp` takes the string to match as its third argument, and
    `ast.BasicLit.Value` for a string keeps its quotes — both mistakes produce a
    test that passes forever.

### Schema
- Migrations are applied by the `migrate` CLI, **not** by the application
  binary. `db/structure.sql` is the snapshot `make db-migrate` generates from
  them; the test database and `make db-init` load that snapshot.
- `db/structure_test.go` fails if the snapshot drifts from the migrations.
- The snapshot is read from disk via `internal/db.StructureSQL()`, not
  `//go:embed` — nothing in production uses it, and embedding it shipped the
  schema in every release binary.
- That path is resolved from `runtime.Caller`, which `-trimpath` rewrites to a
  module-relative path, so the candidate is verified before it is trusted and a
  walk up from the working directory backs it up. `internal/db/schema_test.go`
  covers the walk; `make test-trimpath` is what proves the whole suite survives
  a trimmed build. The same trap exists in `internal/web/static.DevAssetsDir`.

### Dev-only commands
- `make db-init` builds a development database (schema + the real Planning
  Center reference topology in `internal/cmd/dbinit/fixture.json`).
- The `db-init` command is behind the `//go:build dev` tag, so it is absent from
  production binaries, and it refuses to run unless `static.IsDev()`. A new
  dev-only command should do both. `make db-init` applies `-tags dev` itself,
  and sets `ENVIRONMENT=dev` so the target does not depend on `.env` having
  been filled in. `make db-init FORCE=1` passes `--force` to rebuild.
- Its tests are behind the same tag, so `make test` runs `-tags dev ./...` as
  well; without that, `internal/cmd/dbinit` is skipped entirely.
- `make db-init` and `make db-reset` both `mkdir -p` the database directory.
  `database/` is gitignored, so it is absent on a fresh clone and sqlite3
  creates a missing file but never a missing directory.

### Dev-only assets (debug tooling)
- Dev/debug tools live in `internal/web/dev-assets/` and are served at `/static/dev/*` **only when `ENVIRONMENT=dev`** (via `static.IsDev()`); in production they 404 and are not embedded into the binary. See `internal/web/dev-assets/README.md`.
- Add a new tool by dropping the file in `internal/web/dev-assets/` and referencing it from a page handler (inject the `<script>` tag in the page's HTML handler when `static.IsDev()`, mirroring the `checkoutsWeb` preview.js pattern).
- Helpers: `static.DevAssetsDir`, `static.ReadDevAsset(filename)`, `static.IsDev()`.

## Environment and secrets

- Local configuration is loaded via `godotenv` in Makefile targets; keep `.env` out of commits.
- Do not log or commit secrets; use `.env` and `godotenv` when running locally.
- Runtime configuration is typically via env vars (see `internal/cmd/*` flags).

## Cursor/Copilot rules

- No Cursor rules found in `.cursor/rules/` or `.cursorrules`.
- No Copilot instructions found in `.github/copilot-instructions.md`.

## Agent workflow tips

- Prefer minimal, focused diffs; avoid refactors unless requested.
- Keep existing behavior stable; match surrounding style.
- If adding new commands or scripts, update this file.
- Do not commit code. Let the user complete the action.
