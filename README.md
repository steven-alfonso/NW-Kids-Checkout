# Project Overview

This project is a web service for managing kids' check-ins and check-outs, likely for a church or similar organization, given the references to "Planning Center". It's built in Go and uses the Fiber web framework. The service exposes a RESTful API and also uses websockets for real-time updates. The data is stored in a SQLite database.

The project is structured as a command-line application with commands:
- `apiserver`: Starts the web server.
- `checkout-fetcher`: A background worker that fetches checkout information from Planning Center.
- `checkins delete-old`: Deletes old checkins/manual checkins older than a duration.
- `checkins seed-preview`: Deletes all current checkins/manual checkins and seeds preview data (DB equivalent of `preview.js`).
- `locations upsert-location`: Upserts a location from Planning Center.

## Requirements
- [Golang](https://go.dev/) 1.27+
- [SQLite](https://www.sqlite.org/) 3.37+ (should be on macOS by default). If not, install via Brew.
- [GNU make](https://www.gnu.org/software/make/) (should be on macOS by default)
- [Node.js](https://nodejs.org/) 18+ (required for frontend assets and JS tests)
- [godotenv](https://github.com/joho/godotenv). Install via `go install github.com/joho/godotenv/cmd/godotenv@latest` once Golang is installed.

## Quick Start
1. Create a `.env` file if it does not exist. Copy the example and fill it in:
```shell
cp .env.example .env
```
An empty `.env` is not enough: `.env.example` is where `ENVIRONMENT=dev` lives,
and step 2 below needs it.

`LOGIN_PASSWORD_ADMIN` and `LOGIN_PASSWORD_USER` take bcrypt hashes, not
plaintext. Generate one with the same `golang.org/x/crypto/bcrypt` the login
check uses:

```shell
mkdir -p /tmp/pwgen && cat > /tmp/pwgen/main.go <<'EOF'
package main

import (
	"fmt"
	"os"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	h, err := bcrypt.GenerateFromPassword([]byte(os.Args[1]), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(h))
}
EOF
go run /tmp/pwgen/main.go 'your-password'
```

Run that from the repo root so the module resolves `golang.org/x/crypto`, then
paste the output into `.env` **in single quotes**. godotenv expands `$VAR`
inside an unquoted value and a bcrypt hash is mostly `$` signs, so unquoted the
prefix is eaten and login fails with `?error=invalid` and nothing in the log to
say why.

2. Build a development database with the Planning Center reference topology:
```shell
make db-init
```
`db-init` applies the schema and seeds 2 events, 3 location groups, 33 rooms,
and 2 check windows, using their real Planning Center ids. It only exists in
builds made with the `dev` build tag and refuses to run unless
`ENVIRONMENT=dev`, so it cannot touch a production database. Add per-visit
check-in data afterwards with `make random-data`.

Re-running it will not silently overwrite what is already there. If the database
already has schema, `db-init` refuses and exits non-zero:

```shell
database/kids-checkin.db already has schema; re-run with --force to rebuild it
```

To rebuild one that already has schema, pass `FORCE=1`:
```shell
make db-init FORCE=1
```
`FORCE=1` drops every table and starts over, including any check-ins you have
added since. Only pass it when you intend to discard them -- otherwise use
`make db-migrate` to apply new migrations to an existing database.
3. In one terminal, start the checkout fetcher:
```shell
make checkout-fetcher
```
If you want mock data instead of real data, add the following to your `.env` file then run the `checkout-fetcher` target:
```shell
CHECKOUT_FETCHER_USE_MOCK=true
```
4. In another terminal, start the server:
```shell
make web
```
Alternatively, you can use `make web-lr` to start the server with live reload.

5. Navigate to http://localhost:3000/v1/checkins/checkouts?checked_out_after=-31m to see the checkouts for the past 31 minutes.

## Building and Running

The project uses a `Makefile` for common tasks. Run `make` to see a list of available targets.

### Building the application

To build the application binary, run:

```sh
make build
```

This will create a binary at `./bin/kids-checkin`. Running `./bin/kids-checkin` will print the help message.

### Running the web server

To run the web server, use the `web` target:

```sh
make web
```

This will build the application and start the API server on port `3000` by default.

### Running the checkout fetcher

To run the checkout fetcher, use the `checkout-fetcher` target:

```sh
make checkout-fetcher
```

This will build the application and start the fetcher process.

### Checkins commands

Delete old checkins (default 7 days):
```sh
./bin/kids-checkin checkins delete-old --age -168h
# The database path comes from --db-file, then $DB_FILE, then the default
# database/kids-checkin.db. It matches the Makefile, so `make db-reset` and a
# bare `./bin/kids-checkin` always mean the same file.
```

Seed preview data (DB equivalent of `internal/web/dev-assets/preview.js`):

This mirrors `loadPreviewData()` in the browser but writes directly to SQLite via `checkin`/`manualcheckin` Repos. It deletes all rows in `checkins` and `manual_checkins`, then inserts 10 demo rows (5 Planning Center + 5 manual) at 0, 3.9, 6, 7.9, and 2 min ago so you can validate pill colors and the confirm checkbox without waiting for real time. Locations/events are preserved; if no locations exist a `Preview Event`/`Preview Location` is created via `location`/`event` Repos to satisfy `checkins.location_id`.

```sh
# Requires --force (destructive operation). Respects --db-file / $DB_FILE.
godotenv ./bin/kids-checkin checkins seed-preview --force
```

Without `--force` the command exits with `must pass --force to seed preview data`.

### Tailwind CSS Development
When developing the frontend, you need nodejs and npm.

Install:
```shell
npm install
```

Automatically regenerate the tailwind.css file:
```shell
npm run watch:css
```


Build the tailwind.css file:
```shell
npm run build:css
```

### Running tests

To run the test suite, use the `test` target:

```sh
make test
```

## Database

The project uses SQLite for its database. Database migrations are managed with the `migrate` tool.

### Running the tests

```sh
make test       # go test, then -tags dev, then npm test
make test-all   # the above, plus both -trimpath passes -- run this before pushing
```

`make test-trimpath` exists because `db/structure.sql` is read from disk rather
than embedded, and its path is resolved from `runtime.Caller`, which `-trimpath`
rewrites to a module-relative path. Resolution that works in a normal build can
fail in a trimmed one, and that failure is invisible to `make test`.

CI ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)) runs all four
combinations as required steps, along with `gofmt`, `go vet`, and a build of the
release entrypoint, so it is the gate on a push. `make test-all` is the local
equivalent -- run it first so a failure is yours rather than a red pipeline's.
CI calls `go test` directly instead of through `make test` because `godotenv`
exits non-zero when `.env` is absent, and `.env` is gitignored.

- **Resetting the database (empty schema):** `make db-reset`
- **Building a development database (schema + reference topology):** `make db-init`. Fails if the database already has schema; `make db-init FORCE=1` drops and rebuilds it.
- **Running migrations:** `make db-migrate`
- **Creating a new migration:** `make db-new-migration NAME=<migration_name>`

Migrations are applied by the `migrate` CLI, not by the application binary.
`db/structure.sql` is the snapshot `make db-migrate` generates from them, and
it is what the test database and `db-init` load. `db/structure_test.go` fails if
the snapshot drifts from the migrations.

The database path is defined once, in `db.DefaultDBFile`, and every command
takes it from `db.DBFileFlag()`:

| Source | Precedence |
| --- | --- |
| `--db-file` | highest |
| `$DB_FILE` | |
| `db.DefaultDBFile` (`database/kids-checkin.db`) | lowest |

Do not set `DB_FILE` in `.env` unless you need to point at a different file --
the default already matches the Makefile, and an extra copy is what let the two
drift apart before. Production sets `DB_FILE=/data/kids-checkin.db` in the
Dockerfile, which is a deliberate override for the container's volume.

### Production Migrations
Connect to a shell and run:
```shell
migrate -source file:///app/db/migrations -database "sqlite3:///data/kids-checkin.db" up
```

## API Endpoints

The API is currently versioned under `/v1`.

### Check-ins

- `GET /v1/checkins/checkouts/:location`: Get a list of checkouts for a specific location. This endpoint can also be upgraded to a websocket connection for real-time updates.

### Locations

- `GET /v1/locations`: Get a list of locations.

## Development Conventions

- The project follows standard Go project layout.
- It uses `gofiber` for the web framework.
- The `urfave/cli` library is used for the command-line interface.
- Database queries are built using `squirrel`.
- Testing is done with the `testify` library.
