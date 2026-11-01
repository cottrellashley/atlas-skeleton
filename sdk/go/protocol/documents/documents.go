// Package documents is the protocol's 21 documents (§9.3.1), as JSON
// Schemas compiled into one set, so that the gateway validates what it
// sends and a client what it receives (§9.3 rule 1). The files are the
// references' own (spec/protocol/references/schemas), embedded byte for
// byte; a test fails when they drift.
package documents

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	"atlas.local/sdk/go/protocol/ijson"
	"atlas.local/sdk/go/protocol/schema"
)

//go:embed schemas/*.schema.json
var files embed.FS

// Base is the $id every document's URI begins with.
const Base = "https://atlas.jhg.com/protocol/1.0/"

var (
	once sync.Once
	set  *schema.Set
	err  error
)

// Set is every document, compiled together.
func Set() *schema.Set {
	once.Do(func() {
		names, _ := fs.Glob(files, "schemas/*.schema.json")
		var docs []ijson.Value
		for _, n := range names {
			b, e := fs.ReadFile(files, n)
			if e != nil {
				err = e
				return
			}
			v, e := ijson.Parse(b)
			if e != nil {
				err = fmt.Errorf("documents: %s: %w", n, e)
				return
			}
			docs = append(docs, v)
		}
		set, err = schema.CompileSet(docs...)
	})
	if err != nil {
		panic(fmt.Sprintf("documents: the protocol's documents do not compile: %v", err))
	}
	return set
}

// Schema is one document by its name ("bootstrap", "problem"), or one of
// its definitions ("builtins#whoami_result").
func Schema(name string) *schema.Schema {
	doc, def, _ := strings.Cut(name, "#")
	ref := Base + doc + ".schema.json"
	if def != "" {
		ref += "#/$defs/" + def
	}
	s, ok := Set().Schema(ref)
	if !ok {
		panic("documents: no document " + name)
	}
	return s
}

// Validate is v's failures against a document, none when it is valid.
func Validate(name string, v ijson.Value) []schema.Failure { return Schema(name).Validate(v) }

// Files are the embedded documents, by file name.
func Files() fs.FS {
	sub, _ := fs.Sub(files, "schemas")
	return sub
}
