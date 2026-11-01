package fakes_test

import (
	"testing"

	"github.com/jhgonline/fi-app-atlas/gateway/fakes"
	"github.com/jhgonline/fi-app-atlas/gateway/porttest"
)

// The in-memory DataSource keeps the port's contract (§6.4.1, R118): the
// suite every DataSource runs.
func TestDataSource(t *testing.T) {
	porttest.DataSource(t, func(t *testing.T, tables []fakes.Table) *porttest.Rig {
		d := fakes.NewDataSource(tables...)
		return &porttest.Rig{Source: d, Runs: d.Runs, Calls: d.Calls, Hold: d.Hold, Fail: d.Fail}
	})
}
