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

	dbschema "kids-checkin/db"
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

	// Validate the fixture before touching the database: a broken fixture must
	// not destroy a good dev DB via --force and then fail.
	if err := validateFixtureBytes(fixtureJSON); err != nil {
		return err
	}

	log.Info("db-init: starting", slog.String("db_file", dbFile), slog.Bool("force", force))

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
		// Rebuild by removing the file and reopening, rather than dropping
		// tables one by one: a hardcoded drop list goes stale when migrations
		// add tables, while a removed file cannot collide with the schema.
		// (Dev sessions in fiber_storage go with it; acceptable for a dev tool.)
		if err := database.Close(); err != nil {
			return fmt.Errorf("close db for rebuild: %w", err)
		}
		if err := os.Remove(dbFile); err != nil {
			return fmt.Errorf("remove %s for rebuild: %w", dbFile, err)
		}
		database, err = db.InitDB(dbFile)
		if err != nil {
			return fmt.Errorf("init db: %w", err)
		}
		defer func() { _ = database.Close() }()
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
	// fiber_storage (session store), schema_migrations (bookkeeping), and its
	// version_unique index are not app schema: starting apiserver once creates
	// fiber_storage, which must not make an otherwise empty file demand --force.
	// The type filter keeps a standalone bookkeeping index from counting when
	// its table is absent.
	err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type IN ('table', 'view', 'trigger') AND name NOT LIKE 'sqlite\_%' ESCAPE '\' AND name NOT IN ('fiber_storage', 'schema_migrations', 'version_unique')`).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("inspect existing schema: %w", err)
	}
	return n > 0, nil
}

// applySchema loads the schema snapshot generated by `make db-migrate`.
// Loading the snapshot rather than replaying migrations keeps this command in
// step with `make db-reset` and with the test database.
func applySchema(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, dbschema.Schema); err != nil {
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
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (version uint64, dirty bool);
		CREATE UNIQUE INDEX IF NOT EXISTS version_unique ON schema_migrations (version);
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	// Clear first so a future snapshot that starts including the bookkeeping
	// table still converges to exactly one row.
	if _, err := database.ExecContext(ctx, `DELETE FROM schema_migrations`); err != nil {
		return fmt.Errorf("clear schema_migrations: %w", err)
	}
	if _, err := database.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, dirty) VALUES (?, 0)`, version); err != nil {
		return fmt.Errorf("stamp schema_migrations at %s: %w", version, err)
	}
	return nil
}

// validateFixtureBytes parses and validates the embedded fixture. Run before
// any destructive drop so a broken fixture cannot destroy a good database.
func validateFixtureBytes(raw []byte) error {
	var f fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		return fmt.Errorf("parse fixture.json: %w", err)
	}
	return validateFixture(f)
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
	seenEventName := map[string]bool{}
	for _, e := range f.Events {
		if e.Name == "" || e.PlanningCenterID == "" {
			return fmt.Errorf("event with empty name or planning_center_id")
		}
		if seenEventPCID[e.PlanningCenterID] {
			return fmt.Errorf("duplicate event planning_center_id %q", e.PlanningCenterID)
		}
		seenEventPCID[e.PlanningCenterID] = true
		if seenEventName[e.Name] {
			return fmt.Errorf("duplicate event name %q (events.name is UNIQUE)", e.Name)
		}
		seenEventName[e.Name] = true
		if e.LocationGroup != nil && *e.LocationGroup != "" && !seenGroups[*e.LocationGroup] {
			return fmt.Errorf("event %q references unknown location group %q", e.Name, *e.LocationGroup)
		}
	}
	// Natural-key index for cross-reference checks below.
	seenEventKey := map[string]bool{}
	for _, e := range f.Events {
		seenEventKey[e.PlanningCenterID] = true
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
		if !seenEventKey[l.Event] {
			return fmt.Errorf("location %q references unknown event %q", l.Name, l.Event)
		}
		// An empty string is not NULL: resolveGroup treats "" as a name
		// lookup and fails, so reject it here rather than late in the tx.
		if l.LocationGroup != nil && *l.LocationGroup == "" {
			return fmt.Errorf("location %q has empty location_group (use null, not \"\")", l.Name)
		}
		if l.LocationGroup != nil && !seenGroups[*l.LocationGroup] {
			return fmt.Errorf("location %q references unknown location group %q", l.Name, *l.LocationGroup)
		}
	}
	// Every planning_center_parent_id must name a location in the same fixture.
	// The column has no FK, so a typo would otherwise seed silently and show up
	// only as a NULL parent in a LEFT JOIN. Empty string is rejected: it would
	// insert verbatim as "" (not NULL) and match no parent.
	for _, l := range f.Locations {
		if l.PlanningCenterParentID == nil {
			continue
		}
		if *l.PlanningCenterParentID == "" {
			return fmt.Errorf("location %q has empty planning_center_parent_id (use null, not \"\")", l.Name)
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
		if !seenEventKey[w.Event] {
			return fmt.Errorf("check window references unknown event %q", w.Event)
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
	// Validation above rejects duplicates, so the counts must match the slices;
	// a mismatch means a repo changed shape underneath the seeder.
	counts, err := fixtureCounts(ctx, tx)
	if err != nil {
		return err
	}
	for _, want := range []struct {
		name string
		got  int
		want int
	}{
		{"location_groups", counts["location_groups"], len(f.LocationGroups)},
		{"events", counts["events"], len(f.Events)},
		{"locations", counts["locations"], len(f.Locations)},
		{"event_check_windows", counts["event_check_windows"], len(f.EventCheckWindows)},
	} {
		if want.got != want.want {
			return fmt.Errorf("seeded %s = %d, want %d; fixture rows did not all insert", want.name, want.got, want.want)
		}
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
		// Table names come from the fixed list above, never from input.
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n); err != nil {
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
