//go:build !dev

package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Production builds must not ship dev-only commands. See devbuild_dev_test.go.
func TestDevBuildFalse(t *testing.T) {
	assert.False(t, DevBuild)
	for _, c := range NewCommand().Commands {
		assert.NotEqual(t, "db-init", c.Name, "production build must not include db-init")
	}
}
