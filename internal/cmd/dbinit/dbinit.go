//go:build dev

// Package dbinit provides the `db-init` command, which builds a development
// database from scratch: schema, migration stamp, and the Planning Center
// reference topology.
//
// It is excluded from production builds by the `dev` build tag and refuses to
// run unless ENVIRONMENT=dev, so a dev build deployed by mistake still cannot
// seed a production database. That mirrors how internal/web/dev-assets is kept
// out of the release, with the build tag added because this one writes data.
package dbinit

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"kids-checkin/internal/db"
	"kids-checkin/internal/repo/event"
	"kids-checkin/internal/repo/eventcheckwindow"
	"kids-checkin/internal/repo/location"
	"kids-checkin/internal/web/static"

	"github.com/urfave/cli/v3"
)

//go:embed fixture.json
var fixtureJSON []byte

// fixture is the reference topology: events, location groups, rooms, and check
// windows, carrying their real Planning Center ids.
//
// Rows reference each other by natural key -- an event's planning center id, a
// group's name -- because the repos assign surrogate keys themselves and there
// is no Create method that takes one. Per-visit data (children, parents,
// checkins) is deliberately absent; see fixture.json.
type fixture struct {
	LocationGroups []struct {
		Name string `json:"name"`
	} `json:"location_groups"`
	Events []struct {
		Name             string  `json:"name"`
		PlanningCenterID string  `json:"planning_center_id"`
		AutoFetch        bool    `json:"auto_fetch"`
		LocationGroup    *string `json:"location_group"`
	} `json:"events"`
	Locations []struct {
		Name                   string  `json:"name"`
		PlanningCenterID       string  `json:"planning_center_id"`
		PlanningCenterParentID *string `json:"planning_center_parent_id"`
		LocationGroup          *string `json:"location_group"`
		Event                  string  `json:"event"`
	} `json:"locations"`
	EventCheckWindows []struct {
		Event          string `json:"event"`
		StartDayOfWeek int    `json:"start_day_of_week"`
		StartTime      string `json:"start_time"`
		EndDayOfWeek   int    `json:"end_day_of_week"`
		EndTime        string `json:"end_time"`
		Timezone       string `json:"timezone"`
	} `json:"event_check_windows"`
}

// Commands returns the cli.Command for registration in the root command.
func Commands() []*cli.Command {
	return []*cli.Command{
		{
			Name:  "db-init",
			Usage: "Builds a development database with the Planning Center reference topology (dev builds only)",
			Flags: []cli.Flag{
				db.DBFileFlag(),
				&cli.BoolFlag{
					Name:  "force",
					Usage: "Required to rebuild a database that already has schema",
				},
			},
			Action: Run,
		},
	}
}

// Flags returns just the flags, for tests that drive Run directly.
func Flags() []cli.Flag {
	commands := Commands()
	if len(commands) == 0 {
		// Commands is a literal, so this cannot happen today. Indexing [0]
		// without it would turn a future edit into a panic in a test helper.
		return nil
	}
	return commands[0].Flags
}

func Run(ctx context.Context, cmd *cli.Command) error {
	log := slog.Default()

	// The build tag keeps this out of production binaries. This check is what
	// keeps a dev build deployed by mistake from seeding a real database.
	if !static.IsDev() {
		return fmt.Errorf("db-init creates development data and only runs when ENVIRONMENT=dev; refusing to run with ENVIRONMENT=%q", os.Getenv("ENVIRONMENT"))
	}

	dbFile := cmd.String("db-file")
	force := cmd.Bool("force")

	if blocked, reason := blockedProdPath(dbFile); blocked {
		return fmt.Errorf("refusing to run db-init against %q: %s (override by copying the file, not by pointing dev tooling at prod)", dbFile, reason)
	}

	log.Info("db-init: starting", slog.String("db_file", dbFile), slog.Bool("force", force))

	// database/ is gitignored, so it does not exist on a fresh clone, and
	// sqlite3 will not create a missing parent directory -- it only creates a
	// missing file. Without this the documented first-run path fails with a
	// bare "unable to open database file".
	if dir := filepath.Dir(dbFile); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create database directory %s: %w", dir, err)
		}
	}

	database, err := db.InitDB(dbFile)
	if err != nil {
		return fmt.Errorf("init db: %w", err)
	}
	defer func() { _ = database.Close() }()

	populated, err := hasSchema(ctx, database)
	if err != nil {
		return err
	}
	switch {
	case populated && !force:
		return fmt.Errorf("%s already has schema; re-run with --force to rebuild it", dbFile)
	case populated:
		log.Warn("db-init: rebuilding an existing database", slog.String("db_file", dbFile))
		if err := dropEverything(ctx, database); err != nil {
			return err
		}
	}

	if err := applySchema(ctx, database); err != nil {
		return err
	}
	if err := stampMigrations(ctx, database); err != nil {
		return err
	}
	if err := applyFixture(ctx, database); err != nil {
		return err
	}

	log.Info("db-init: complete", slog.String("db_file", dbFile))
	return nil
}

