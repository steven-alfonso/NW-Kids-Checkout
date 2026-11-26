package session

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Masterminds/squirrel"
)

// table is the session store's table. StartServer in
// internal/controllers/server.go configures sqlite3.Config without a Table, so
// the storage's default applies: fiber_storage. The same table also holds
// Planning Center pagination cursors (see planningcenterv1).
const table = "fiber_storage"

type Repo struct {
	db *sql.DB
}

func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

type PurgeResult struct {
	Before int64
	After  int64
}

// Purge deletes every row in the session table, including unexpired ones, so
// every user must log in again. It reports the row counts before and after the
// delete so the caller can log the effect.
func (r *Repo) Purge(ctx context.Context) (PurgeResult, error) {
	var result PurgeResult

	err := squirrel.Select("COUNT(*)").From(table).
		RunWith(r.db).
		QueryRowContext(ctx).
		Scan(&result.Before)
	if err != nil {
		return result, fmt.Errorf("count sessions before delete: %w", err)
	}

	_, err = squirrel.Delete(table).RunWith(r.db).ExecContext(ctx)
	if err != nil {
		return result, fmt.Errorf("delete sessions: %w", err)
	}

	err = squirrel.Select("COUNT(*)").From(table).
		RunWith(r.db).
		QueryRowContext(ctx).
		Scan(&result.After)
	if err != nil {
		return result, fmt.Errorf("count sessions after delete: %w", err)
	}

	return result, nil
}
