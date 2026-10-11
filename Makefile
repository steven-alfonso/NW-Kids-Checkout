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
# development database with the reference topology use `make db-seed` after it.
#
# The mkdir matters: database/ is gitignored and absent on a fresh clone.
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
# The mkdir matters (see db-reset): database/ is gitignored and absent on a
# fresh clone.
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
	mkdir -p $(dir $(KIDS_CHECKIN_DB_FILE)) && \
	go tool godotenv $(BIN_PATH) apiserver

.PHONY: web-lr
# All air configuration lives in .air.toml.
web-lr:
	mkdir -p $(dir $(KIDS_CHECKIN_DB_FILE)) && \
	go tool air

.PHONY: checkout-fetcher
checkout-fetcher: build
	mkdir -p $(dir $(KIDS_CHECKIN_DB_FILE)) && \
	go tool godotenv $(BIN_PATH) checkout-fetcher --use-check-windows --service

.PHONY: test
test:
	go tool godotenv go test ./...
	npm test

.PHONY: db-seed
# Seeds the reference topology from the committed fixture. The database path
# is passed explicitly so this writes the same file the Makefile and the
# binary default to; bin/db-seed takes it as $1 with $DB_FILE as fallback.
db-seed:
	mkdir -p $(dir $(KIDS_CHECKIN_DB_FILE)) && \
	go tool godotenv ./bin/db-seed $(KIDS_CHECKIN_DB_FILE)

.PHONY: random-data
# The mkdir matters (see db-reset): sqlite creates a missing file but never a
# missing directory.
random-data:
	mkdir -p $(dir $(KIDS_CHECKIN_DB_FILE)) && \
	go tool godotenv go run ./cmd/randomdata --db-file $(KIDS_CHECKIN_DB_FILE)
