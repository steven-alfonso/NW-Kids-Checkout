//go:build dev

package dbinit

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// White-box tests for the validate-before-destroy guard: Run validates the
// embedded fixture before any --force remove, so these prove the check itself
// rejects the corruptions that would otherwise destroy a good DB and then fail.
func TestValidateFixtureBytes_rejectsBroken(t *testing.T) {
	require.NoError(t, validateFixtureBytes(fixtureJSON))

	var f fixture
	require.NoError(t, json.Unmarshal(fixtureJSON, &f))
	require.NotEmpty(t, f.Events)

	dup := f
	dup.Events = append(dup.Events, f.Events[0])
	raw, err := json.Marshal(dup)
	require.NoError(t, err)
	err = validateFixtureBytes(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate event planning_center_id")

	empty := f
	empty.Locations = nil
	raw, err = json.Marshal(empty)
	require.NoError(t, err)
	err = validateFixtureBytes(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "locations")

	bad := f
	bad.Events[0].PlanningCenterID = f.Events[1].PlanningCenterID
	raw, err = json.Marshal(bad)
	require.NoError(t, err)
	require.Error(t, validateFixtureBytes(raw))
}
