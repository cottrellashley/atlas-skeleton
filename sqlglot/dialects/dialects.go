// Package dialects registers every dialect implementation. Import it for its side effects:
//
//	import _ "atlas.local/sqlglot/dialects"
//
// Individual dialects can also be imported on their own, e.g.
// atlas.local/sqlglot/dialects/duckdb.
package dialects

import (
	_ "atlas.local/sqlglot/dialects/duckdb"
	_ "atlas.local/sqlglot/dialects/snowflake"
)
