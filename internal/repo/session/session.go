package session

import (
	"context"
	"fmt"

	"kids-checkin/internal/repo"

	"github.com/Masterminds/squirrel"
)

// Table is the session store's table. StartServer in
// internal/controllers/server.go passes it to sqlite3.Config, which otherwise
// defaults to this same name. The table also holds Planning Center pagination
// cursors (see planningcenterv1), not only user sessions.
const Table = "fiber_storage"

type Repo interface {
	Purge(ctx context.Context) (deleted int64, err error)
}

type sqliteRepo struct {
	db repo.DBTX
}

func NewRepo(db repo.DBTX) Repo {
	return &sqliteRepo{
		db: db,
	}
}

// Purge deletes every row in the session table, including unexpired ones, so
// every user must log in again, and returns how many rows it deleted.
func (r *sqliteRepo) Purge(ctx context.Context) (int64, error) {
	result, err := squirrel.Delete(Table).RunWith(r.db).ExecContext(ctx)
	if err != nil {
		return 0, fmt.Errorf("deleting sessions: %w", err)
	}

	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("reading deleted session count: %w", err)
	}

	return deleted, nil
}
