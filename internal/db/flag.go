package db

import "github.com/urfave/cli/v3"

// EnvDBFile is the environment variable that overrides DefaultDBFile.
const EnvDBFile = "DB_FILE"

// DBFileFlag returns the --db-file flag, the single definition of it in the
// codebase. Every command that opens the database mounts this rather than
// spelling out its own flag, because a per-command copy is what let the
// defaults drift from the Makefile in the first place.
//
// Precedence is --db-file, then $DB_FILE, then DefaultDBFile.
func DBFileFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:    "db-file",
		Usage:   "path to the sqlite database file",
		Value:   DefaultDBFile,
		Sources: cli.NewValueSourceChain(cli.EnvVar(EnvDBFile)),
	}
}
