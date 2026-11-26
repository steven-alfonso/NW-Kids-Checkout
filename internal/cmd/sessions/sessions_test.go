package sessions

import (
	"path/filepath"
	"testing"

	"github.com/gofiber/storage/sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

func Test_purgeCmd(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "kids-checkin.db")

	store := sqlite3.New(sqlite3.Config{Database: dbFile})
	require.NoError(t, store.Set("session-1", []byte("signed-session"), 0))
	require.NoError(t, store.Close())

	cmd := &cli.Command{Flags: Commands[0].Flags}
	require.NoError(t, cmd.Set("db-file", dbFile))

	t.Run("refuses without force", func(t *testing.T) {
		err := purgeCmd(t.Context(), cmd)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--force")

		// The session must survive the refusal.
		store := sqlite3.New(sqlite3.Config{Database: dbFile})
		defer store.Close()

		got, err := store.Get("session-1")
		require.NoError(t, err)
		assert.Equal(t, []byte("signed-session"), got)
	})

	t.Run("deletes every session with force", func(t *testing.T) {
		require.NoError(t, cmd.Set("force", "true"))

		require.NoError(t, purgeCmd(t.Context(), cmd))

		store := sqlite3.New(sqlite3.Config{Database: dbFile})
		defer store.Close()

		got, err := store.Get("session-1")
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}
