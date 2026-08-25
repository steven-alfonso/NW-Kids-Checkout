package parent

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
)

type Parent struct {
	ID        int64
	CreatedAt time.Time
	FirstName string
	LastName  string
	Phone     string
	Email     string
}

type Repo interface {
	CreateParent(ctx context.Context, p Parent) (Parent, error)
}

type sqliteRepo struct {
	db *sql.DB
}

func NewRepo(db *sql.DB) Repo {
	return &sqliteRepo{db: db}
}

func (s *sqliteRepo) CreateParent(ctx context.Context, p Parent) (Parent, error) {
	now := time.Now().UTC()

	res, err := squirrel.Insert("parents").
		Columns("first_name", "last_name", "phone", "email", "created_at").
		Values(p.FirstName, p.LastName, p.Phone, p.Email, now).
		RunWith(s.db).
		ExecContext(ctx)
	if err != nil {
		return Parent{}, fmt.Errorf("inserting parent: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return Parent{}, fmt.Errorf("getting parent id: %w", err)
	}

	p.ID = id
	p.CreatedAt = now
	return p, nil
}
