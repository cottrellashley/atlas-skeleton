package snowflake

import "strings"

// WallClock reports a database type name that holds wall-clock time with no
// zone: Snowflake's TIMESTAMP_NTZ, DuckDB's TIMESTAMP.
func WallClock(typeName string) bool {
	switch strings.ToUpper(typeName) {
	case "TIMESTAMP_NTZ", "TIMESTAMPNTZ", "TIMESTAMP", "DATETIME", "TIMESTAMP WITHOUT TIME ZONE":
		return true
	}
	return false
}
