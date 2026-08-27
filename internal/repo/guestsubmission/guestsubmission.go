package guestsubmission

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"kids-checkin/internal/repo"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
)

const (
	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusRejected = "rejected"
	StatusEntered  = "entered"
)

type Parent struct {
	ID        int64
	CreatedAt time.Time
	FirstName string
	LastName  string
	Phone     string
	Email     string
}

type Child struct {
	ID        int64
	ParentID  int64
	FirstName string
	LastName  string
	DOB       string
	Grade     string
	CreatedAt time.Time
}

type Submission struct {
	ID         int64
	PublicID   string
	ParentID   int64
	Status     string
	ApprovedAt time.Time
	RejectedAt time.Time
	EnteredAt  time.Time
	CreatedAt  time.Time
	Parent     Parent
	Children   []Child
}

type Filter struct {
	Status                string
	PublicID              string
	WithoutManualCheckins bool
}

type Repo interface {
	CreateSubmission(ctx context.Context, parent Parent, children []Child) (Submission, error)
	ListSubmissions(ctx context.Context, filter Filter) ([]Submission, error)
	UpdateSubmissionStatus(ctx context.Context, publicID string, status string, now time.Time) error
	// ApproveSubmission creates one manual_checkins row per child of the
	// (pending) submission and transitions it to approved in a single
	// transaction. Returns repo.ErrNotFound if publicID is unknown.
	ApproveSubmission(ctx context.Context, publicID string, now time.Time) error
	// CreateManualCheckins creates one manual_checkins row per child of the
	// submission without changing its status. Returns ErrManualCheckinsExist
	// if rows already exist for the submission's children.
	CreateManualCheckins(ctx context.Context, publicID string) error
}

var (
	ErrManualCheckinsExist = errors.New("manual check-ins already exist for this submission")
	ErrConflict            = errors.New("conflict: submission status changed since last read")
)

func statusFromTimestamps(approved, rejected, entered bool) string {
	switch {
	case entered:
		return StatusEntered
	case approved:
		return StatusApproved
	case rejected:
		return StatusRejected
	default:
		return StatusPending
	}
}

func statusPredicate(status string) (squirrel.Sqlizer, error) {
	switch status {
	case StatusPending:
		return squirrel.And{
			squirrel.Eq{"approved_at": nil},
			squirrel.Eq{"rejected_at": nil},
			squirrel.Eq{"entered_at": nil},
		}, nil
	case StatusApproved:
		return squirrel.And{
			squirrel.NotEq{"approved_at": nil},
			squirrel.Eq{"entered_at": nil},
		}, nil
	case StatusRejected:
		return squirrel.And{
			squirrel.NotEq{"rejected_at": nil},
			squirrel.Eq{"approved_at": nil},
			squirrel.Eq{"entered_at": nil},
		}, nil
	case StatusEntered:
		return squirrel.NotEq{"entered_at": nil}, nil
	default:
		return nil, fmt.Errorf("unknown status: %s", status)
	}
}

type sqliteRepo struct {
	db *sql.DB
}

func NewRepo(db *sql.DB) Repo {
	return &sqliteRepo{db: db}
}

