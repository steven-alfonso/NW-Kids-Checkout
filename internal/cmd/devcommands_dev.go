//go:build dev

package cmd

import (
	"kids-checkin/internal/cmd/dbinit"

	"github.com/urfave/cli/v3"
)

// DevBuild reports whether this binary was built with the `dev` tag.
//
// It exists as a constant rather than something a test infers by looking for
// db-init in the command tree: inferring it that way makes a test skip its own
// check exactly when the command goes missing.
const DevBuild = true

// devCommands returns the commands that exist only in development builds.
//
// The `dev` build tag keeps dbinit -- and the reference topology it embeds --
// out of production binaries entirely. `make db-init` builds with -tags dev;
// the Dockerfile does not.
func devCommands() []*cli.Command { return dbinit.Commands() }
