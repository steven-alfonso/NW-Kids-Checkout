//go:build dev

package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// DevBuild exists so tests do not infer dev-ness by looking for db-init in
// the command tree (which would skip its own check exactly when the command
// goes missing). This test consumes it in the dev build.
func TestDevBuildTrue(t *testing.T) {
	assert.True(t, DevBuild)
	names := map[string]bool{}
	for _, c := range NewCommand().Commands {
		names[c.Name] = true
	}
	assert.True(t, names["db-init"], "dev build must include db-init")
}