// blockedProdPath refuses the container production paths even when
// ENVIRONMENT=dev. The build tag + env gate keeps a dev build deployed by
// mistake from seeding prod, but `ENVIRONMENT=dev go run -tags dev .
// db-init --db-file /data/kids-checkin.db --force` on a prod host would still
// destroy prod. Tests use /tmp/... absolute paths, which stay allowed.
func blockedProdPath(dbFile string) (bool, string) {
	clean := filepath.Clean(dbFile)
	if clean == "/data/kids-checkin.db" || strings.HasPrefix(clean, "/data/") {
		return true, "path is under the container /data volume"
	}
	if clean == "/app/database/kids-checkin.db" || strings.HasPrefix(clean, "/app/db/") {
		return true, "path is under the container /app image"
	}
	return false, ""
}

// hasSchema reports whether the app schema is already present. A missing or
// empty file is fine to initialise; one that already has tables needs --force.
//
// This counts every non-internal object rather than looking for one table by
// name. Keying on a single table made the check lie in both directions: a
// database left by `make db-reset` (schema, zero rows) was reported as
// "already has data", while a database missing that one table took the
// populate path and then failed inside applySchema with "table already exists",
// which points at neither --force nor the real cause.
func hasSchema(ctx context.Context, database *sql.DB) (bool, error) {
	var n int
	// fiber_storage (session store) and schema_migrations (bookkeeping) are not
	// app schema: starting apiserver once creates fiber_storage, which must not
	// make an otherwise empty file demand --force.
	err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE name NOT LIKE 'sqlite\_%' ESCAPE '\' AND name NOT IN ('fiber_storage', 'schema_migrations')`).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("inspect existing schema: %w", err)
	}
	return n > 0, nil
}

// keepTables lists the tables dropEverything leaves in place. Everything else is
// dropped.
//
// It is a denylist of drops rather than a list of drops on purpose: a hardcoded
// list of the tables that exist today would let a table introduced by a future
// migration survive a rebuild and then collide with the schema being applied.
var keepTables = map[string]bool{
	// Owned by the Fiber session store rather than by structure.sql, so
	// dropping it would sign everyone out of a running dev server.
	"fiber_storage": true,
}

