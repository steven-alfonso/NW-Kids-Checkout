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
db-migrate:
	@tmpdb=$$(mktemp) && \
	sqlite3 $$tmpdb < db/pragmas.sqlite && \
	migrate -source file://db/migrations -database "sqlite3://$$tmpdb" up && \
	sqlite3 $$tmpdb .schema | grep -v -e '^CREATE TABLE schema_migrations' -e '^CREATE UNIQUE INDEX version_unique ON schema_migrations' > db/structure.sql && \
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
	if [ "$(ASSET_BUILD)" = "1" ]; then godotenv $(ASSET_SCRIPT); fi && \
    godotenv go build -o $(BIN_PATH) main.go

.PHONY: assets
assets:
	godotenv $(ASSET_SCRIPT)

.PHONY: web
web: build
	godotenv $(BIN_PATH) apiserver

.PHONY: web-lr
web-lr:
	go tool air --build.cmd="make build" --build.full_bin="godotenv $(BIN_PATH) apiserver" --build.exclude_dir="bin,database"

.PHONY: checkout-fetcher
checkout-fetcher: build
	godotenv $(BIN_PATH) checkout-fetcher --use-check-windows --service

.PHONY: test
test:
	godotenv go test ./...
	npm test

.PHONY: db-seed
db-seed:
	godotenv ./bin/db-seed
