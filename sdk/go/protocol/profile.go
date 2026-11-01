package protocol

import "encoding/json"

// Families of capability (§2.1.B).
const (
	FamilyData     = "data"
	FamilyResource = "resource"
	FamilyAction   = "action"
	FamilyEvent    = "event"
)

// Traits the skeleton understands (§2.1.E).
const (
	TraitProjectable = "Projectable"
	TraitFilterable  = "Filterable"
	TraitAsOf        = "AsOf"
	TraitKeyed       = "Keyed"
	TraitHandoff     = "Handoff"
	TraitLicensed    = "Licensed"
)

// Profile is everything a capability says about itself (§3.1). In atlas/0.1
// it holds only what the skeleton needs; §9.3's profile document comes later.
type Profile struct {
	ID           string                 `json:"id"`
	Version      int                    `json:"version"`
	Title        string                 `json:"title,omitempty"`
	Summary      string                 `json:"summary,omitempty"`
	Domain       string                 `json:"domain"`
	Owner        string                 `json:"owner,omitempty"`
	Family       string                 `json:"family"`
	Stability    string                 `json:"stability"`
	Lifecycle    string                 `json:"lifecycle"`
	Visibility   string                 `json:"visibility"`
	Traits       []string               `json:"traits,omitempty"`
	Interactions map[string]Interaction `json:"interactions"`
	Request      *Schema                `json:"request,omitempty"`
	Result       *Table                 `json:"result,omitempty"`
	Meaning      map[string]string      `json:"meaning,omitempty"`
	Evolution    *Evolution             `json:"evolution,omitempty"`
	// Revision counts changes to prose, which never change the fingerprint.
	Revision int `json:"revision,omitempty"`
}

// Pinned returns the profile's id@vN.
func (p *Profile) Pinned() Pinned { return Pinned{ID: p.ID, Version: p.Version} }

// HasTrait reports whether the capability implements the trait.
func (p *Profile) HasTrait(t string) bool { return contains(p.Traits, t) }

// Previous returns the exact id of the profile this one replaces within its
// version, or "" for the first.
func (p *Profile) Previous() string {
	if p.Evolution == nil {
		return ""
	}
	return p.Evolution.Previous
}

// Interaction is one verb a capability offers (§2.1.D).
type Interaction struct {
	Effect     string    `json:"effect"`
	Result     string    `json:"result"`
	Delivery   string    `json:"delivery"`
	Executions []string  `json:"execution,omitempty"`
	Bindings   []Binding `json:"bindings"`
}

// Offers reports whether the interaction offers the execution.
func (i Interaction) Offers(execution string) bool { return contains(i.Executions, execution) }

// Binding is where and how one interaction is served (§4.2). Clients follow
// bindings; they never build an address.
type Binding struct {
	Protocol  string   `json:"protocol"`
	Server    string   `json:"server"`
	Method    string   `json:"method"`
	Address   string   `json:"address"`
	Encodings []string `json:"encodings,omitempty"`
}

// Table is a table result's schema: its columns, in order.
type Table struct {
	Columns []Column `json:"columns"`
}

// Index returns the position of the column, or -1.
func (t *Table) Index(name string) int {
	for i, c := range t.Columns {
		if c.Name == name {
			return i
		}
	}
	return -1
}

// Column returns the named column.
func (t *Table) Column(name string) (Column, bool) {
	if i := t.Index(name); i >= 0 {
		return t.Columns[i], true
	}
	return Column{}, false
}

// Lookup finds the column a name refers to, by its name or one of its
// aliases, as a request written under an earlier profile may use (§3.3.4).
func (t *Table) Lookup(name string) (Column, bool) {
	if c, ok := t.Column(name); ok {
		return c, true
	}
	for _, c := range t.Columns {
		if contains(c.Aliases, name) {
			return c, true
		}
	}
	return Column{}, false
}

// Project keeps only the named columns, in the table's own order.
func (t *Table) Project(names []string) *Table {
	if len(names) == 0 {
		return t
	}
	out := &Table{}
	for _, c := range t.Columns {
		if contains(names, c.Name) {
			out.Columns = append(out.Columns, c)
		}
	}
	return out
}

// Column types of a table result. Their Arrow mapping is ArrowType.
const (
	TypeString   = "string"
	TypeInteger  = "integer"
	TypeNumber   = "number"
	TypeBoolean  = "boolean"
	TypeDate     = "date"
	TypeDateTime = "date-time"
	TypeDecimal  = "decimal"
)

// Unknown is the value a reader maps any enum value it does not know to
// (§3.2.4). A request's enum never includes it.
const Unknown = "UNKNOWN"

// Column is one column of a table result, with its meaning carried in the
// schema (§3.2).
type Column struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Precision   int      `json:"precision,omitempty"`
	Scale       int      `json:"scale,omitempty"`
	Nullable    bool     `json:"nullable,omitempty"`
	Key         bool     `json:"key,omitempty"`
	Unit        string   `json:"unit,omitempty"`
	Logical     string   `json:"logical,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Aliases     []string `json:"aliases,omitempty"`
	Description string   `json:"description,omitempty"`
}

// Evolution records the profile this one replaces within its version and the
// lens from it (§3.3).
type Evolution struct {
	Previous  string   `json:"previous,omitempty"`
	Lens      []LensOp `json:"lens,omitempty"`
	Changelog string   `json:"changelog,omitempty"`
}

// Lens operations (§9.5.3).
const (
	LensRename      = "rename"
	LensAliasValue  = "alias_value"
	LensConvertUnit = "convert_unit"
	LensPromote     = "promote"
	LensDefault     = "default"
	LensDrop        = "drop"
)

// Lens targets. A lens operation names a field of one schema of the profile.
const (
	SchemaResult  = "result"
	SchemaRequest = "request"
)

// LensOp is one closed-form operation of a lens, describing the change from
// the previous profile to this one.
type LensOp struct {
	Op string `json:"op"`
	// Schema is the schema whose records the operation applies to: result
	// (the default) or request.
	Schema string `json:"schema,omitempty"`
	// Field is the field a value operation, default or drop applies to.
	Field string `json:"field,omitempty"`
	// From and To are field names for rename, values for alias_value, units
	// for convert_unit and types for promote.
	From  string          `json:"from,omitempty"`
	To    string          `json:"to,omitempty"`
	Value json.RawMessage `json:"value,omitempty"`
}

// AppliesTo reports whether the operation applies to the schema.
func (op LensOp) AppliesTo(schema string) bool {
	s := op.Schema
	if s == "" {
		s = SchemaResult
	}
	return s == schema
}
