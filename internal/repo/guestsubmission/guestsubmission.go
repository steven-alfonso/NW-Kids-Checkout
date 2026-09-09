package guestsubmission

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
)

const (
	StatusPending = "pending"
	StatusEntered = "entered"
)

type Parent struct {
	ID        int64
	CreatedAt time.Time
	FirstName string
	LastName  string
	Phone     string
	Email     string
	Address1  string
	Address2  string
	City      string
	State     string
	Zip       string
}

type Child struct {
	ID                  int64
	ParentID            int64
	FirstName           string
	LastName            string
	DOB                 string
	Grade               string
	Gender              string
	DietaryRestrictions string
	SpecialNeeds        string
	Relationship        string
	CreatedAt           time.Time
}

type Submission struct {
	ID                   int64
	PublicID             string
	ParentID             int64
	Status               string
	EnteredAt            time.Time
	CheckinsBackfilledAt time.Time
	SafetyAck            bool
	CreatedAt            time.Time
	Parent               Parent
	Children             []Child
}

type Filter struct {
	Status                string
	PublicID              string
	WithoutManualCheckins bool
	Limit                 int
	Offset                int
}

type Repo interface {
	CreateSubmission(ctx context.Context, parent Parent, children []Child, safetyAck bool) (Submission, error)
	ListSubmissions(ctx context.Context, filter Filter) ([]Submission, error)
	CountSubmissions(ctx context.Context, filter Filter) (int, error)
	UpdateSubmissionStatus(ctx context.Context, publicID string, status string, now time.Time) error
	// CreateManualCheckins creates one manual_checkins row per child of the
	// submission without changing its status. If rows already exist for the
	// submission's children, this is a no-op (returns nil).
	CreateManualCheckins(ctx context.Context, publicID string) error
}

var (
	ErrConflict          = errors.New("conflict: submission status changed since last read")
	ErrInvalidStatus     = errors.New("invalid submission status")
	ErrInvalidSubmission = errors.New("invalid submission")
)

func statusFromTimestamps(entered bool) string {
	if entered {
		return StatusEntered
	}
	return StatusPending
}

func statusPredicate(status string) (squirrel.Sqlizer, error) {
	switch status {
	case StatusPending:
		return squirrel.Eq{"entered_at": nil}, nil
	case StatusEntered:
		return squirrel.NotEq{"entered_at": nil}, nil
	default:
		return nil, fmt.Errorf("unknown status: %s", status)
	}
}

func applyFilter(builder squirrel.SelectBuilder, filter Filter) (squirrel.SelectBuilder, error) {
	if filter.Status != "" {
		if strings.Contains(filter.Status, ",") {
			parts := strings.Split(filter.Status, ",")
			var ors squirrel.Or
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				pred, err := statusPredicate(p)
				if err != nil {
					return builder, err
				}
				ors = append(ors, pred)
			}
			if len(ors) == 0 {
				return builder, fmt.Errorf("unknown status: %s", filter.Status)
			}
			builder = builder.Where(ors)
		} else {
			statusFilter, err := statusPredicate(strings.TrimSpace(filter.Status))
			if err != nil {
				return builder, err
			}
			builder = builder.Where(statusFilter)
		}
	}
	if filter.PublicID != "" {
		builder = builder.Where(squirrel.Eq{"public_id": filter.PublicID})
	}
	if filter.WithoutManualCheckins {
		builder = builder.Where(withoutManualCheckinsExpr())
	}
	return builder, nil
}

type sqliteRepo struct {
	db *sql.DB
}

func NewRepo(db *sql.DB) Repo {
	return &sqliteRepo{db: db}
}

func normalizeGender(g string) string {
	return strings.ToLower(strings.TrimSpace(g))
}

func normalizeRelationship(r string) string {
	return strings.ToLower(strings.TrimSpace(r))
}

