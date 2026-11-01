package views

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"reflect"
	"slices"
	"strings"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/profile"
)

// Model is one capability's view model: where its answers are. It serves
// the interaction it names (fetch) and, where the profile offers explain,
// explain too, which plans the same read and runs nothing (ATLAS-DATA-004,
// ATLAS-DATA-007).
type Model struct {
	// Interaction is the interaction it serves (fetch).
	Interaction string `json:"interaction"`
	// Object is the object answered from: database.schema.object.
	Object string `json:"object"`
	// Columns give each result property its source.
	Columns map[string]Source `json:"columns"`
	// Filters map request members to conditions.
	Filters map[string]Filter `json:"filters"`
	// Latest chooses each group's latest date on or before a request date.
	Latest *Latest `json:"latest,omitempty"`
	// Scope gives each data dimension the column that holds it: a caller
	// sees only rows whose value is in their scope.
	Scope map[string]string `json:"scope"`
	// Order is the columns rows are ordered by. It begins with the key's
	// columns, so rows that share a key are next to each other.
	Order []string `json:"order"`
	// Search ranks the rows by words instead of returning them all.
	Search *Search `json:"search,omitempty"`
	// Exists refuses a request naming values the reference object does not
	// hold, with the capability's declared error.
	Exists *Exists `json:"exists,omitempty"`
	// Valued refuses a request naming values with no valuation on or before
	// the request's date, with the capability's declared error.
	Valued *Valued `json:"valued,omitempty"`
	// Freshness writes a status as the call finds it, from the dates a row
	// holds and how recently they were checked.
	Freshness *Freshness `json:"freshness,omitempty"`
	// Unavailable is the declared error for a source that cannot answer.
	Unavailable string `json:"unavailable,omitempty"`
}

// Source is where a property's value is.
type Source struct {
	Column string           `json:"column,omitempty"`
	Type   ports.ColumnType `json:"type,omitempty"`
	// Zone is a timestamp column's zone when it holds wall-clock times
	// (ports.Column.Zone): "UTC", or an IANA name. A read converts them to
	// instants; a wall-clock time with no zone is refused, never assumed
	// to be UTC (§2.2.1.6).
	Zone string `json:"zone,omitempty"`
	// Values map the column's values to the property's enum values.
	Values map[string]string `json:"values,omitempty"`
	// Money is the two columns of a Money property.
	Money *Money `json:"money,omitempty"`
}

// Money is the amount and currency columns of a Money property.
type Money struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// Filter is the condition a request member makes, when it is not null.
type Filter struct {
	Column string           `json:"column"`
	Op     ports.Op         `json:"op"`
	Type   ports.ColumnType `json:"type"`
	Zone   string           `json:"zone,omitempty"` // as Source.Zone
	// Values map the member's enum values to the column's.
	Values map[string]string `json:"values,omitempty"`
}

// Latest keeps each group's latest date on or before the request's date.
type Latest struct {
	Request string   `json:"request"` // the request member holding the date
	Column  string   `json:"column"`  // the date column
	Per     []string `json:"per"`     // the group's columns
	Within  []string `json:"within"`  // the filters that choose the group's rows
}

// Search ranks rows by the request's words, matched in their code and name.
type Search struct {
	Words string `json:"words"` // the request member holding the words
	Limit string `json:"limit"` // the request member holding the most answers
	Code  string `json:"code"`  // the property matched exactly first
	Name  string `json:"name"`  // the property matched word by word
	Score string `json:"score"` // the property the score is written to
	Why   string `json:"why"`   // the property the reason is written to
}

// Exists names the values a request member must find in a reference object.
type Exists struct {
	Request string `json:"request"`
	Object  string `json:"object"`
	Column  string `json:"column"`
	Error   string `json:"error"`
}

// Valued names the values a request member must find valued, on or before
// the date another member holds (every date when it is null), in a register
// of valuations: a portfolio that was not yet valued then is refused with
// the declared error, never answered as no rows (R037).
type Valued struct {
	Request string `json:"request"` // the member holding the values (portfolios)
	AsOf    string `json:"as_of"`   // the member holding the date (as_of)
	Object  string `json:"object"`  // the register of valuations
	Column  string `json:"column"`  // its column holding the values
	Date    string `json:"date"`    // its column holding the valuation date
	Error   string `json:"error"`
}

// Freshness writes a status property as the call finds it (R035): current
// when the latest date is on or after the expected date, late when it is
// before, and unchecked when the dates were last checked longer ago than
// Within, so that a register that stopped checking is never read as
// current. Latest, Expected and Checked are properties of the row.
type Freshness struct {
	Status   string          `json:"status"`
	Latest   string          `json:"latest"`
	Expected string          `json:"expected"`
	Checked  string          `json:"checked"`
	Within   string          `json:"within"` // an ISO 8601 duration
	Values   FreshnessValues `json:"values"`
}

// FreshnessValues are the status property's enum values for each state.
type FreshnessValues struct {
	Current   string `json:"current"`
	Late      string `json:"late"`
	Unchecked string `json:"unchecked"`
}

// MaxModel is the most bytes a view model may hold.
const MaxModel = 1 << 20

// ParseModel reads a view model: one JSON object, refusing duplicate and
// unknown members, a member named in another case than its own, and
// anything after it.
func ParseModel(b []byte) (Model, error) {
	if len(b) > MaxModel {
		return Model{}, fmt.Errorf("views: a view model of %d bytes is more than %d", len(b), MaxModel)
	}
	v, err := ijson.Parse(b)
	if err != nil {
		return Model{}, fmt.Errorf("views: %w", err)
	}
	if err := exact(v, reflect.TypeFor[Model](), ""); err != nil {
		return Model{}, fmt.Errorf("views: %w", err)
	}
	var m Model
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Model{}, fmt.Errorf("views: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Model{}, errors.New("views: a view model is one JSON object, with nothing after it")
	}
	return m, nil
}

