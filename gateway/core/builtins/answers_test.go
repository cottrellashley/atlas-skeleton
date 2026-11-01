package builtins

import (
	"regexp"
	"testing"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/documents"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/gate"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
)

// answers are the documents each built-in interaction answers with
// (§9.3.2): a record's schema, or a page's and its items'.
var answers = map[string]struct{ record, item string }{
	"atlas.whoami@v1:fetch":     {record: "builtins#whoami_result"},
	"atlas.catalog@v1:fetch":    {record: "catalog-page"},
	"atlas.search@v1:fetch":     {record: "catalog-page"},
	"atlas.describe@v1:fetch":   {record: "describe"},
	"atlas.guide@v1:fetch":      {record: "builtins#guide_result"},
	"atlas.access@v1:fetch":     {record: "page", item: "builtins#access_entry"},
	"atlas.usage@v1:fetch":      {record: "page", item: "builtins#usage_entry"},
	"atlas.setup@v1:fetch":      {record: "builtins#setup_result"},
	"atlas.clients@v1:fetch":    {record: "page", item: "builtins#client_entry"},
	"atlas.operation@v1:list":   {record: "page", item: "operation"},
	"atlas.operation@v1:get":    {},
	"atlas.operation@v1:cancel": {},
	"atlas.operation@v1:watch":  {},
}

// AP-DOC-001, AP-DOC-002, §9.3 rule 1: every built-in answers, for every
// caller, each example its profile gives, and every answer validates
// against its document in builtins.schema.json and the documents beside
// it; an interaction the gateway does not serve answers a problem that is
// a problem document, with a next step.
func TestEveryBuiltinAnswerIsItsDocument(t *testing.T) {
	w := NewWorld(t)
	w.Provider = w.NewProvider(Options{CursorKey: w.Key, Clients: []Client{{Name: "atlas", Language: "shell", BuiltFor: "people", Version: "1.0.0", Install: "From a checkout.", Roles: []string{"explore"}, Levels: []string{"core"}}}})
	ps, err := Profiles()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, p := range ps {
		examples, _ := get(p.Usage, "examples").([]ijson.Value)
		for _, e := range examples {
			address := p.ID + "@v1:" + str(e, "interaction")
			doc, ok := answers[address]
			if !ok {
				t.Fatalf("an example of %s, which the built-ins do not serve", address)
			}
			seen[address] = true
			body := str(e, "request")
			for who := range Identities {
				o, prob := w.Serve(Call{Who: who, Address: address, Body: body})
				switch {
				case doc.record == "" && prob == nil:
					t.Errorf("%s answered %s", address, str(o))
				case doc.record == "":
					sent := *prob
					sent.RequestID = "req-1" // the edge's, as it writes every problem
					if fs := documents.Validate("problem", mustProblem(t, &sent)); len(fs) > 0 || prob.Next == "" {
						t.Errorf("%s: %+v is not a problem document: %v", address, prob, fs)
					}
				case prob != nil:
					t.Errorf("%s for %s %s: %v", address, who, body, prob)
				default:
					if fs := documents.Validate(doc.record, o); len(fs) > 0 {
						t.Errorf("%s for %s: not a %s: %v\n%s", address, who, doc.record, fs, str(o))
					}
					items, _ := get(o, "items").([]ijson.Value)
					for _, it := range items {
						if doc.item == "" {
							break
						}
						if fs := documents.Validate(doc.item, it); len(fs) > 0 {
							t.Errorf("%s for %s: an item is not a %s: %v\n%s", address, who, doc.item, fs, str(it))
						}
					}
				}
			}
		}
	}
	for address := range answers {
		if !seen[address] {
			t.Errorf("no built-in profile gives an example of %s", address)
		}
	}
}

// §3.1.4a, ATLAS-VISION-002, R082, A-016: every built-in profile is ready
// for agents, but for the description and unit rule over the protocol's
// own schemas, which a built-in uses by reference exactly as atlas/1.0
// fixes them (§9.3.2) and cannot change.
func TestR082TheBuiltinsAreReadyForAgents(t *testing.T) {
	ps, err := Profiles()
	if err != nil {
		t.Fatal(err)
	}
	protocolSchema := regexp.MustCompile(`^/schemas/\S+( has a description|, a number, has x-atlas-logical or x-atlas-unit .*)$`)
	for _, p := range ps {
		for _, miss := range gate.Ready(p) {
			if !protocolSchema.MatchString(miss) {
				t.Errorf("%s: %s", p.ID, miss)
			}
		}
		for _, name := range p.Order {
			if !hasExample(p.Usage, name) {
				t.Errorf("%s:%s has no example", p.ID, name)
			}
		}
		if w, _ := get(p.Usage, "when_not").(string); w == "" {
			t.Errorf("%s says nothing of when not to use it", p.ID)
		}
	}
}

func hasExample(usage *ijson.Object, interaction string) bool {
	examples, _ := get(usage, "examples").([]ijson.Value)
	for _, e := range examples {
		if str(e, "interaction") == interaction {
			return true
		}
	}
	return false
}