func (s *sqliteRepo) CreateSubmission(ctx context.Context, parent Parent, children []Child, safetyAck bool) (Submission, error) {
	if len(children) == 0 {
		return Submission{}, errors.New("at least one child is required")
	}
	// Normalize gender/relationship to lowercase to match DB CHECK constraints.
	for i := range children {
		children[i].Gender = normalizeGender(children[i].Gender)
		children[i].Relationship = normalizeRelationship(children[i].Relationship)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Submission{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO parents (first_name, last_name, phone, email, address1, address2, city, state, zip, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		parent.FirstName, parent.LastName, parent.Phone, parent.Email, parent.Address1, parent.Address2, parent.City, parent.State, parent.Zip, now)
	if err != nil {
		return Submission{}, fmt.Errorf("inserting parent: %w", err)
	}
	parentID, err := res.LastInsertId()
	if err != nil {
		return Submission{}, fmt.Errorf("getting parent id: %w", err)
	}

	createdChildren := make([]Child, 0, len(children))
	for i := range children {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO children (parent_id, first_name, last_name, dob, grade, gender, dietary_restrictions, special_needs, relationship, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			parentID, children[i].FirstName, children[i].LastName, children[i].DOB, children[i].Grade, children[i].Gender, children[i].DietaryRestrictions, children[i].SpecialNeeds, children[i].Relationship, now)
		if err != nil {
			return Submission{}, fmt.Errorf("inserting child %d: %w", i, err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return Submission{}, fmt.Errorf("getting child id %d: %w", i, err)
		}
		children[i].ID = id
		children[i].ParentID = parentID
		children[i].CreatedAt = now
		createdChildren = append(createdChildren, children[i])
	}

	publicID := uuid.New().String()
	safetyAckInt := 0
	if safetyAck {
		safetyAckInt = 1
	}
	res, err = tx.ExecContext(ctx,
		`INSERT INTO guest_submissions (public_id, parent_id, safety_ack, created_at) VALUES (?, ?, ?, ?)`,
		publicID, parentID, safetyAckInt, now)
	if err != nil {
		return Submission{}, fmt.Errorf("inserting submission: %w", err)
	}
	subID, err := res.LastInsertId()
	if err != nil {
		return Submission{}, fmt.Errorf("getting submission id: %w", err)
	}

	// Auto-create manual_checkins for each child
	for _, child := range createdChildren {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO manual_checkins (public_id, child_id, first_name, last_name, checked_out_at, checked_out_confirmed_at) VALUES (?, ?, ?, ?, NULL, NULL)`,
			uuid.New().String(), child.ID, child.FirstName, child.LastName); err != nil {
			return Submission{}, fmt.Errorf("inserting manual checkin for child %d: %w", child.ID, err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE guest_submissions SET checkins_backfilled_at = ? WHERE id = ?`,
		now, subID); err != nil {
		return Submission{}, fmt.Errorf("updating checkins_backfilled_at: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Submission{}, fmt.Errorf("commit tx: %w", err)
	}

	parent.ID = parentID
	parent.CreatedAt = now

	return Submission{
		ID:                   subID,
		PublicID:             publicID,
		ParentID:             parentID,
		Status:               StatusPending,
		EnteredAt:            time.Time{},
		CheckinsBackfilledAt: now,
		SafetyAck:            safetyAck,
		CreatedAt:            now,
		Parent:               parent,
		Children:             createdChildren,
	}, nil
}

func (s *sqliteRepo) ListSubmissions(ctx context.Context, filter Filter) ([]Submission, error) {
	builder := squirrel.Select(
		"id", "public_id", "parent_id",
		"entered_at", "checkins_backfilled_at", "safety_ack", "created_at",
	).From("guest_submissions")

	builder, err := applyFilter(builder, filter)
	if err != nil {
		return nil, err
	}
	builder = builder.OrderBy("created_at DESC, id DESC")
	if filter.Limit > 0 {
		builder = builder.Limit(uint64(filter.Limit))
	}
	if filter.Offset > 0 {
		builder = builder.Offset(uint64(filter.Offset))
	}

	rows, err := builder.RunWith(s.db).QueryContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("querying guest submissions: %w", err)
	}
	defer rows.Close()

	submissions := make([]Submission, 0)
	parentIDs := make([]int64, 0)
	for rows.Next() {
		var sub Submission
		var enteredAt, checkinsBackfilledAt sql.NullTime
		var safetyAck sql.NullInt64
		err := rows.Scan(
			&sub.ID, &sub.PublicID, &sub.ParentID,
			&enteredAt, &checkinsBackfilledAt, &safetyAck, &sub.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning guest submission: %w", err)
		}
		sub.EnteredAt = enteredAt.Time
		sub.CheckinsBackfilledAt = checkinsBackfilledAt.Time
		sub.SafetyAck = safetyAck.Valid && safetyAck.Int64 != 0
		sub.Status = statusFromTimestamps(enteredAt.Valid)
		submissions = append(submissions, sub)
		parentIDs = append(parentIDs, sub.ParentID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating guest submissions: %w", err)
	}

	parents := make(map[int64]Parent)
	if len(parentIDs) > 0 {
		pRows, err := squirrel.Select("id", "created_at", "first_name", "last_name", "phone", "email", "address1", "address2", "city", "state", "zip").
			From("parents").
			Where(squirrel.Eq{"id": parentIDs}).
			RunWith(s.db).
			QueryContext(ctx)
		if err != nil {
			return nil, fmt.Errorf("querying parents: %w", err)
		}
		defer pRows.Close()
		for pRows.Next() {
			var p Parent
			if err := pRows.Scan(&p.ID, &p.CreatedAt, &p.FirstName, &p.LastName, &p.Phone, &p.Email, &p.Address1, &p.Address2, &p.City, &p.State, &p.Zip); err != nil {
				return nil, fmt.Errorf("scanning parent: %w", err)
			}
			parents[p.ID] = p
		}
		if err := pRows.Err(); err != nil {
			return nil, fmt.Errorf("iterating parents: %w", err)
		}
	}

	childrenByParent := make(map[int64][]Child)
	if len(parentIDs) > 0 {
		cRows, err := squirrel.Select("id", "parent_id", "first_name", "last_name", "dob", "grade", "gender", "dietary_restrictions", "special_needs", "relationship", "created_at").
			From("children").
			Where(squirrel.Eq{"parent_id": parentIDs}).
			OrderBy("id").
			RunWith(s.db).
			QueryContext(ctx)
		if err != nil {
			return nil, fmt.Errorf("querying children: %w", err)
		}
		defer cRows.Close()
		for cRows.Next() {
			var c Child
			if err := cRows.Scan(&c.ID, &c.ParentID, &c.FirstName, &c.LastName, &c.DOB, &c.Grade, &c.Gender, &c.DietaryRestrictions, &c.SpecialNeeds, &c.Relationship, &c.CreatedAt); err != nil {
				return nil, fmt.Errorf("scanning child: %w", err)
			}
			childrenByParent[c.ParentID] = append(childrenByParent[c.ParentID], c)
		}
		if err := cRows.Err(); err != nil {
			return nil, fmt.Errorf("iterating children: %w", err)
		}
	}

	for i := range submissions {
		submissions[i].Parent = parents[submissions[i].ParentID]
		submissions[i].Children = childrenByParent[submissions[i].ParentID]
	}

	return submissions, nil
}

func (s *sqliteRepo) CountSubmissions(ctx context.Context, filter Filter) (int, error) {
	builder, err := applyFilter(squirrel.Select("COUNT(*)").From("guest_submissions"), filter)
	if err != nil {
		return 0, err
	}

	var total int
	if err := builder.RunWith(s.db).QueryRowContext(ctx).Scan(&total); err != nil {
		return 0, fmt.Errorf("counting guest submissions: %w", err)
	}
	return total, nil
}

func (s *sqliteRepo) UpdateSubmissionStatus(ctx context.Context, publicID string, status string, now time.Time) error {
	builder := squirrel.Update("guest_submissions").Where(squirrel.Eq{"public_id": publicID})

	switch status {
	case StatusEntered:
		builder = builder.
			Set("entered_at", now.UTC()).
			Where(squirrel.Eq{"entered_at": nil})
	default:
		return fmt.Errorf("unknown status: %s", status)
	}

	res, err := builder.RunWith(s.db).ExecContext(ctx)
	if err != nil {
		return fmt.Errorf("updating submission status: %w", err)
	}
	ra, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if ra == 0 {
		var exists bool
		err = squirrel.Select("1").From("guest_submissions").
			Where(squirrel.Eq{"public_id": publicID}).
			RunWith(s.db).QueryRowContext(ctx).Scan(&exists)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return repo.ErrNotFound
			}
			return fmt.Errorf("checking submission existence: %w", err)
		}
		return ErrConflict
	}
	return nil
}

func (s *sqliteRepo) CreateManualCheckins(ctx context.Context, publicID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var parentID int64
	var enteredAt sql.NullTime
	err = squirrel.Select("parent_id", "entered_at").
		From("guest_submissions").
		Where(squirrel.Eq{"public_id": publicID}).
		RunWith(tx).
		QueryRowContext(ctx).
		Scan(&parentID, &enteredAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return repo.ErrNotFound
		}
		return fmt.Errorf("querying guest submission: %w", err)
	}
	_ = enteredAt

	if err := s.insertManualCheckins(ctx, tx, parentID); err != nil {
		return err
	}

	var remaining int
	if err := squirrel.Select("COUNT(*)").From("children").
		Where(squirrel.Eq{"parent_id": parentID}).
		Where(childWithoutManualCheckinExpr()).
		RunWith(tx).QueryRowContext(ctx).Scan(&remaining); err != nil {
		return fmt.Errorf("checking remaining children without checkins: %w", err)
	}
	if remaining == 0 {
		if _, err := squirrel.Update("guest_submissions").
			Set("checkins_backfilled_at", time.Now().UTC()).
			Where(squirrel.Eq{"parent_id": parentID}).
			Where(squirrel.Eq{"checkins_backfilled_at": nil}).
			RunWith(tx).ExecContext(ctx); err != nil {
			return fmt.Errorf("updating checkins_backfilled_at: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

type checkinChild struct {
	ID        int64
	FirstName string
	LastName  string
}

func (s *sqliteRepo) insertManualCheckins(ctx context.Context, tx *sql.Tx, parentID int64) error {
	rows, err := squirrel.Select("id", "first_name", "last_name").
		From("children").
		Where(squirrel.Eq{"parent_id": parentID}).
		Where(childWithoutManualCheckinExpr()).
		OrderBy("id").
		RunWith(tx).
		QueryContext(ctx)
	if err != nil {
		return fmt.Errorf("querying children: %w", err)
	}
	defer rows.Close()
	children := make([]checkinChild, 0)
	for rows.Next() {
		var child checkinChild
		if err := rows.Scan(&child.ID, &child.FirstName, &child.LastName); err != nil {
			return fmt.Errorf("scanning child: %w", err)
		}
		children = append(children, child)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating children: %w", err)
	}

	if len(children) == 0 {
		var total int
		if err := squirrel.Select("COUNT(*)").From("children").
			Where(squirrel.Eq{"parent_id": parentID}).
			RunWith(tx).
			QueryRowContext(ctx).
			Scan(&total); err != nil {
			return fmt.Errorf("checking children count: %w", err)
		}
		if total == 0 {
			return fmt.Errorf("%w: submission has no children", ErrInvalidSubmission)
		}
		return nil // all children already have manual_checkins; per-child no-op
	}

	for _, child := range children {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO manual_checkins (public_id, child_id, first_name, last_name, checked_out_at, checked_out_confirmed_at) VALUES (?, ?, ?, ?, NULL, NULL)`,
			uuid.New().String(), child.ID, child.FirstName, child.LastName); err != nil {
			return fmt.Errorf("inserting manual checkin: %w", err)
		}
	}
	return nil
}

func withoutManualCheckinsExpr() squirrel.Sqlizer {
	return squirrel.Eq{"checkins_backfilled_at": nil}
}

func childWithoutManualCheckinExpr() squirrel.Sqlizer {
	return squirrel.Expr("NOT EXISTS (SELECT 1 FROM manual_checkins mc WHERE mc.child_id = children.id)")
}
