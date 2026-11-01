package pipeline

import (
	"strings"
	"testing"

	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/protocol"
)

// R081, 02-6 discovery rule 4, 06-2 rule 4: a call to a listed capability
// the caller cannot use is refused with the problem for its status and a
// next step that names atlas.access, with the capability to ask about.
func TestR081RefusalsOfAListedCapabilityNameAtlasAccess(t *testing.T) {
	r := newRig(t, rows(listRows("147HYC")...), Options{})
	withLimit(r, ports.Limit{Subject: "alice", Capability: "portfolio.list@v1", PerMinute: 1})
	if res, err := r.run(r.call("t-alice", listAt, `{}`)); err != nil {
		t.Fatal(err)
	} else {
		res.Close()
	}
	retired := r.reg.Snapshot().Lookup("data.status@v1").Current
	retired.Lifecycle = "retired"
	defer func() { retired.Lifecycle = "stable" }()
	for _, c := range []struct {
		name, token, path, body, code, pinned string
	}{
		{"denied", "t-dave", holdingsAt, `{"portfolios": ["147HYC"]}`, protocol.CodeDenied, "portfolio.holdings@v1"},
		{"not granted", "t-carol", holdingsAt, `{"portfolios": ["147HYC"]}`, protocol.CodeNotGranted, "portfolio.holdings@v1"},
		{"outside the scope", "t-bob", holdingsAt, `{"portfolios": ["GLOBAL-CREDIT"]}`, protocol.CodeNotGranted, "portfolio.holdings@v1"},
		{"limited", "t-alice", listAt, `{}`, protocol.CodeLimitExceeded, "portfolio.list@v1"},
		{"retired", "t-alice", statusAt, `{}`, protocol.CodeRetired, "data.status@v1"},
	} {
		_, err := r.run(r.call(c.token, c.path, c.body))
		p := problemOf(t, err)
		if p.Code != c.code || !strings.Contains(p.Next, "atlas.access@v1:fetch") || !strings.Contains(p.Next, c.pinned) {
			t.Errorf("%s: %s, next %q", c.name, p.Code, p.Next)
		}
	}
}
