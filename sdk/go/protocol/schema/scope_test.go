package schema

import (
	"strings"
	"testing"
)

// C1, the access-dimension amendment (ASSUMPTIONS.md A-200): x-atlas-scope
// names the access dimension a string's value is a code of, in a request or
// a result, written beside the property or beside its $ref. It is a
// dimension's name, and stands only on a string.
func TestC1ScopeAnnotatesADimension(t *testing.T) {
	s := compile(t, Open, `"type": "object", "$defs": {"code": {"type": "string"}}, "properties": {
	  "workspace": {"type": "string", "x-atlas-scope": "workspace", "description": "W."},
	  "maybe": {"type": ["string", "null"], "x-atlas-scope": "workspace", "description": "M."},
	  "referred": {"$ref": "#/$defs/code", "x-atlas-scope": "desk", "description": "R."},
	  "plain": {"type": "string", "description": "P."}}`)
	for name, want := range map[string]string{"workspace": "workspace", "maybe": "workspace", "referred": "desk", "plain": ""} {
		p, _ := s.Property(name)
		got, ok := p.Scope()
		if got != want || ok != (want != "") {
			t.Errorf("%s: Scope() = %q, %v; want %q", name, got, ok, want)
		}
	}
	req := compile(t, Request, `"type": "object", "additionalProperties": false, "properties": {"workspaces": {"type": "array", "items": {"type": "string", "x-atlas-scope": "workspace", "description": "W."}, "description": "Ws."}}`)
	ws, _ := req.Property("workspaces")
	if got, ok := ws.Items().Scope(); !ok || got != "workspace" {
		t.Errorf("a request's items: Scope() = %q, %v", got, ok)
	}
	for _, bad := range []string{
		`"type": "object", "properties": {"w": {"type": "string", "x-atlas-scope": "Workspace", "description": "W."}}`,
		`"type": "object", "properties": {"w": {"type": "string", "x-atlas-scope": "", "description": "W."}}`,
		`"type": "object", "properties": {"w": {"type": "string", "x-atlas-scope": ["workspace"], "description": "W."}}`,
		`"type": "object", "properties": {"w": {"type": "integer", "x-atlas-scope": "workspace", "description": "W."}}`,
		`"type": "object", "properties": {"w": {"type": "object", "x-atlas-scope": "workspace", "description": "W."}}`,
		`"type": "object", "properties": {"w": {"type": "string", "x-atlas-scope": "` + strings.Repeat("a", MaxDimension+1) + `", "description": "W."}}`,
	} {
		if vs := refused(t, Open, bad); vs[0].Rule != "x-atlas-scope" {
			t.Errorf("%s: refused for %+v, want x-atlas-scope", bad, vs)
		}
	}
}