func (s *sqliteRepo) CreateSubmission(ctx context.Context, parent Parent, children []Child) (Submission, error) {
	if len(children) == 0 {
		return Submission{}, errors.New("at least one child is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Submission{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO parents (first_name, last_name, phone, email, created_at) VALUES (?, ?, ?, ?, ?)`,
		parent.FirstName, parent.LastName, parent.Phone, parent.Email, now)
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
			`INSERT INTO children (parent_id, first_name, last_name, dob, grade, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			parentID, children[i].FirstName, children[i].LastName, children[i].DOB, children[i].Grade, now)
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
	res, err = tx.ExecContext(ctx,
		`INSERT INTO guest_submissions (public_id, parent_id, created_at) VALUES (?, ?, ?)`,
		publicID, parentID, now)
	if err != nil {
		return Submission{}, fmt.Errorf("inserting submission: %w", err)
	}
	subID, err := res.LastInsertId()
	if err != nil {
		return Submission{}, fmt.Errorf("getting submission id: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Submission{}, fmt.Errorf("commit tx: %w", err)
	}

	parent.ID = parentID
	parent.CreatedAt = now

	return Submission{
		ID:        subID,
		PublicID:  publicID,
		ParentID:  parentID,
		Status:    StatusPending,
		CreatedAt: now,
		Parent:    parent,
		Children:  createdChildren,
	}, nil
}

func (s *sqliteRepo) ListSubmissions(ctx context.Context, filter Filter) ([]Submission, error) {
	builder := squirrel.Select(
		"id", "public_id", "parent_id",
		"approved_at", "rejected_at", "entered_at", "created_at",
	).From("guest_submissions")

	if filter.Status != "" {
		statusFilter, err := statusPredicate(filter.Status)
		if err != nil {
			return nil, err
		}
		builder = builder.Where(statusFilter)
	}
	if filter.PublicID != "" {
		builder = builder.Where(squirrel.Eq{"public_id": filter.PublicID})
	}
	if filter.WithoutManualCheckins {
		builder = builder.Where(squirrel.Expr(
			"NOT EXISTS (SELECT 1 FROM manual_checkins mc JOIN children ch ON ch.id = mc.child_id WHERE ch.parent_id = guest_submissions.parent_id)"))
	}
	builder = builder.OrderBy("created_at DESC")

	rows, err := builder.RunWith(s.db).QueryContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("querying guest submissions: %w", err)
	}
	defer rows.Close()

	submissions := make([]Submission, 0)
	parentIDs := make([]int64, 0)
	for rows.Next() {
		var sub Submission
		var approvedAt, rejectedAt, enteredAt sql.NullTime
		err := rows.Scan(
			&sub.ID, &sub.PublicID, &sub.ParentID,
			&approvedAt, &rejectedAt, &enteredAt, &sub.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning guest submission: %w", err)
		}
		sub.ApprovedAt = approvedAt.Time
		sub.RejectedAt = rejectedAt.Time
		sub.EnteredAt = enteredAt.Time
		sub.Status = statusFromTimestamps(approvedAt.Valid, rejectedAt.Valid, enteredAt.Valid)
		submissions = append(submissions, sub)
		parentIDs = append(parentIDs, sub.ParentID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating guest submissions: %w", err)
	}

	parents := make(map[int64]Parent)
	if len(parentIDs) > 0 {
		pRows, err := squirrel.Select("id", "created_at", "first_name", "last_name", "phone", "email").
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
			if err := pRows.Scan(&p.ID, &p.CreatedAt, &p.FirstName, &p.LastName, &p.Phone, &p.Email); err != nil {
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
		cRows, err := squirrel.Select("id", "parent_id", "first_name", "last_name", "dob", "grade", "created_at").
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
			if err := cRows.Scan(&c.ID, &c.ParentID, &c.FirstName, &c.LastName, &c.DOB, &c.Grade, &c.CreatedAt); err != nil {
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

func (s *sqliteRepo) UpdateSubmissionStatus(ctx context.Context, publicID string, status string, now time.Time) error {
	builder := squirrel.Update("guest_submissions").Where(squirrel.Eq{"public_id": publicID})

	switch status {
	case StatusApproved:
		builder = builder.
			Set("approved_at", now.UTC()).Set("rejected_at", nil).Set("entered_at", nil).
			Where(squirrel.And{
				squirrel.Eq{"approved_at": nil},
				squirrel.Eq{"rejected_at": nil},
				squirrel.Eq{"entered_at": nil},
			})
	case StatusRejected:
		builder = builder.
			Set("rejected_at", now.UTC()).Set("approved_at", nil).Set("entered_at", nil).
			Where(squirrel.And{
				squirrel.Eq{"approved_at": nil},
				squirrel.Eq{"rejected_at": nil},
				squirrel.Eq{"entered_at": nil},
			})
	case StatusEntered:
		builder = builder.
			Set("entered_at", now.UTC()).Set("approved_at", nil).Set("rejected_at", nil).
			Where(squirrel.And{
				squirrel.Eq{"entered_at": nil},
				squirrel.Or{
					squirrel.And{squirrel.Eq{"approved_at": nil}, squirrel.Eq{"rejected_at": nil}},
					squirrel.NotEq{"approved_at": nil},
				},
			})
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

func (s *sqliteRepo) ApproveSubmission(ctx context.Context, publicID string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var parentID int64
	var approvedAt, rejectedAt, enteredAt sql.NullTime
	err = squirrel.Select("parent_id", "approved_at", "rejected_at", "entered_at").
		From("guest_submissions").
		Where(squirrel.Eq{"public_id": publicID}).
		RunWith(tx).
		QueryRowContext(ctx).
		Scan(&parentID, &approvedAt, &rejectedAt, &enteredAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return repo.ErrNotFound
		}
		return fmt.Errorf("querying guest submission: %w", err)
	}
	if approvedAt.Valid || rejectedAt.Valid || enteredAt.Valid {
		return fmt.Errorf("cannot approve submission in status %s", statusFromTimestamps(approvedAt.Valid, rejectedAt.Valid, enteredAt.Valid))
	}

	if err := s.insertManualCheckins(ctx, tx, parentID); err != nil {
		return err
	}

	if _, err := squirrel.Update("guest_submissions").
		Set("approved_at", now.UTC()).
		Set("rejected_at", nil).
		Set("entered_at", nil).
		Where(squirrel.Eq{"public_id": publicID}).
		Where(squirrel.And{
			squirrel.Eq{"approved_at": nil},
			squirrel.Eq{"rejected_at": nil},
			squirrel.Eq{"entered_at": nil},
		}).
		RunWith(tx).
		ExecContext(ctx); err != nil {
		return fmt.Errorf("updating submission status: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
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
	var rejectedAt sql.NullTime
	err = squirrel.Select("parent_id", "rejected_at").
		From("guest_submissions").
		Where(squirrel.Eq{"public_id": publicID}).
		RunWith(tx).
		QueryRowContext(ctx).
		Scan(&parentID, &rejectedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return repo.ErrNotFound
		}
		return fmt.Errorf("querying guest submission: %w", err)
	}
	if rejectedAt.Valid {
		return fmt.Errorf("cannot create manual check-ins for rejected submission")
	}

	if err := s.insertManualCheckins(ctx, tx, parentID); err != nil {
		return err
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
		return errors.New("submission has no children")
	}

	var existing int
	if err := squirrel.Select("COUNT(*)").From("manual_checkins").
		Where(squirrel.Eq{"child_id": func() []int64 {
			ids := make([]int64, 0, len(children))
			for _, child := range children {
				ids = append(ids, child.ID)
			}
			return ids
		}()}).
		RunWith(tx).
		QueryRowContext(ctx).
		Scan(&existing); err != nil {
		return fmt.Errorf("checking existing manual checkins: %w", err)
	}
	if existing > 0 {
		return ErrManualCheckinsExist
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
