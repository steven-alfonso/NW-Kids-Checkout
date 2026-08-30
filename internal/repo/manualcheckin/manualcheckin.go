package manualcheckin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"kids-checkin/internal/repo"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/mattn/go-sqlite3"
)

var ErrInvalidManualCheckin = errors.New("invalid manual checkin")

type Filter struct {
	ID                 int64
	PublicID           string
	FirstName          string
	LastName           string
	CheckedOutAtBefore time.Time
	CheckedOutAtAfter  time.Time
	Limit              int
	Recent             bool
	IncludeUnchecked   bool
}

type ManualCheckin struct {
	ID                    int64
	CreatedAt             time.Time
	PublicID              string
	ChildID               int64
	FirstName             string
	LastName              string
	CheckedOutAt          time.Time
	CheckedOutConfirmedAt time.Time
}

type Repo interface {
	ListManualCheckins(ctx context.Context, filter Filter) ([]ManualCheckin, error)
	CreateManualCheckin(ctx context.Context, manualCheckin ManualCheckin) (ManualCheckin, error)
	SetManualCheckedOutAt(ctx context.Context, id int64, checkedOut bool) (ManualCheckin, error)
	SetManualCheckedOutConfirmedAt(ctx context.Context, id int64, confirmed bool) (ManualCheckin, error)
	RemoveOldManualCheckins(ctx context.Context, olderThan time.Time) (deletedCount int64, err error)
}

type sqliteRepo struct {
	db *sql.DB
}

func NewRepo(db *sql.DB) Repo {
	return &sqliteRepo{
		db: db,
	}
}

func (s *sqliteRepo) ListManualCheckins(ctx context.Context, filter Filter) ([]ManualCheckin, error) {
	builder := squirrel.Select(
		"manual_checkins.id",
		"manual_checkins.created_at",
		"manual_checkins.public_id",
		"manual_checkins.child_id",
		"manual_checkins.first_name",
		"manual_checkins.last_name",
		"manual_checkins.checked_out_at",
		"manual_checkins.checked_out_confirmed_at",
	).From("manual_checkins")

	if filter.ID > 0 {
		builder = builder.Where(squirrel.Eq{"manual_checkins.id": filter.ID})
	}

	if filter.PublicID != "" {
		builder = builder.Where(squirrel.Eq{"manual_checkins.public_id": filter.PublicID})
	}

	if filter.FirstName != "" {
		builder = builder.Where(squirrel.Eq{"manual_checkins.first_name": filter.FirstName})
	}

	if filter.LastName != "" {
		builder = builder.Where(squirrel.Eq{"manual_checkins.last_name": filter.LastName})
	}

	if !filter.CheckedOutAtBefore.IsZero() {
		builder = builder.Where(squirrel.Lt{"manual_checkins.checked_out_at": filter.CheckedOutAtBefore.UTC()})
	}

	if !filter.CheckedOutAtAfter.IsZero() {
		if filter.IncludeUnchecked {
			builder = builder.Where(squirrel.Or{
				squirrel.Gt{"manual_checkins.checked_out_at": filter.CheckedOutAtAfter.UTC()},
				squirrel.Eq{"manual_checkins.checked_out_at": nil},
			})
		} else {
			builder = builder.Where(squirrel.Gt{"manual_checkins.checked_out_at": filter.CheckedOutAtAfter.UTC()})
		}
	}

	if filter.Recent {
		builder = builder.OrderBy("manual_checkins.checked_out_at DESC")
	}

	if filter.Limit > 0 {
		builder = builder.Limit(uint64(filter.Limit))
	}

	rows, err := builder.RunWith(s.db).QueryContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("querying manual checkins: %w", err)
	}
	defer rows.Close()

	manualCheckins := make([]ManualCheckin, 0)
	for rows.Next() {
		var manualCheckin ManualCheckin
		var checkedOutAt sql.NullTime
		var checkedOutConfirmedAt sql.NullTime
		var childID sql.NullInt64

		err := rows.Scan(
			&manualCheckin.ID,
			&manualCheckin.CreatedAt,
			&manualCheckin.PublicID,
			&childID,
			&manualCheckin.FirstName,
			&manualCheckin.LastName,
			&checkedOutAt,
			&checkedOutConfirmedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning manual checkin: %w", err)
		}

		if childID.Valid {
			manualCheckin.ChildID = childID.Int64
		}

		if checkedOutAt.Valid {
			manualCheckin.CheckedOutAt = checkedOutAt.Time
		}

		if checkedOutConfirmedAt.Valid {
			manualCheckin.CheckedOutConfirmedAt = checkedOutConfirmedAt.Time
		}

		manualCheckins = append(manualCheckins, manualCheckin)
	}

	return manualCheckins, nil
}

