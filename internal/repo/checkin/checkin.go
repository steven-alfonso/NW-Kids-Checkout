package checkin

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"kids-checkin/internal/repo"

	"github.com/Masterminds/squirrel"
)

type Filter struct {
	ID                 int64
	PlanningCenterID   string
	LocationID         int64
	EventID            int64
	LocationName       string
	LocationGroupID    int64
	LocationGroupIDs   []int64
	IncludeUnassigned  bool
	LocationGroupName  string
	FirstName          string
	LastName           string
	CheckedOutAtBefore time.Time
	CheckedOutAtAfter  time.Time
	Limit              int
	Recent             bool
}

type Checkin struct {
	ID                    int64
	PlanningCenterID      string
	LocationID            int64
	EventID               int64
	FirstName             string
	LastName              string
	SecurityCode          string
	CheckedOutAt          time.Time
	FetchedAt             time.Time
	CheckedOutConfirmedAt time.Time
	LocationGroupID       *int64
}

type Repo interface {
	ListCheckins(ctx context.Context, filter Filter) ([]Checkin, error)
	CreateCheckin(ctx context.Context, checkin Checkin) (Checkin, error)
	SetCheckedOutConfirmedAt(ctx context.Context, planningCenterID string, confirmed bool) (Checkin, error)
	RemoveOldCheckins(ctx context.Context, olderThan time.Time) (deletedCount int64, err error)
	DeleteCheckin(ctx context.Context, id int64) error
	DeleteAllCheckins(ctx context.Context) (int64, error)
}

type sqliteRepo struct {
	db repo.DBTX
}

func NewRepo(db repo.DBTX) Repo {
	return &sqliteRepo{
		db: db,
	}
}

func (s *sqliteRepo) ListCheckins(ctx context.Context, filter Filter) ([]Checkin, error) {
	joinedTables := map[string]bool{}

	builder := squirrel.Select(
		"checkins.id",
		"checkins.planning_center_id",
		"checkins.location_id",
		"checkins.first_name",
		"checkins.last_name",
		"checkins.security_code",
		"checkins.checked_out_at",
		"checkins.fetched_at",
		"checkins.checked_out_confirmed_at",
		"locations.location_group_id",
	).From("checkins").LeftJoin("locations ON locations.id = checkins.location_id")
	joinedTables["locations"] = true

	if filter.LocationName != "" {
		builder = builder.Where(squirrel.Eq{"locations.name": filter.LocationName})
	}

	if filter.ID > 0 {
		builder = builder.Where(squirrel.Eq{"checkins.id": filter.ID})
	}

	if len(filter.LocationGroupIDs) > 0 && filter.IncludeUnassigned {
		builder = builder.Where(squirrel.Or{
			squirrel.Eq{"locations.location_group_id": filter.LocationGroupIDs},
			squirrel.Eq{"locations.location_group_id": nil},
		})
	} else if len(filter.LocationGroupIDs) > 0 {
		builder = builder.Where(squirrel.Eq{"locations.location_group_id": filter.LocationGroupIDs})
	} else if filter.IncludeUnassigned {
		builder = builder.Where(squirrel.Eq{"locations.location_group_id": nil})
	} else if filter.LocationGroupID > 0 {
		builder = builder.Where(squirrel.Eq{"locations.location_group_id": filter.LocationGroupID})
	}

	if filter.LocationGroupName != "" {
		if !joinedTables["location_groups"] {
			builder = builder.Join("location_groups ON location_groups.id = locations.location_group_id")
			joinedTables["location_groups"] = true
		}
		builder = builder.Where(squirrel.Eq{"location_groups.name": filter.LocationGroupName})
	}

	if filter.PlanningCenterID != "" {
		builder = builder.Where(squirrel.Eq{"checkins.planning_center_id": filter.PlanningCenterID})
	}

	if filter.FirstName != "" {
		builder = builder.Where(squirrel.Eq{"checkins.first_name": filter.FirstName})
	}

	if filter.LastName != "" {
		builder = builder.Where(squirrel.Eq{"checkins.last_name": filter.LastName})
	}

	if !filter.CheckedOutAtBefore.IsZero() {
		builder = builder.Where(squirrel.Lt{"checkins.checked_out_at": filter.CheckedOutAtBefore.UTC()})
	}

	if !filter.CheckedOutAtAfter.IsZero() {
		builder = builder.Where(squirrel.Gt{"checkins.checked_out_at": filter.CheckedOutAtAfter.UTC()})
	}

	if filter.LocationID > 0 {
		builder = builder.Where(squirrel.Eq{"checkins.location_id": filter.LocationID})
	}

	if filter.EventID > 0 {
		builder = builder.Where(squirrel.Eq{"checkins.event_id": filter.EventID})
	}

	if filter.Recent {
		builder = builder.OrderBy("checkins.checked_out_at DESC")
	}

	if filter.Limit > 0 {
		builder = builder.Limit(uint64(filter.Limit))
	}

	rows, err := builder.RunWith(s.db).QueryContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("querying checkins: %w", err)
	}
	defer rows.Close()
	checkins := make([]Checkin, 0)
	for rows.Next() {
		var checkin Checkin
		var checkedOutAt sql.NullTime
		var fetchedAt sql.NullTime
		var checkedOutConfirmedAt sql.NullTime
		var lgID sql.NullInt64

		err := rows.Scan(
			&checkin.ID,
			&checkin.PlanningCenterID,
			&checkin.LocationID,
			&checkin.FirstName,
			&checkin.LastName,
			&checkin.SecurityCode,
			&checkedOutAt,
			&fetchedAt,
			&checkedOutConfirmedAt,
			&lgID,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning checkin: %w", err)
		}

		if checkedOutAt.Valid {
			checkin.CheckedOutAt = checkedOutAt.Time
		}

		if fetchedAt.Valid {
			checkin.FetchedAt = fetchedAt.Time
		}

		if checkedOutConfirmedAt.Valid {
			checkin.CheckedOutConfirmedAt = checkedOutConfirmedAt.Time
		}

		if lgID.Valid {
			v := lgID.Int64
			checkin.LocationGroupID = &v
		}

		checkins = append(checkins, checkin)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating checkins: %w", err)
	}

	return checkins, nil
}

