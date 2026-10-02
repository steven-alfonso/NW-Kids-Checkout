//go:build dev

package dbinit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A fixture row naming a group that no group defines must fail loudly. Silently
// leaving the row ungrouped is the failure this guards: the room would still be
// created, just quietly in the wrong group, and nothing downstream would notice.
//
// No row in the shipped fixture triggers this, so it is only reachable by
// calling the resolver directly.
func TestResolveGroup(t *testing.T) {
	groups := map[string]int64{"Kids": 1, "Nursery": 3}

	t.Run("absent reference stays nil", func(t *testing.T) {
		got, err := resolveGroup(groups, nil)
		require.NoError(t, err)
		assert.Nil(t, got, "an omitted group must stay NULL, not default to some group")
	})

	t.Run("known name resolves to its id", func(t *testing.T) {
		name := "Nursery"
		got, err := resolveGroup(groups, &name)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, int64(3), *got)
	})

	t.Run("unknown name is an error", func(t *testing.T) {
		name := "Kids Jr"
		got, err := resolveGroup(groups, &name)
		require.Error(t, err)
		assert.Nil(t, got)
		assert.Contains(t, err.Error(), `unknown location group "Kids Jr"`)
	})
}