// exact refuses a member of v that t does not name exactly: encoding/json
// matches names in any case, which would read "Object" as "object".
func exact(v ijson.Value, t reflect.Type, at string) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		o, ok := v.(*ijson.Object)
		if !ok {
			return nil // a type mismatch is the decoder's to report
		}
		for _, name := range o.Names() {
			f, ok := memberOf(t, name)
			if !ok {
				return fmt.Errorf("%s/%s is no member of a view model", at, name)
			}
			member, _ := o.Get(name)
			if err := exact(member, f.Type, at+"/"+name); err != nil {
				return err
			}
		}
	case reflect.Map:
		if o, ok := v.(*ijson.Object); ok {
			for _, name := range o.Names() {
				member, _ := o.Get(name)
				if err := exact(member, t.Elem(), at+"/"+name); err != nil {
					return err
				}
			}
		}
	case reflect.Slice:
		if list, ok := v.([]ijson.Value); ok {
			for i, e := range list {
				if err := exact(e, t.Elem(), fmt.Sprintf("%s/%d", at, i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func memberOf(t reflect.Type, name string) (reflect.StructField, bool) {
	for i := range t.NumField() {
		f := t.Field(i)
		if tag, _, _ := strings.Cut(f.Tag.Get("json"), ","); tag == name {
			return f, true
		}
	}
	return reflect.StructField{}, false
}

// Binding is a profile and the view model of one of its interactions.
type Binding struct {
	Profile *profile.Profile
	Model   Model
}

// Files are one capability's files: its profile, its view model, and the
// profiles it replaced within its version.
type Files struct {
	Name    string // id@vN
	Profile []byte
	Model   []byte
	// Previous are the profiles the served one replaced within its version,
	// oldest first: what a request written under an earlier profile is
	// resolved forward from (§3.3, BACKWARD_TRANSITIVE), and what the gate
	// classifies the served one against.
	Previous [][]byte
}

// MaxHistory is the most earlier profiles one capability's history holds.
const MaxHistory = 64

// Read pairs every profiles/<id>@vN.json in fsys with its
// views/<id>@vN.json, and with the earlier profiles of history/<id>@vN/: a
// domain's capabilities, as the files its owner writes. Each earlier
// profile is linked to the next by the next one's evolution's
// previous_profile; one the chain does not reach is refused, and a chain
// that ends before its first profile (a directory with no history) is what
// the gateway serves from.
func Read(fsys fs.FS) ([]Files, error) {
	names, err := fs.Glob(fsys, "profiles/*.json")
	if err != nil {
		return nil, fmt.Errorf("views: %w", err)
	}
	var out []Files
	for _, n := range names {
		base := path.Base(n)
		name := strings.TrimSuffix(base, ".json")
		p, err := fs.ReadFile(fsys, n)
		if err != nil {
			return nil, fmt.Errorf("views: %w", err)
		}
		m, err := fs.ReadFile(fsys, "views/"+base)
		if err != nil {
			return nil, fmt.Errorf("views: %s has no view model: %w", base, err)
		}
		prev, err := history(fsys, name, p)
		if err != nil {
			return nil, err
		}
		out = append(out, Files{Name: name, Profile: p, Model: m, Previous: prev})
	}
	return out, nil
}

// history reads history/<name>/ and orders it by the chain the served
// profile begins.
func history(fsys fs.FS, name string, current []byte) ([][]byte, error) {
	dir := "history/" + name
	entries, err := fs.ReadDir(fsys, dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("views: %s: %w", dir, err)
	}
	byExact := map[string][]byte{}
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".json" {
			continue
		}
		if len(byExact) == MaxHistory {
			return nil, fmt.Errorf("views: %s holds more than %d profiles", dir, MaxHistory)
		}
		b, err := fs.ReadFile(fsys, dir+"/"+e.Name())
		if err != nil {
			return nil, fmt.Errorf("views: %w", err)
		}
		exact, _, err := identify(b)
		if err != nil {
			return nil, fmt.Errorf("views: %s/%s: %w", dir, e.Name(), err)
		}
		byExact[exact] = b
	}
	_, at, err := identify(current)
	if err != nil {
		return nil, fmt.Errorf("views: %s: %w", name, err)
	}
	var chain [][]byte
	for at != "" {
		b, ok := byExact[at]
		if !ok {
			break
		}
		delete(byExact, at)
		chain = append(chain, b)
		if _, at, err = identify(b); err != nil {
			return nil, err
		}
	}
	if len(byExact) > 0 {
		left := make([]string, 0, len(byExact))
		for exact := range byExact {
			left = append(left, exact)
		}
		slices.Sort(left)
		return nil, fmt.Errorf("views: %s holds %s, which %s's chain of earlier profiles does not reach", dir, strings.Join(left, ", "), name)
	}
	slices.Reverse(chain)
	return chain, nil
}

// identify is a profile document's exact id, as the gateway serves it, and
// the exact id of the profile it names as its previous one, or "".
func identify(doc []byte) (exact, previous string, err error) {
	v, err := ijson.Parse(doc)
	if err != nil {
		return "", "", err
	}
	o, ok := v.(*ijson.Object)
	if !ok {
		return "", "", errors.New("a profile is a JSON object")
	}
	o.Delete("fingerprint")
	p, err := profile.Parse(o)
	if err != nil {
		return "", "", err
	}
	if pp := p.Evolution.PreviousProfile; pp != nil {
		previous = pp.String()
	}
	return p.Exact().String(), previous, nil
}