func (s *sqliteRepo) CreateManualCheckin(ctx context.Context, manualCheckin ManualCheckin) (ManualCheckin, error) {
	// Names required even with child_id to ensure standalone guest checkin data completeness; stricter than DB CHECK which permits blank names with child_id.
	if strings.TrimSpace(manualCheckin.FirstName) == "" || strings.TrimSpace(manualCheckin.LastName) == "" {
		return ManualCheckin{}, ErrInvalidManualCheckin
	}

	if manualCheckin.PublicID == "" {
		manualCheckin.PublicID = uuid.New().String()
	}

	var checkedOutAt *time.Time
	if !manualCheckin.CheckedOutAt.IsZero() {
		tt := manualCheckin.CheckedOutAt.UTC()
		checkedOutAt = &tt
	}

	var checkedOutConfirmedAt *time.Time
	if !manualCheckin.CheckedOutConfirmedAt.IsZero() {
		tt := manualCheckin.CheckedOutConfirmedAt.UTC()
		checkedOutConfirmedAt = &tt
	}

	var childID *int64
	if manualCheckin.ChildID > 0 {
		id := manualCheckin.ChildID
		childID = &id

		var exists int
		err := squirrel.Select("1").From("children").Where(squirrel.Eq{"id": manualCheckin.ChildID}).RunWith(s.db).QueryRowContext(ctx).Scan(&exists)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ManualCheckin{}, fmt.Errorf("%w: child %d not found", ErrInvalidManualCheckin, manualCheckin.ChildID)
			}
			return ManualCheckin{}, fmt.Errorf("validating child_id: %w", err)
		}
	}

	builder := squirrel.Insert("manual_checkins").
		RunWith(s.db).
		Columns("public_id", "child_id", "first_name", "last_name", "checked_out_at", "checked_out_confirmed_at").
		Values(manualCheckin.PublicID, childID, manualCheckin.FirstName, manualCheckin.LastName, checkedOutAt, checkedOutConfirmedAt)

	res, err := builder.ExecContext(ctx)
	if err != nil {
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) && sqliteErr.ExtendedCode == sqlite3.ErrConstraintForeignKey {
			return ManualCheckin{}, fmt.Errorf("%w: %w", ErrInvalidManualCheckin, err)
		}
		return ManualCheckin{}, fmt.Errorf("creating manual checkin: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return ManualCheckin{}, err
	}

	manualCheckin.ID = id

	err = squirrel.Select("created_at").
		From("manual_checkins").
		Where(squirrel.Eq{"id": id}).
		RunWith(s.db).
		QueryRowContext(ctx).
		Scan(&manualCheckin.CreatedAt)
	if err != nil {
		return ManualCheckin{}, fmt.Errorf("fetching created_at: %w", err)
	}

	return manualCheckin, nil
}

func (s *sqliteRepo) SetManualCheckedOutAt(ctx context.Context, id int64, checkedOut bool) (ManualCheckin, error) {
	var checkedOutAt *time.Time
	updateBuilder := squirrel.Update("manual_checkins").
		Where(squirrel.Eq{"id": id}).
		RunWith(s.db)

	if checkedOut {
		now := time.Now().UTC()
		checkedOutAt = &now
	}
	updateBuilder = updateBuilder.Set("checked_out_at", checkedOutAt)
	if !checkedOut {
		updateBuilder = updateBuilder.Set("checked_out_confirmed_at", nil)
	}

	res, err := updateBuilder.ExecContext(ctx)
	if err != nil {
		return ManualCheckin{}, err
	}

	ra, err := res.RowsAffected()
	if err != nil {
		return ManualCheckin{}, err
	}
	if ra == 0 {
		return ManualCheckin{}, repo.ErrNotFound
	}

	manualCheckins, err := s.ListManualCheckins(ctx, Filter{ID: id, Limit: 1})
	if err != nil {
		return ManualCheckin{}, err
	}
	if len(manualCheckins) == 0 {
		return ManualCheckin{}, repo.ErrNotFound
	}

	return manualCheckins[0], nil
}

func (s *sqliteRepo) SetManualCheckedOutConfirmedAt(ctx context.Context, id int64, confirmed bool) (ManualCheckin, error) {
	var checkedOutConfirmedAt *time.Time
	if confirmed {
		now := time.Now().UTC()
		checkedOutConfirmedAt = &now
	}

	res, err := squirrel.Update("manual_checkins").
		Set("checked_out_confirmed_at", checkedOutConfirmedAt).
		Where(squirrel.Eq{"id": id}).
		RunWith(s.db).
		ExecContext(ctx)
	if err != nil {
		return ManualCheckin{}, err
	}

	ra, err := res.RowsAffected()
	if err != nil {
		return ManualCheckin{}, err
	}
	if ra == 0 {
		return ManualCheckin{}, repo.ErrNotFound
	}

	manualCheckins, err := s.ListManualCheckins(ctx, Filter{ID: id, Limit: 1})
	if err != nil {
		return ManualCheckin{}, err
	}
	if len(manualCheckins) == 0 {
		return ManualCheckin{}, repo.ErrNotFound
	}

	return manualCheckins[0], nil
}

func (s *sqliteRepo) RemoveOldManualCheckins(ctx context.Context, olderThan time.Time) (int64, error) {
	if time.Now().Before(olderThan) {
		return 0, nil
	}

	res, err := squirrel.Delete("manual_checkins").
		Where(squirrel.Lt{"checked_out_at": olderThan.UTC()}).
		RunWith(s.db).
		ExecContext(ctx)
	if err != nil {
		return 0, err
	}

	ra, _ := res.RowsAffected()
	return ra, err
}
