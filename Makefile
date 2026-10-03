KIDS_CHECKIN_DB_FILE := database/kids-checkin.db

BIN_NAME := kids-checkin
BIN_PATH := ./bin/$(BIN_NAME)

ASSET_BUILD ?= 0
ASSET_SCRIPT := go run ./cmd/assets

# help must be first so that it is the default.
.PHONY: help
help:
	@grep -vE '^(\.PHONY|.*:=)' Makefile | grep '^[^#[:space:]].*:' | cut -d: -f1

.PHONY: db-reset
# Drops and recreates an empty database with the current schema. For a
# development database with the Planning Center reference topology use
# `make db-init` instead.
#
# The mkdir matters: database/ is gitignored, so it does not exist on a fresh
# clone and both `touch` and sqlite3 would otherwise fail with a bare
# "no such file or directory".
db-reset:
	mkdir -p $(dir $(KIDS_CHECKIN_DB_FILE)) && \
	rm -f $(KIDS_CHECKIN_DB_FILE) && \
	touch $(KIDS_CHECKIN_DB_FILE) && \
	set -e; sqlite3 $(KIDS_CHECKIN_DB_FILE) < db/structure.sql && \
	latest=$$(ls db/migrations/*.up.sqlite | sort | tail -1 | xargs basename | cut -d_ -f1) && \
	sqlite3 $(KIDS_CHECKIN_DB_FILE) "CREATE TABLE IF NOT EXISTS schema_migrations (version uint64, dirty bool); \
		CREATE UNIQUE INDEX IF NOT EXISTS version_unique ON schema_migrations (version); \
		INSERT INTO schema_migrations (version, dirty) VALUES ($$latest, 0);"

.PHONY: db-migrate
# The snapshot dumps raw sqlite_master text (not .schema) so db/structure.sql
# is byte-identical regardless of the sqlite3 CLI version.
#
# The mkdir matters for the same reason it does in db-reset: database/ is
# gitignored, so on a fresh clone the migrate step at the end of this recipe
# would otherwise fail with a bare "unable to open database file".
db-migrate:
	@mkdir -p $(dir $(KIDS_CHECKIN_DB_FILE)) && \
	tmpdb=$$(mktemp) && \
	sqlite3 $$tmpdb < db/pragmas.sqlite && \
	migrate -source file://db/migrations -database "sqlite3://$$tmpdb" up && \
	sqlite3 -batch -noheader -init /dev/null $$tmpdb "SELECT sql || ';' FROM sqlite_master WHERE type IN ('table','index') AND name NOT IN ('schema_migrations','sqlite_sequence','version_unique') AND sql IS NOT NULL;" > db/structure.sql && \
	rm -f $$tmpdb && \
	migrate -source file://db/migrations -database "sqlite3://$(KIDS_CHECKIN_DB_FILE)" up

# usage: make db-new-migration NAME=<migration name>
.PHONY: db-new-migration
db-new-migration:
	@if [ -z "$(NAME)" ]; then \
		echo "ERROR: You must provide a migration name using NAME=."; \
		exit 1; \
	fi
	@echo "Creating new migration: $(NAME)..."
	migrate create -ext sqlite -dir db/migrations $(NAME)

.PHONY: build
build:
	mkdir -pv bin && \
	if [ "$(ASSET_BUILD)" = "1" ]; then go tool godotenv $(ASSET_SCRIPT); fi && \
	go tool godotenv go build -o $(BIN_PATH) main.go

.PHONY: assets
assets:
	go tool godotenv $(ASSET_SCRIPT)

.PHONY: web
web: build
	go tool godotenv $(BIN_PATH) apiserver

.PHONY: web-lr
# All air configuration lives in .air.toml.
web-lr:
	go tool air

.PHONY: checkout-fetcher
checkout-fetcher: build
	go tool godotenv $(BIN_PATH) checkout-fetcher --use-check-windows --service

.PHONY: test
# Both tag sets are run. The dev-tagged packages hold every test for the
# dev-only db-init command; without -tags dev those packages are skipped
# entirely, which is how a few hundred lines of dbinit tests went unrun.
test:
	go tool godotenv go test ./...
	go tool godotenv go test -tags dev ./...
	npm test

.PHONY: test-trimpath
# Same suite with -trimpath, for BOTH build-tag sets.
#
# -tags dev is not optional here. The dev-tagged packages include the dbinit
# tests, which are consumers of the very path resolution this target exists to
# check; without the tag they are skipped entirely ("build constraints exclude
# all Go files"), so the target would silently cover less than it claims.
test-trimpath:
	go tool godotenv go test -trimpath ./...
	go tool godotenv go test -trimpath -tags dev ./...

.PHONY: test-all
# The complete matrix: both build-tag sets, with and without -trimpath.
#
# The trimpath half is not optional and `make test` cannot stand in for it.
# db/structure.sql is read from disk rather than //go:embed'd, and its path is
# resolved from runtime.Caller, which -trimpath rewrites to a module-relative
# path. Reverting that resolution to the plain runtime.Caller form passes
# `go test ./...` and fails only here -- verified by breaking it and confirming
# the failure appears in this target and nowhere else.
#
# This repository has no CI, so this target is the entire gate. Run it before
# pushing anything that touches internal/db, internal/web/static, or the
# db-file wiring.
test-all: test test-trimpath

.PHONY: db-init
# Builds a development database: schema, migration stamp, and the Planning
# Center reference topology. The db-init command only exists under the `dev`
# build tag and refuses to run unless ENVIRONMENT=dev, so the tag is applied
# here rather than left to the caller to remember.
#
# This replaces the old `make db-seed`, which shelled out to a checked-in bash
# script (bin/db-seed) and a committed sqlite fixture (seed/seed.db). The
# topology now lives in internal/cmd/dbinit/fixture.json and is applied through
# parameterized statements, so it can no longer drift from the schema.
#
# Use `make random-data` afterwards for per-visit check-in data.
#
# ENVIRONMENT=dev is set here rather than left to .env: the command refuses to
# run without it, and a fresh clone has no .env at all, so relying on it made
# the documented first-run path fail. --force is opt-in via
# `make db-init FORCE=1` so that rebuilding an existing database is possible
# without hand-typing the go run invocation.
db-init:
	mkdir -p $(dir $(KIDS_CHECKIN_DB_FILE)) && \
	ENVIRONMENT=dev go tool godotenv go run -tags dev . db-init \
		--db-file $(KIDS_CHECKIN_DB_FILE) $(if $(FORCE),--force,)

.PHONY: random-data
# The mkdir matters for the same reason it does in db-reset and db-init:
# database/ is gitignored, so it is absent on a fresh clone and sqlite creates
# a missing file but never a missing directory.
random-data:
	mkdir -p $(dir $(KIDS_CHECKIN_DB_FILE)) && \
	go tool godotenv go run ./cmd/random-data --db-file $(KIDS_CHECKIN_DB_FILE)
