package db

import (
	_ "embed"
)

//go:embed structure.sql
var Schema string
