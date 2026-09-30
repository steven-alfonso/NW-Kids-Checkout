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
db-reset:
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
db-migrate:
	@tmpdb=$$(mktemp) && \
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
# include_ext is load-bearing, not a convenience. The static assets under
# internal/web/static are compiled into the binary with go:embed, so a .js or
# .css edit cannot be observed by the running server until it is rebuilt and
# restarted. air's default include_ext does not list .js or .css, so without
# this an edit to checkouts.js is silently ignored: air logs the directory as
# watched, never rebuilds, and the browser keeps getting the previously
# embedded copy. That reads as a browser cache problem and is not one --
# hard-refreshing will not help either, because the old bytes are in the
# binary, not in the cache.
web-lr:
	go tool air --build.cmd="make build" --build.full_bin="go tool godotenv $(BIN_PATH) apiserver" --build.exclude_dir="bin,database,node_modules" --build.include_ext="go,html,js,css,svg,png,ico,webmanifest,json"

.PHONY: checkout-fetcher
checkout-fetcher: build
	go tool godotenv $(BIN_PATH) checkout-fetcher --use-check-windows --service

.PHONY: test
test:
	go tool godotenv go test ./...
	npm test

.PHONY: db-seed
db-seed:
	go tool godotenv ./bin/db-seed

.PHONY: random-data
random-data:
	go tool godotenv go run ./cmd/random-data --db-file $(KIDS_CHECKIN_DB_FILE)
