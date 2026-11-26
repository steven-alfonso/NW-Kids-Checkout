package session

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofiber/storage/sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/mattn/go-sqlite3"
)

func TestRepo_Purge(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "kids-checkin.db")

	store := sqlite3.New(sqlite3.Config{Database: dbPath, Table: Table})
	defer store.Close()
	require.NoError(t, store.Set("session-1", []byte("signed-session"), 24*time.Hour))

	deleted, err := NewRepo(store.Conn()).Purge(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	// The table still exists; only its rows are gone.
	var count int
	require.NoError(t, store.Conn().QueryRow("SELECT COUNT(*) FROM fiber_storage").Scan(&count))
	assert.Equal(t, 0, count)

	got, err := store.Get("session-1")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestRepo_Purge_missingTable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "plain.db")

	database, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer database.Close()

	_, err = database.Exec("CREATE TABLE unrelated (id INTEGER)")
	require.NoError(t, err)

	_, err = NewRepo(database).Purge(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deleting sessions")
	assert.Contains(t, err.Error(), "no such table")
}