// dropEverything removes every table, view and trigger except the ones in
// keepTables, so --force yields a clean rebuild.
func dropEverything(ctx context.Context, database *sql.DB) error {
	tables, err := objectNames(ctx, database, "table")
	if err != nil {
		return err
	}
	views, err := objectNames(ctx, database, "view")
	if err != nil {
		return err
	}
	triggers, err := objectNames(ctx, database, "trigger")
	if err != nil {
		return err
	}

	// Foreign keys are on and SQLite will not drop a table that another table
	// still references, which would otherwise force a dependency-order drop
	// list -- the same kind of list that goes stale. Deferring the checks to
	// commit-time avoids the ordering problem: every referencing table is gone
	// by the time the transaction commits.
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin drop transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
		return fmt.Errorf("defer foreign keys: %w", err)
	}

	for _, name := range tables {
		if keepTables[name] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+quoteIdentifier(name)); err != nil {
			return fmt.Errorf("drop table %s: %w", name, err)
		}
	}

	// Views and triggers have no such dependency on the tables above, so
	// dropping them first would be safe too -- but they are dropped after for
	// the same reason the tables are: a view left behind by a previous
	// --force would otherwise survive the rebuild and collide with the schema
	// being applied. A standalone index goes away with its table, and an index
	// attached to a kept table is left alone, which is the intent of
	// keepTables.
	for _, kind := range []struct {
		keyword string
		names   []string
	}{
		{"VIEW", views},
		{"TRIGGER", triggers},
	} {
		for _, name := range kind.names {
			if _, err := tx.ExecContext(ctx, `DROP `+kind.keyword+` IF EXISTS `+quoteIdentifier(name)); err != nil {
				return fmt.Errorf("drop %s %s: %w", strings.ToLower(kind.keyword), name, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit drop: %w", err)
	}
	// Reset AUTOINCREMENT counters so --force is a clean rebuild. DROP TABLE
	// removes its own sqlite_sequence row, but an explicit reset keeps the
	// "clean rebuild" claim true even for counters left by prior partial runs.
	// fiber_storage is kept, so its counter (if any) is left alone by scoping
	// to dropped names only when the table exists.
	if _, err := database.ExecContext(ctx, `DELETE FROM sqlite_sequence`); err != nil {
		// sqlite_sequence only exists when some table uses AUTOINCREMENT; a
		// database without one legitimately has no such table.
		if !strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return fmt.Errorf("reset autoincrement counters: %w", err)
		}
	}
	return nil
}

// objectNames lists the user-defined objects of one sqlite_master type.
// SQLite's own bookkeeping (sqlite_sequence and anything beginning with
// "sqlite_") is excluded: the LIKE pattern escapes the underscore so it cannot
// act as a single-character wildcard.
func objectNames(ctx context.Context, database *sql.DB, kind string) ([]string, error) {
	rows, err := database.QueryContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = ? AND name NOT LIKE 'sqlite\_%' ESCAPE '\' ORDER BY name`, kind)
	if err != nil {
		return nil, fmt.Errorf("list %ss: %w", kind, err)
	}
	defer func() { _ = rows.Close() }()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan %s name: %w", kind, err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list %ss: %w", kind, err)
	}
	return names, nil
}

// quoteIdentifier renders name as a SQL identifier, doubling any embedded
// quote. sqlite_master only ever yields valid identifiers, but an unquoted name
// is a syntax error the moment one does need quoting.
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// applySchema loads db/structure.sql, the snapshot `make db-migrate` generates
// from the migrations. Loading the snapshot rather than replaying migrations
// keeps this command in step with `make db-reset` and with the test database.
func applySchema(ctx context.Context, database *sql.DB) error {
	schema, err := db.StructureSQL()
	if err != nil {
		return err
	}
	if _, err := database.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}

