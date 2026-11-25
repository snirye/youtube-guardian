// Package migrations embeds the SQLite schema changes in application order.
package migrations

import _ "embed"

//go:embed 001_foundation.sql
var foundation string

func Ordered() []string {
	return []string{foundation}
}
