//go:build dev

package dbinit

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every fixture section is required. An empty one parses cleanly and seeds
// nothing, which would report "db-init: complete" with zero rooms.
//
// These tests exist because this guard was previously inline in applyFixture,
// where it could only be reached through a mutated fixture.json -- so nothing
// verified it. Neutering the check with `&& false` compiled cleanly and left
// every test in this package green.
func TestValidateFixture(t *testing.T) {
	t.Run("the shipped fixture is valid", func(t *testing.T) {
		var f fixture
		require.NoError(t, json.Unmarshal(fixtureJSON, &f))
		require.NoError(t, validateFixture(f),
			"the checked-in fixture must pass its own validation")
	})

	// Each section is dropped in turn. Named subtests so a failure points at the
	// section rather than at "the fixture".
	t.Run("refuses a fixture missing each section", func(t *testing.T) {
		for _, tc := range []struct {
			section string
			empty   func(*fixture)
		}{
			{"location_groups", func(f *fixture) { f.LocationGroups = nil }},
			{"events", func(f *fixture) { f.Events = nil }},
			{"locations", func(f *fixture) { f.Locations = nil }},
			{"event_check_windows", func(f *fixture) { f.EventCheckWindows = nil }},
		} {
			t.Run(tc.section, func(t *testing.T) {
				var f fixture
				require.NoError(t, json.Unmarshal(fixtureJSON, &f))
				tc.empty(&f)

				err := validateFixture(f)
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.section)
				assert.Contains(t, err.Error(), "empty reference topology")
			})
		}
	})

	// An empty slice and a nil slice both parse to len 0, so a truncated file and
	// a hand-edit that empties the array must fail the same way.
	t.Run("an empty slice is as invalid as a missing one", func(t *testing.T) {
		var f fixture
		require.NoError(t, json.Unmarshal(fixtureJSON, &f))
		f.Locations = []struct {
			Name                   string  `json:"name"`
			PlanningCenterID       string  `json:"planning_center_id"`
			PlanningCenterParentID *string `json:"planning_center_parent_id"`
			LocationGroup          *string `json:"location_group"`
			Event                  string  `json:"event"`
		}{}

		require.Error(t, validateFixture(f))
	})
}

// The shipped fixture must parse, or the sections above are all empty and every
// case would "pass" for the wrong reason.
func TestFixtureJSONParses(t *testing.T) {
	var f fixture
	require.NoError(t, json.Unmarshal(fixtureJSON, &f),
		"fixture.json must parse; if it does not, the other tests in this file are vacuous")
	assert.NotEmpty(t, f.Locations)
	assert.NotEmpty(t, f.Events)
}
