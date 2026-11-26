package sessions

import (
	"database/sql"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"kids-checkin/internal/db"

	"github.com/gofiber/storage/sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

// newPurgeCommand builds fresh flags so cmd.Set cannot mutate Commands[0].
func newPurgeCommand(t *testing.T, dbFile string, force bool) *cli.Command {
	t.Helper()

	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "force",
				Usage: "Required to confirm deleting every session",
			},
			db.DBFileFlag(),
		},
	}
	require.NoError(t, cmd.Set("db-file", dbFile))
	require.NoError(t, cmd.Set("force", strconv.FormatBool(force)))

	return cmd
}

func countSessions(t *testing.T, dbFile string) int {
	t.Helper()

	database, err := sql.Open("sqlite3", "file:"+dbFile+"?mode=rw")
	require.NoError(t, err)
	defer database.Close()

	var count int
	require.NoError(t, database.QueryRow("SELECT COUNT(*) FROM fiber_storage").Scan(&count))

	return count
}

func Test_purgeCmd_refusesWithoutForce(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "kids-checkin.db")

	store := sqlite3.New(sqlite3.Config{Database: dbFile})
	require.NoError(t, store.Set("session-1", []byte("signed-session"), 24*time.Hour))
	require.NoError(t, store.Close())

	err := purgeCmd(t.Context(), newPurgeCommand(t, dbFile, false))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--force")

	// The refusal leaves the session in place.
	assert.Equal(t, 1, countSessions(t, dbFile))
}

func Test_purgeCmd_deletesEverySession(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "kids-checkin.db")

	store := sqlite3.New(sqlite3.Config{Database: dbFile})
	require.NoError(t, store.Set("session-1", []byte("signed-session"), 24*time.Hour))
	require.NoError(t, store.Close())

	require.NoError(t, purgeCmd(t.Context(), newPurgeCommand(t, dbFile, true)))

	// The table still exists; only its rows are gone.
	assert.Equal(t, 0, countSessions(t, dbFile))

	store = sqlite3.New(sqlite3.Config{Database: dbFile})
	defer store.Close()

	got, err := store.Get("session-1")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func Test_purgeCmd_refusesMissingDatabase(t *testing.T) {
	t.Run("empty db file", func(t *testing.T) {
		err := purgeCmd(t.Context(), newPurgeCommand(t, "", true))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing database DSN")
	})

	t.Run("missing file", func(t *testing.T) {
		dbFile := filepath.Join(t.TempDir(), "nope.db")

		err := purgeCmd(t.Context(), newPurgeCommand(t, dbFile, true))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not exist")
		require.NoFileExists(t, dbFile, "a failed purge must not create the database file")
	})

	t.Run("missing directory", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "nope")
		dbFile := filepath.Join(dir, "kids-checkin.db")

		err := purgeCmd(t.Context(), newPurgeCommand(t, dbFile, true))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not exist")
		require.NoDirExists(t, dir, "a failed purge must not create the database directory")
		require.NoFileExists(t, dbFile)
	})
}
