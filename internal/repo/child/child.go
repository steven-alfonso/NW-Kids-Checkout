package child

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
)

type Child struct {
	ID        int64
	ParentID  int64
	FirstName string
	LastName  string
	DOB       string
	Grade     string
	CreatedAt time.Time
}

type Repo interface {
	CreateChild(ctx context.Context, c Child) (Child, error)
}

type sqliteRepo struct {
	db *sql.DB
}

func NewRepo(db *sql.DB) Repo {
	return &sqliteRepo{db: db}
}

func (s *sqliteRepo) CreateChild(ctx context.Context, c Child) (Child, error) {
	now := time.Now().UTC()

	res, err := squirrel.Insert("children").
		Columns("parent_id", "first_name", "last_name", "dob", "grade", "created_at").
		Values(c.ParentID, c.FirstName, c.LastName, c.DOB, c.Grade, now).
		RunWith(s.db).
		ExecContext(ctx)
	if err != nil {
		return Child{}, fmt.Errorf("inserting child: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return Child{}, fmt.Errorf("getting child id: %w", err)
	}

	c.ID = id
	c.CreatedAt = now
	return c, nil
}
