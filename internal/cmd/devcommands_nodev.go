//go:build !dev

package cmd

import "github.com/urfave/cli/v3"

// DevBuild reports whether this binary was built with the `dev` tag.
// See devcommands_dev.go.
const DevBuild = false

// devCommands returns nothing in a production build. See devcommands_dev.go.
func devCommands() []*cli.Command { return nil }