func (s *sqliteRepo) CreateCheckin(ctx context.Context, checkin Checkin) (Checkin, error) {
	var checkedOutAt *time.Time
	if !checkin.CheckedOutAt.IsZero() {
		tt := checkin.CheckedOutAt.UTC()
		checkedOutAt = &tt
	}

	var checkedOutConfirmedAt *time.Time
	if !checkin.CheckedOutConfirmedAt.IsZero() {
		tt := checkin.CheckedOutConfirmedAt.UTC()
		checkedOutConfirmedAt = &tt
	}

	var fetchedAt *time.Time
	if !checkin.FetchedAt.IsZero() {
		tt := checkin.FetchedAt.UTC()
		fetchedAt = &tt
	}

	columns := []string{"planning_center_id", "location_id", "first_name", "last_name", "security_code", "checked_out_at", "fetched_at", "checked_out_confirmed_at"}
	values := []any{checkin.PlanningCenterID, checkin.LocationID, checkin.FirstName, checkin.LastName, checkin.SecurityCode, checkedOutAt, fetchedAt, checkedOutConfirmedAt}
	if checkin.EventID > 0 {
		columns = append(columns, "event_id")
		values = append(values, checkin.EventID)
	}

	// The conflict update intentionally overwrites the stored checked_out_at and
	// checked_out_confirmed_at columns with the values passed in, clearing them
	// (setting NULL) when the incoming value is unset. This is intended behavior:
	// a re-fetched checkout from Planning Center carries no confirmation
	// timestamp, so its upsert resets the confirmed time to NULL. fetched_at is
	// different: first fetch wins. The existing value is kept via COALESCE so a
	// duplicate fetch never moves it, while a pre-existing NULL gets backfilled.
	conflictSuffix := squirrel.Expr("ON CONFLICT(planning_center_id) DO UPDATE SET checked_out_at = ?, checked_out_confirmed_at = ?, fetched_at = COALESCE(fetched_at, excluded.fetched_at)", checkedOutAt, checkedOutConfirmedAt)
	if checkin.EventID > 0 {
		conflictSuffix = squirrel.Expr("ON CONFLICT(planning_center_id) DO UPDATE SET checked_out_at = ?, checked_out_confirmed_at = ?, fetched_at = COALESCE(fetched_at, excluded.fetched_at), event_id = ?", checkedOutAt, checkedOutConfirmedAt, checkin.EventID)
	}

	builder := squirrel.Insert("checkins").
		RunWith(s.db).
		Columns(columns...).
		Values(values...).
		SuffixExpr(conflictSuffix)

	res, err := builder.ExecContext(ctx)
	if err != nil {
		return Checkin{}, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return Checkin{}, err
	}

	checkin.ID = id
	return checkin, nil
}

func (s *sqliteRepo) SetCheckedOutConfirmedAt(ctx context.Context, planningCenterID string, confirmed bool) (Checkin, error) {
	var checkedOutConfirmedAt *time.Time
	if confirmed {
		now := time.Now().UTC()
		checkedOutConfirmedAt = &now
	}

	res, err := squirrel.Update("checkins").
		Set("checked_out_confirmed_at", checkedOutConfirmedAt).
		Where(squirrel.Eq{"planning_center_id": planningCenterID}).
		RunWith(s.db).
		ExecContext(ctx)
	if err != nil {
		return Checkin{}, err
	}

	ra, err := res.RowsAffected()
	if err != nil {
		return Checkin{}, err
	}
	if ra == 0 {
		return Checkin{}, repo.ErrNotFound
	}

	checkins, err := s.ListCheckins(ctx, Filter{PlanningCenterID: planningCenterID, Limit: 1})
	if err != nil {
		return Checkin{}, err
	}
	if len(checkins) == 0 {
		return Checkin{}, repo.ErrNotFound
	}

	return checkins[0], nil
}

func (s *sqliteRepo) RemoveOldCheckins(ctx context.Context, olderThan time.Time) (int64, error) {
	if time.Now().Before(olderThan) {
		return 0, nil
	}

	res, err := squirrel.Delete("checkins").
		Where(squirrel.Lt{"checked_out_at": olderThan.UTC()}).
		RunWith(s.db).
		ExecContext(ctx)
	if err != nil {
		return 0, err
	}

	ra, _ := res.RowsAffected()
	return ra, err
}

func (s *sqliteRepo) DeleteCheckin(ctx context.Context, id int64) error {
	builder := squirrel.
		Delete("checkins").
		Where(squirrel.Eq{"id": id}).
		RunWith(s.db)

	res, err := builder.ExecContext(ctx)
	if err != nil {
		return fmt.Errorf("deleting checkin: %w", err)
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return repo.ErrNotFound
	}

	return nil
}

func (s *sqliteRepo) DeleteAllCheckins(ctx context.Context) (int64, error) {
	res, err := squirrel.Delete("checkins").RunWith(s.db).ExecContext(ctx)
	if err != nil {
		return 0, fmt.Errorf("deleting all checkins: %w", err)
	}

	ra, _ := res.RowsAffected()
	return ra, nil
}
