package location

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"kids-checkin/internal/repo"

	"github.com/Masterminds/squirrel"
)

type LocationFilter struct {
	IDs              []int64
	PlanningCenterID string
	LocationGroupID  int64
	EventID          int64
	Name             string
}

type LocationGroupFilter struct {
	ID   int64
	Name string
}

type LocationGroup struct {
	ID   int64
	Name string
}

type Location struct {
	ID                     int64
	PlanningCenterID       string
	PlanningCenterParentID *string
	EventID                int64
	LocationGroupID        *int64
	Name                   string
	LastCheckedOutTime     time.Time
}

type Repo interface {
	ListLocations(ctx context.Context, filter LocationFilter) ([]Location, error)
	CreateLocation(ctx context.Context, location Location) (Location, error)
	UpdateLocation(ctx context.Context, location Location) error
	DeleteLocation(ctx context.Context, id int64) error
	ListLocationGroups(ctx context.Context, filter LocationGroupFilter) ([]LocationGroup, error)
	CreateLocationGroup(ctx context.Context, lg LocationGroup) (LocationGroup, error)
	UpdateLocationGroup(ctx context.Context, lg LocationGroup) error
	DeleteLocationGroup(ctx context.Context, id int64) error
}

type sqliteRepo struct {
	db repo.DBTX
}

func NewRepo(db repo.DBTX) Repo {
	return &sqliteRepo{
		db: db,
	}
}

func (r *sqliteRepo) ListLocationGroups(ctx context.Context, filter LocationGroupFilter) ([]LocationGroup, error) {
	builder := squirrel.
		Select("id", "name").
		From("location_groups").
		RunWith(r.db)

	if filter.ID > 0 {
		builder = builder.Where(squirrel.Eq{"id": filter.ID})
	}

	if filter.Name != "" {
		builder = builder.Where(squirrel.Eq{"name": filter.Name})
	}

	rows, err := builder.QueryContext(ctx)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := make([]LocationGroup, 0)
	for rows.Next() {
		var group LocationGroup
		err := rows.Scan(&group.ID, &group.Name)
		if err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating location groups: %w", err)
	}

	return groups, nil
}

func (r *sqliteRepo) CreateLocationGroup(ctx context.Context, lg LocationGroup) (LocationGroup, error) {
	builder := squirrel.
		Insert("location_groups").
		RunWith(r.db).
		Columns("name").
		Values(lg.Name).
		SuffixExpr(squirrel.Expr("ON CONFLICT(name) DO UPDATE SET name = ?", lg.Name))
	res, err := builder.ExecContext(ctx)
	if err != nil {
		return LocationGroup{}, fmt.Errorf("inserting location group: %w", err)
	}
	// Not swallowed: callers use the returned ID as the location_group_id of
	// every room in the group, so a zero here surfaces much later as an opaque
	// foreign-key failure rather than at the point of the error.
	lg.ID, err = res.LastInsertId()
	if err != nil {
		return LocationGroup{}, fmt.Errorf("reading new location group id: %w", err)
	}
	return lg, nil
}

