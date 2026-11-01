// Package dialects registers every dialect implementation. Import it for its side effects:
//
//	import _ "github.com/jhgonline/fi-app-atlas/sqlglot/dialects"
//
// Individual dialects can also be imported on their own, e.g.
// github.com/jhgonline/fi-app-atlas/sqlglot/dialects/duckdb.
package dialects

import (
	_ "github.com/jhgonline/fi-app-atlas/sqlglot/dialects/duckdb"
	_ "github.com/jhgonline/fi-app-atlas/sqlglot/dialects/snowflake"
)
