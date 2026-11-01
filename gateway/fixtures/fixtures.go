// Package fixtures is the development gateway's data: the Harbour views its
// warehouse holds (harbour.json), and who may read what (policy.json). The
// same files are what the clients' end-to-end tests check results against,
// independently of the gateway (INTEGRATIONS, the acceptance result's
// sixth point).
package fixtures

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"atlas.local/gateway/fakes"
	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/protocol/grammar"
	"atlas.local/sdk/go/protocol/ijson"
)

//go:embed harbour.json
var harbour []byte

//go:embed policy.json
var policy []byte

//go:embed identities.json
var identities []byte

// HarbourJSON is the fixture as written.
func HarbourJSON() []byte { return harbour }

// Harbour is the fixture's tables, each value in its column's Go type.
func Harbour() ([]fakes.Table, error) { return parseHarbour(harbour) }

// maxHarbour is the most bytes a Harbour fixture may hold.
const maxHarbour = 16 << 20

// parseHarbour reads a Harbour fixture: its tables, each with named, typed
// columns and rows of values. A column is [name, type], or for a timestamp
// that holds wall-clock times [name, "timestamp", zone] (ports.Column.Zone).
// Every value is held to its column's type as the dev warehouse holds it:
// an integer within what a JSON number holds exactly, a date a real
// calendar date, a decimal its exact text, a timestamp an RFC 3339
// date-time with its offset. A duplicate member, table or column, an
// unknown member, type or zone, or a row of the wrong width is refused, so
// a fixture never loads as something other than it says.
func parseHarbour(b []byte) ([]fakes.Table, error) {
	if len(b) > maxHarbour {
		return nil, fmt.Errorf("fixtures: a fixture of %d bytes is more than %d", len(b), maxHarbour)
	}
	v, err := ijson.Parse(b)
	if err != nil {
		return nil, fmt.Errorf("fixtures: %w", err)
	}
	if err := members(v); err != nil {
		return nil, err
	}
	var doc struct {
		Description string `json:"description"`
		Tables      []struct {
			Name    string     `json:"name"`
			Columns [][]string `json:"columns"`
			Rows    [][]any    `json:"rows"`
		} `json:"tables"`
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("fixtures: %w", err)
	}
	var out []fakes.Table
	tables := map[string]bool{}
	for _, t := range doc.Tables {
		if t.Name == "" || tables[strings.ToUpper(t.Name)] {
			return nil, fmt.Errorf("fixtures: a table is named once: %q", t.Name)
		}
		tables[strings.ToUpper(t.Name)] = true
		table := fakes.Table{Name: t.Name}
		names := map[string]bool{}
		for _, c := range t.Columns {
			if len(c) != 2 && len(c) != 3 {
				return nil, fmt.Errorf("fixtures: %s: a column is [name, type] or [name, type, zone], not %q", t.Name, c)
			}
			typ := ports.ColumnType(c[1])
			switch typ {
			case ports.String, ports.Integer, ports.Float, ports.Decimal, ports.Boolean, ports.Date, ports.Timestamp:
			default:
				return nil, fmt.Errorf("fixtures: %s.%s has no type %q", t.Name, c[0], c[1])
			}
			if c[0] == "" || names[strings.ToUpper(c[0])] {
				return nil, fmt.Errorf("fixtures: %s names a column once: %q", t.Name, c[0])
			}
			names[strings.ToUpper(c[0])] = true
			col := ports.Column{Name: c[0], Type: typ}
			if len(c) == 3 {
				if _, err := time.LoadLocation(c[2]); err != nil || typ != ports.Timestamp || c[2] == "" || c[2] == "Local" {
					return nil, fmt.Errorf("fixtures: %s.%s: only a timestamp has a zone, an IANA name: %q", t.Name, c[0], c[2])
				}
				col.Zone = c[2]
			}
			table.Columns = append(table.Columns, col)
		}
		for r, row := range t.Rows {
			if len(row) != len(table.Columns) {
				return nil, fmt.Errorf("fixtures: %s row %d has %d values", t.Name, r, len(row))
			}
			typed := make([]any, len(row))
			for i, v := range row {
				x, err := typedValue(v, table.Columns[i].Type)
				if err != nil {
					return nil, fmt.Errorf("fixtures: %s row %d, %s: %w", t.Name, r, table.Columns[i].Name, err)
				}
				typed[i] = x
			}
			table.Rows = append(table.Rows, typed)
		}
		out = append(out, table)
	}
	return out, nil
}

// members refuses a member a fixture does not have, named exactly:
// encoding/json would read "Tables" as "tables".
func members(v ijson.Value) error {
	only := func(o *ijson.Object, names ...string) error {
		for _, n := range o.Names() {
			if !slices.Contains(names, n) {
				return fmt.Errorf("fixtures: %q is no member of a fixture", n)
			}
		}
		return nil
	}
	doc, ok := v.(*ijson.Object)
	if !ok {
		return errors.New("fixtures: a fixture is a JSON object")
	}
	if err := only(doc, "description", "tables"); err != nil {
		return err
	}
	tables, _ := doc.Get("tables")
	list, _ := tables.([]ijson.Value)
	for _, t := range list {
		if o, ok := t.(*ijson.Object); ok {
			if err := only(o, "name", "columns", "rows"); err != nil {
				return err
			}
		}
	}
	return nil
}

func typedValue(v any, t ports.ColumnType) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch t {
	case ports.Integer:
		f, ok := v.(float64)
		if !ok || f != math.Trunc(f) || math.Abs(f) > ijson.MaxSafeInteger {
			return nil, fmt.Errorf("%v is not an integer a JSON number holds exactly", v)
		}
		return int64(f), nil
	case ports.Float:
		f, ok := v.(float64)
		if !ok {
			return nil, fmt.Errorf("%v is not a number", v)
		}
		return f, nil
	case ports.Timestamp:
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%v is not a date-time", v)
		}
		ts, err := grammar.ParseDateTime(s)
		if err != nil {
			return nil, err
		}
		return ts.UTC(), nil
	case ports.Boolean:
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("%v is not a boolean", v)
		}
		return b, nil
	}
	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("%v is not a string", v)
	}
	switch t {
	case ports.Date:
		if _, err := grammar.ParseDate(s); err != nil {
			return nil, err
		}
	case ports.Decimal:
		if _, err := grammar.ParseDecimal(s); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Policy is who may do what in the development gateway, read as
// production reads its policy file (R134), so a member the reader does not
// know fails here as it would there.
func Policy() (*ports.Policy, error) { return parsePolicy(policy) }

func parsePolicy(b []byte) (*ports.Policy, error) { return ports.ParsePolicy(b) }

// PolicyJSON is the policy as written.
func PolicyJSON() []byte { return policy }

// Identity is a development identity and the token that proves it.
type Identity struct {
	Token    string `json:"token"`
	Identity ports.Identity
}

// Identities are the development gateway's people and workloads.
func Identities() ([]Identity, error) {
	var doc struct {
		Identities []struct {
			Token     string   `json:"token"`
			Principal string   `json:"principal"`
			Name      string   `json:"name"`
			Kind      string   `json:"kind"`
			Groups    []string `json:"groups"`
		} `json:"identities"`
	}
	if err := json.Unmarshal(identities, &doc); err != nil {
		return nil, err
	}
	out := make([]Identity, len(doc.Identities))
	for i, d := range doc.Identities {
		out[i] = Identity{Token: d.Token, Identity: ports.Identity{Principal: d.Principal, Name: d.Name, Kind: d.Kind, Groups: d.Groups, Tenant: "dev", Application: "atlas-dev"}}
	}
	return out, nil
}