func (r *sqliteRepo) UpdateLocationGroup(ctx context.Context, lg LocationGroup) error {
	result, err := squirrel.Update("location_groups").
		Set("name", lg.Name).
		Where(squirrel.Eq{"id": lg.ID}).
		RunWith(r.db).
		ExecContext(ctx)
	if err != nil {
		return fmt.Errorf("updating location group: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("reading updated rows: %w", err)
	}
	if rows == 0 {
		return repo.ErrNotFound
	}
	return nil
}

func (r *sqliteRepo) DeleteLocationGroup(ctx context.Context, id int64) error {
	for _, table := range []string{"locations", "events"} {
		count, err := r.countWhere(ctx, table, squirrel.Eq{"location_group_id": id})
		if err != nil {
			return fmt.Errorf("checking %s references: %w", table, err)
		}
		if count > 0 {
			return ErrLocationGroupInUse
		}
	}
	result, err := squirrel.Delete("location_groups").
		Where(squirrel.Eq{"id": id}).
		RunWith(r.db).
		ExecContext(ctx)
	if err != nil {
		return fmt.Errorf("deleting location group: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("reading deleted rows: %w", err)
	}
	if rows == 0 {
		return repo.ErrNotFound
	}
	return nil
}

func (r *sqliteRepo) countWhere(ctx context.Context, table string, cond squirrel.Sqlizer) (int, error) {
	var count int
	err := squirrel.Select("COUNT(*)").
		From(table).
		Where(cond).
		RunWith(r.db).
		QueryRowContext(ctx).
		Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (r *sqliteRepo) ListLocations(ctx context.Context, filter LocationFilter) ([]Location, error) {
	builder := squirrel.
		Select(
			"id",
			"planning_center_id",
			"planning_center_parent_id",
			"event_id",
			"location_group_id",
			"name",
			"last_checked_out_time",
		).
		From("locations").
		RunWith(r.db)

	if len(filter.IDs) > 0 {
		builder = builder.Where(squirrel.Eq{"id": filter.IDs})
	}

	if filter.PlanningCenterID != "" {
		builder = builder.Where(squirrel.Eq{"planning_center_id": filter.PlanningCenterID})
	}

	if filter.Name != "" {
		builder = builder.Where(squirrel.Eq{"name": filter.Name})
	}

	if filter.LocationGroupID > 0 {
		builder = builder.Where(squirrel.Eq{"location_group_id": filter.LocationGroupID})
	}

	if filter.EventID > 0 {
		builder = builder.Where(squirrel.Eq{"event_id": filter.EventID})
	}

	rows, err := builder.QueryContext(ctx)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	locations := make([]Location, 0)
	for rows.Next() {
		var location Location
		var lgIDSQL sql.NullInt64
		var pcpIDSQL sql.NullString
		var lastFetchedAtSQL sql.NullTime
		err := rows.Scan(&location.ID, &location.PlanningCenterID, &pcpIDSQL, &location.EventID, &lgIDSQL, &location.Name, &lastFetchedAtSQL)
		if err != nil {
			return nil, err
		}

		if lgIDSQL.Valid {
			location.LocationGroupID = &lgIDSQL.Int64
		}
		if pcpIDSQL.Valid {
			location.PlanningCenterParentID = &pcpIDSQL.String
		}
		if lastFetchedAtSQL.Valid {
			location.LastCheckedOutTime = lastFetchedAtSQL.Time
		}

		locations = append(locations, location)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating locations: %w", err)
	}
	return locations, nil
}

var ErrLocationGroupInUse = errors.New("location group is in use")

var ErrLocationExists = errors.New("location with Planning Center ID already exists")

func (r *sqliteRepo) CreateLocation(ctx context.Context, location Location) (Location, error) {
	columns := []string{"planning_center_id", "event_id", "name"}
	values := []any{location.PlanningCenterID, location.EventID, location.Name}

	if location.PlanningCenterParentID != nil {
		columns = append(columns, "planning_center_parent_id")
		values = append(values, *location.PlanningCenterParentID)
	}

	if location.LocationGroupID != nil {
		columns = append(columns, "location_group_id")
		values = append(values, *location.LocationGroupID)
	}

	if !location.LastCheckedOutTime.IsZero() {
		columns = append(columns, "last_checked_out_time")
		values = append(values, location.LastCheckedOutTime.Format(time.RFC3339))
	}

	builder := squirrel.
		Insert("locations").
		RunWith(r.db).
		Columns(columns...).
		Values(values...).
		SuffixExpr(squirrel.Expr("ON CONFLICT(planning_center_id) DO UPDATE SET name = excluded.name"))

	result, err := builder.ExecContext(ctx)
	if err != nil {
		return Location{}, fmt.Errorf("inserting location: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return Location{}, err
	}

	location.ID = id
	return location, nil
}

func (r *sqliteRepo) UpdateLocation(ctx context.Context, location Location) error {
	setMap := map[string]any{
		"planning_center_id":        location.PlanningCenterID,
		"planning_center_parent_id": location.PlanningCenterParentID,
		"event_id":                  location.EventID,
		"location_group_id":         location.LocationGroupID,
		"name":                      location.Name,
	}

	if !location.LastCheckedOutTime.IsZero() {
		setMap["last_checked_out_time"] = location.LastCheckedOutTime
	}

	builder := squirrel.
		Update("locations").
		RunWith(r.db).
		SetMap(setMap).
		Where(squirrel.Eq{"id": location.ID})
	res, err := builder.ExecContext(ctx)
	if err != nil {
		return err
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return repo.ErrNotFound
	}
	return nil
}

func (r *sqliteRepo) DeleteLocation(ctx context.Context, id int64) error {
	builder := squirrel.
		Delete("locations").
		RunWith(r.db).
		Where(squirrel.Eq{"id": id})

	res, err := builder.ExecContext(ctx)
	if err != nil {
		return err
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return repo.ErrNotFound
	}
	return nil
}
