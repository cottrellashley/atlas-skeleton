package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Schema is a request schema, in the subset of JSON Schema 2020-12 that
// atlas/0.1 uses (§9.2.1 fixes the full Atlas Schema profile later). Request
// schemas are closed: additionalProperties is false on every object.
type Schema struct {
	Type                 Type               `json:"type"`
	Description          string             `json:"description,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	Required             []string           `json:"required,omitempty"`
	AdditionalProperties *bool              `json:"additionalProperties,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	Enum                 []string           `json:"enum,omitempty"`
	Format               string             `json:"format,omitempty"`
	MinItems             *int               `json:"minItems,omitempty"`
	MaxItems             *int               `json:"maxItems,omitempty"`
	Minimum              *float64           `json:"minimum,omitempty"`
	Maximum              *float64           `json:"maximum,omitempty"`
	Default              json.RawMessage    `json:"default,omitempty"`
	Reference            string             `json:"x-atlas-reference,omitempty"`
	Aliases              []string           `json:"x-atlas-aliases,omitempty"`
}

// Type is a JSON Schema type: one name, or a pair whose second member is null
// (§9.2.1, rule 4).
type Type struct {
	Name     string
	Nullable bool
}

func (t Type) MarshalJSON() ([]byte, error) {
	if t.Nullable {
		return json.Marshal([]string{t.Name, "null"})
	}
	return json.Marshal(t.Name)
}

func (t *Type) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*t = Type{Name: one}
		return nil
	}
	var pair []string
	if err := json.Unmarshal(b, &pair); err != nil || len(pair) != 2 || pair[1] != "null" {
		return fmt.Errorf(`a type must be a name or [name, "null"]`)
	}
	*t = Type{Name: pair[0], Nullable: true}
	return nil
}

// Failure is one way an instance does not match its schema (§9.2.5).
type Failure struct {
	Pointer string `json:"pointer"` // RFC 6901
	Keyword string `json:"keyword"`
	Message string `json:"message"`
}

// DecodeRecord reads a JSON object with numbers kept exact, as every request
// and record is read.
func DecodeRecord(b []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, fmt.Errorf("trailing data after the JSON object")
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// Validate checks an instance, decoded with DecodeRecord, against the schema
// and returns every failure, in a stable order.
func Validate(s *Schema, v any) []Failure {
	var out []Failure
	validate(s, v, "", &out)
	return out
}

// ApplyDefaults fills every absent property that has a default, so that
// validation sees the final request (§6.2.3).
func ApplyDefaults(s *Schema, obj map[string]any) error {
	if s == nil {
		return nil
	}
	for name, p := range s.Properties {
		if _, present := obj[name]; present || len(p.Default) == 0 {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(p.Default))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			return fmt.Errorf("default of %q: %w", name, err)
		}
		obj[name] = v
	}
	return nil
}

func validate(s *Schema, v any, ptr string, out *[]Failure) {
	if s == nil {
		return
	}
	fail := func(kw, msg string) {
		*out = append(*out, Failure{Pointer: root(ptr), Keyword: kw, Message: msg})
	}
	if v == nil {
		if !s.Type.Nullable {
			fail("type", "must not be null")
		}
		return
	}
	switch s.Type.Name {
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			fail("type", "must be an object")
			return
		}
		for _, name := range s.Required {
			if _, ok := obj[name]; !ok {
				*out = append(*out, Failure{Pointer: ptr + "/" + escape(name), Keyword: "required", Message: "is required"})
			}
		}
		names := make([]string, 0, len(obj))
		for k := range obj {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, name := range names {
			sub, ok := s.Properties[name]
			if !ok {
				if s.AdditionalProperties != nil && !*s.AdditionalProperties {
					*out = append(*out, Failure{Pointer: ptr + "/" + escape(name), Keyword: "additionalProperties", Message: "is not a parameter of this request"})
				}
				continue
			}
			validate(sub, obj[name], ptr+"/"+escape(name), out)
		}
	case "array":
		arr, ok := v.([]any)
		if !ok {
			fail("type", "must be an array")
			return
		}
		if s.MinItems != nil && len(arr) < *s.MinItems {
			fail("minItems", fmt.Sprintf("must have at least %d items", *s.MinItems))
		}
		if s.MaxItems != nil && len(arr) > *s.MaxItems {
			fail("maxItems", fmt.Sprintf("must have at most %d items", *s.MaxItems))
		}
		for i, item := range arr {
			validate(s.Items, item, ptr+"/"+strconv.Itoa(i), out)
		}
	case "string":
		str, ok := v.(string)
		if !ok {
			fail("type", "must be a string")
			return
		}
		if len(s.Enum) > 0 && !contains(s.Enum, str) {
			fail("enum", "must be one of: "+strings.Join(s.Enum, ", "))
		}
		switch s.Format {
		case "date":
			if _, err := time.Parse(time.DateOnly, str); err != nil {
				fail("format", "must be a date, YYYY-MM-DD")
			}
		case "date-time":
			if _, err := time.Parse(time.RFC3339, str); err != nil {
				fail("format", "must be an RFC 3339 date-time with an offset")
			}
		}
	case "integer", "number":
		n, ok := v.(json.Number)
		if !ok {
			fail("type", "must be a "+s.Type.Name)
			return
		}
		f, err := n.Float64()
		if err != nil {
			fail("type", "must be a "+s.Type.Name)
			return
		}
		if s.Type.Name == "integer" {
			if _, err := strconv.ParseInt(n.String(), 10, 64); err != nil || math.Abs(f) > 1<<53-1 {
				fail("type", "must be an integer within the I-JSON range")
				return
			}
		}
		if s.Minimum != nil && f < *s.Minimum {
			fail("minimum", fmt.Sprintf("must be at least %v", *s.Minimum))
		}
		if s.Maximum != nil && f > *s.Maximum {
			fail("maximum", fmt.Sprintf("must be at most %v", *s.Maximum))
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			fail("type", "must be true or false")
		}
	}
}

func root(ptr string) string {
	if ptr == "" {
		return "/"
	}
	return ptr
}

func escape(name string) string {
	return strings.ReplaceAll(strings.ReplaceAll(name, "~", "~0"), "/", "~1")
}
