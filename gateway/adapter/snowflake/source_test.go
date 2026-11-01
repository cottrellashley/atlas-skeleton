package snowflake_test

import (
	"strings"
	"testing"

	"atlas.local/gateway/adapter/snowflake"
	"atlas.local/gateway/adapter/snowflake/enginetest"
	"atlas.local/gateway/fakes"
	"atlas.local/gateway/porttest"
)

// R118, R072, ATLAS-DATA-004: the Snowflake Source keeps the DataSource
// port's contract over a counting fake engine. Planning, the guard, the
// bulkhead, the tag, the answer's assembly and settlement are the adapter's
// real code; only the engine is fake.
func TestSourceOverACountingEngine(t *testing.T) {
	porttest.DataSource(t, func(t *testing.T, tables []fakes.Table) *porttest.Rig {
		engine := enginetest.Count(enginetest.NewMemory(tables...))
		s, err := snowflake.NewSource(engine, snowflake.Options{Registered: registered(tables)})
		if err != nil {
			t.Fatal(err)
		}
		return engine.Rig(s)
	})
}

// registered are the tables' objects, each [database, schema, object].
func registered(tables []fakes.Table) [][]string {
	var out [][]string
	for _, t := range tables {
		out = append(out, strings.Split(t.Name, "."))
	}
	return out
}
