package ports

import "errors"

// Query is a read of one object, as a capability's view model describes it:
// no SQL text. It is the request side of DataSource.Plan: the source
// compiles it for its engine, checks the statement it built (the read-only
// guard, §6.4.3) and binds every value as a parameter, so a request's
// values never become SQL.
type Query struct {
	// Object is database.schema.object, as the warehouse names it.
	Object string
	// Columns are what the read returns, in order.
	Columns []Column
	// Where are conditions every returned row meets, ANDed.
	Where []Predicate
	// Latest, when set, keeps only each group's latest date, on or before
	// a bound, chosen before Where narrows the rows: a portfolio's last
	// valuation on or before as_of.
	Latest *Latest
	// OrderBy are columns, ascending.
	OrderBy []string
	// Limit is the most rows to return; 0 is no limit.
	Limit int
	// Tag names the capability the read serves, for the warehouse's own
	// records (Snowflake's QUERY_TAG); it is never part of the statement.
	Tag string
}

// Column is one column of the object.
type Column struct {
	// Name is the column as the object names it.
	Name string
	// Type is what the adapter returns its values as.
	Type ColumnType
	// Precision and Scale, when Precision is set, are a decimal column's
	// type, Decimal(Precision, Scale): a read casts the column to it, so
	// the source rounds a value to the scale (R100), and a fixture is
	// loaded at it, so nothing is rounded on the way in (R117).
	Precision, Scale int
	// Zone is the zone a timestamp column's wall-clock times are in, when
	// the source holds wall-clock times (Snowflake's TIMESTAMP_NTZ): an
	// IANA name such as "Europe/London", or "UTC". A read converts them to
	// UTC instants in its statement. Empty means the column holds instants
	// (TIMESTAMP_TZ, TIMESTAMP_LTZ); a wall-clock time without a zone is
	// refused, never assumed to be UTC (§2.2.1.6, R053).
	Zone string
}

// ColumnType is how a column's values are returned.
type ColumnType string

// The column types, and the Go type each value is returned as. A null is
// nil in every type.
const (
	String    ColumnType = "string"    // string
	Integer   ColumnType = "integer"   // int64
	Float     ColumnType = "float"     // float64
	Decimal   ColumnType = "decimal"   // string: the exact decimal, never a float
	Boolean   ColumnType = "boolean"   // bool
	Date      ColumnType = "date"      // string: YYYY-MM-DD
	Timestamp ColumnType = "timestamp" // time.Time, in UTC
)

// Op is a predicate's comparison.
type Op string

// The comparisons.
const (
	In  Op = "in"  // Column is one of Values
	Eq  Op = "eq"  // Column equals Values[0]
	Gte Op = "gte" // Column ≥ Values[0]
	Lte Op = "lte" // Column ≤ Values[0]
)

// Predicate is one condition. Type is the column's type, which the
// values are written in: a date as YYYY-MM-DD, a decimal as its string, a
// timestamp as an RFC 3339 date-time with its offset or a time.Time, which
// is compared as an instant (R111). Zone is a timestamp column's zone, as
// Column.Zone says.
type Predicate struct {
	Column string
	Op     Op
	Type   ColumnType
	Values []any
	Zone   string
}

// Latest keeps each group's latest Date: the most recent on or before
// OnOrBefore (a YYYY-MM-DD date), or the most recent of all when it is "".
type Latest struct {
	Date       string
	Per        []string
	OnOrBefore string
	// Within are conditions that choose the group's rows before the
	// latest date is found; the query's Where applies afterwards.
	Within []Predicate
}

// ErrUnknownObject is a query of an object the warehouse adapter does not
// serve: no capability may read past what its deployment registered.
var ErrUnknownObject = errors.New("ports: the warehouse serves no such object")

// ErrQueryTooLarge is a read with more values than one statement may bind
// (R112): an IN of a request's values or of a caller's scope past the
// source's bound. The error names the column. It is never retried; a
// request that caused it is narrowed by its caller.
var ErrQueryTooLarge = errors.New("ports: the read has more values than one statement may bind")

// ErrUnknownQuery is a query id the source holds no record of: never one it
// ran, or one whose record it no longer keeps.
var ErrUnknownQuery = errors.New("ports: the source has no record of the query")

// ErrWarehouseUnavailable is a warehouse that cannot answer now: suspended,
// unreachable, or out of capacity.
var ErrWarehouseUnavailable = errors.New("ports: the warehouse is unavailable")