// stampMigrations records the current migration version in schema_migrations.
// The snapshot omits that table -- it is bookkeeping, not schema -- and without
// a stamp `make db-migrate` would replay every migration against a database
// that is already current.
func stampMigrations(ctx context.Context, database *sql.DB) error {
	version, err := db.LatestMigrationVersion()
	if err != nil {
		return err
	}
	// IF NOT EXISTS + DELETE+INSERT keeps this idempotent if structure.sql ever
	// starts including the bookkeeping table: without it a snapshot change
	// would make CREATE fail after applySchema already committed, leaving a
	// partial DB that only --force can retry.
	const ddl = `
		CREATE TABLE IF NOT EXISTS schema_migrations (version uint64, dirty bool);
		CREATE UNIQUE INDEX IF NOT EXISTS version_unique ON schema_migrations (version);
	`
	if _, err := database.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	if _, err := database.ExecContext(ctx, `DELETE FROM schema_migrations`); err != nil {
		return fmt.Errorf("clear schema_migrations: %w", err)
	}
	if _, err := database.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, dirty) VALUES (?, 0)`, version); err != nil {
		return fmt.Errorf("stamp schema_migrations at %s: %w", version, err)
	}
	return nil
}

// validateFixture refuses a fixture missing any required section.
//
// An empty collection parses cleanly and seeds nothing, so a truncated or
// mistyped fixture would otherwise report "db-init: complete" with zero rooms.
//
// This is split out of applyFixture and takes the parsed fixture rather than
// reading fixtureJSON, so a test can pass a deliberately broken one. Inline it
// was reachable only with a mutated fixture file, and nothing verified it:
// neutering the check with `&& false` compiled cleanly and left every test in
// this package green.
func validateFixture(f fixture) error {
	for _, section := range []struct {
		name string
		n    int
	}{
		{"location_groups", len(f.LocationGroups)},
		{"events", len(f.Events)},
		{"locations", len(f.Locations)},
		{"event_check_windows", len(f.EventCheckWindows)},
	} {
		if section.n == 0 {
			return fmt.Errorf("fixture.json has no %s; refusing to build an empty reference topology", section.name)
		}
	}
	seenGroups := map[string]bool{}
	for _, g := range f.LocationGroups {
		if g.Name == "" {
			return fmt.Errorf("location group with empty name")
		}
		if seenGroups[g.Name] {
			return fmt.Errorf("duplicate location group %q", g.Name)
		}
		seenGroups[g.Name] = true
	}
	seenEventPCID := map[string]bool{}
	for _, e := range f.Events {
		if e.Name == "" || e.PlanningCenterID == "" {
			return fmt.Errorf("event with empty name or planning_center_id")
		}
		if seenEventPCID[e.PlanningCenterID] {
			return fmt.Errorf("duplicate event planning_center_id %q", e.PlanningCenterID)
		}
		seenEventPCID[e.PlanningCenterID] = true
		if e.LocationGroup != nil && *e.LocationGroup != "" && !seenGroups[*e.LocationGroup] {
			return fmt.Errorf("event %q references unknown location group %q", e.Name, *e.LocationGroup)
		}
	}
	seenLocPCID := map[string]bool{}
	for _, l := range f.Locations {
		if l.Name == "" || l.PlanningCenterID == "" || l.Event == "" {
			return fmt.Errorf("location with empty name, planning_center_id, or event")
		}
		if seenLocPCID[l.PlanningCenterID] {
			return fmt.Errorf("duplicate location planning_center_id %q", l.PlanningCenterID)
		}
		seenLocPCID[l.PlanningCenterID] = true
		if l.LocationGroup != nil && *l.LocationGroup != "" && !seenGroups[*l.LocationGroup] {
			return fmt.Errorf("location %q references unknown location group %q", l.Name, *l.LocationGroup)
		}
	}
	// Every planning_center_parent_id must name a location in the same fixture.
	// The column has no FK, so a typo would otherwise seed silently and show up
	// only as a NULL parent in a LEFT JOIN.
	for _, l := range f.Locations {
		if l.PlanningCenterParentID == nil || *l.PlanningCenterParentID == "" {
			continue
		}
		if !seenLocPCID[*l.PlanningCenterParentID] {
			return fmt.Errorf("location %q references unknown parent planning_center_id %q", l.Name, *l.PlanningCenterParentID)
		}
		if *l.PlanningCenterParentID == l.PlanningCenterID {
			return fmt.Errorf("location %q cannot be its own parent", l.Name)
		}
	}
	for _, w := range f.EventCheckWindows {
		if w.Event == "" {
			return fmt.Errorf("check window with empty event")
		}
		if w.Timezone == "" {
			return fmt.Errorf("check window for event %q has empty timezone", w.Event)
		}
		if w.StartTime == "" || w.EndTime == "" {
			return fmt.Errorf("check window for event %q has empty start/end time", w.Event)
		}
	}
	return nil
}

// applyFixture seeds the reference topology through the repo layer rather than
// with hand-written SQL, so the seeding cannot drift from the queries the
// application actually runs.
//
// Everything happens in one transaction, the read-back count included. The repos
// take a repo.DBTX, which a *sql.Tx satisfies, so a failed fixture leaves
// nothing behind.
func applyFixture(ctx context.Context, database *sql.DB) error {
	var f fixture
	if err := json.Unmarshal(fixtureJSON, &f); err != nil {
		return fmt.Errorf("parse fixture.json: %w", err)
	}
	if err := validateFixture(f); err != nil {
		return err
	}

	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin fixture transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	locationRepo := location.NewRepo(tx)
	eventRepo := event.NewRepo(tx)
	windowRepo := eventcheckwindow.NewRepo(tx)

	// Groups first: events and rooms both point at them.
	groupID := make(map[string]int64, len(f.LocationGroups))
	for _, g := range f.LocationGroups {
		created, err := locationRepo.CreateLocationGroup(ctx, location.LocationGroup{Name: g.Name})
		if err != nil {
			return fmt.Errorf("create location group %q: %w", g.Name, err)
		}
		groupID[g.Name] = created.ID
	}

	// Then events, keyed by planning center id for the rooms and windows below.
	eventID := make(map[string]int64, len(f.Events))
	for _, e := range f.Events {
		group, err := resolveGroup(groupID, e.LocationGroup)
		if err != nil {
			return fmt.Errorf("event %q: %w", e.Name, err)
		}
		created, err := eventRepo.CreateEvent(ctx, event.Event{
			Name:             e.Name,
			PlanningCenterID: e.PlanningCenterID,
			AutoFetch:        e.AutoFetch,
			LocationGroupID:  group,
		})
		if err != nil {
			return fmt.Errorf("create event %q: %w", e.Name, err)
		}
		eventID[e.PlanningCenterID] = created.ID
	}

	for _, l := range f.Locations {
		id, ok := eventID[l.Event]
		if !ok {
			return fmt.Errorf("location %q references unknown event %q", l.Name, l.Event)
		}
		group, err := resolveGroup(groupID, l.LocationGroup)
		if err != nil {
			return fmt.Errorf("location %q: %w", l.Name, err)
		}
		if _, err := locationRepo.CreateLocation(ctx, location.Location{
			Name:                   l.Name,
			PlanningCenterID:       l.PlanningCenterID,
			PlanningCenterParentID: l.PlanningCenterParentID,
			EventID:                id,
			LocationGroupID:        group,
		}); err != nil {
			return fmt.Errorf("create location %q: %w", l.Name, err)
		}
	}

	for _, w := range f.EventCheckWindows {
		id, ok := eventID[w.Event]
		if !ok {
			return fmt.Errorf("check window references unknown event %q", w.Event)
		}
		if _, err := windowRepo.CreateCheckWindow(ctx, eventcheckwindow.EventCheckWindow{
			EventID:        id,
			StartDayOfWeek: w.StartDayOfWeek,
			StartTime:      w.StartTime,
			EndDayOfWeek:   w.EndDayOfWeek,
			EndTime:        w.EndTime,
			Timezone:       w.Timezone,
		}); err != nil {
			return fmt.Errorf("create check window for event %q: %w", w.Event, err)
		}
	}

	// Counted from the database rather than from the fixture slices. Several of
	// these repos upsert, so a duplicate planning_center_id -- the single most
	// likely fixture edit -- inserts fewer rows than the fixture lists, and
	// logging len(f.Locations) would report rooms that are not there.
	counts, err := fixtureCounts(ctx, tx)
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit fixture: %w", err)
	}

	slog.InfoContext(ctx, "db-init: seeded reference topology",
		slog.Int("location_groups", counts["location_groups"]),
		slog.Int("events", counts["events"]),
		slog.Int("locations", counts["locations"]),
		slog.Int("event_check_windows", counts["event_check_windows"]))
	return nil
}

// fixtureCounts reads back the number of rows each seeded table ended up with.
//
// It runs inside the fixture transaction rather than after it. A transaction
// reads its own uncommitted writes, so the numbers are the same -- but a
// failure here now rolls the fixture back instead of reporting an error for a
// database that was in fact seeded successfully, which left it built and made
// the retry demand --force.
func fixtureCounts(ctx context.Context, tx *sql.Tx) (map[string]int, error) {
	tables := []string{"location_groups", "events", "locations", "event_check_windows"}
	counts := make(map[string]int, len(tables))
	for _, table := range tables {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+quoteIdentifier(table)).Scan(&n); err != nil {
			return nil, fmt.Errorf("count %s: %w", table, err)
		}
		counts[table] = n
	}
	return counts, nil
}

// resolveGroup turns a fixture's optional group name into the optional id the
// repos expect, reporting a name that no group in the fixture defines rather
// than silently leaving the row ungrouped.
func resolveGroup(groups map[string]int64, name *string) (*int64, error) {
	if name == nil {
		return nil, nil
	}
	id, ok := groups[*name]
	if !ok {
		return nil, fmt.Errorf("references unknown location group %q", *name)
	}
	return &id, nil
}
