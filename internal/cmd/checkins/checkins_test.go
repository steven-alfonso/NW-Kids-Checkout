package checkins

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"kids-checkin/internal/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

// Test_deleteOlderThanCmd_dbInitFailureWraps pins that an InitDB failure is
// returned wrapped, not stringified into cli.Exit. Stringifying drops the
// chain so errors.Is/As cannot see the underlying cause.
func Test_deleteOlderThanCmd_dbInitFailureWraps(t *testing.T) {
	// A parent that is a file: MkdirAll fails with "not a directory".
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))

	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.DurationFlag{Name: "age", Value: -7 * 24 * time.Hour},
			db.DBFileFlag(),
		},
	}
	require.NoError(t, cmd.Set("db-file", filepath.Join(blocker, "k.db")))

	err := deleteOlderThanCmd(t.Context(), cmd)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "init db")
	assert.NotNil(t, errors.Unwrap(err), "must wrap the cause with %%w, not stringify it into cli.Exit")
}
