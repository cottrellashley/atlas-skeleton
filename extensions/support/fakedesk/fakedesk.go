// Package fakedesk is a stand-in for Acme's support desk API, at the seam
// the real desk is reached through (HTTP): GET /tickets and GET /health. It
// records what it is asked, and can be made to fail.
package fakedesk

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync"
	"testing"
)

// Ticket is one ticket the desk holds.
type Ticket struct {
	ID        string `json:"id"`
	Workspace string `json:"workspace"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	OpenedAt  string `json:"opened_at"`
}

// Tickets are the desk's tickets: three workspaces, oldest first.
var Tickets = []Ticket{
	{"T-1001", "acme-eu", "Cannot sign in", "open", "2026-10-28T09:00:00Z"},
	{"T-1002", "acme-us", "Invoice is wrong", "pending", "2026-10-29T10:30:00Z"},
	{"T-1003", "acme-eu", "Export is slow", "closed", "2026-10-30T11:15:00Z"},
	{"T-1004", "globex", "Password reset loops", "open", "2026-10-31T08:45:00Z"},
	{"T-1005", "acme-us", "Report is empty", "open", "2026-10-31T16:20:00Z"},
}

// Desk is a running stand-in.
type Desk struct {
	URL string

	mu       sync.Mutex
	tickets  []Ticket
	asked    [][]string // the workspaces each GET /tickets named
	status   int        // the status every answer has, 0 for 200
	ignoring bool       // whether it ignores the workspace filter, as a faulty desk would
}

// New is a desk serving Tickets until the test ends.
func New(t testing.TB) *Desk {
	d := &Desk{tickets: slices.Clone(Tickets)}
	srv := httptest.NewServer(http.HandlerFunc(d.serve))
	t.Cleanup(srv.Close)
	d.URL = srv.URL
	return d
}

// Fail makes every answer have status (0 heals it).
func (d *Desk) Fail(status int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.status = status
}

// IgnoreFilter makes the desk answer every ticket whatever it is asked, so
// that only Atlas's own result check holds the answer to the caller's scope.
func (d *Desk) IgnoreFilter(on bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ignoring = on
}

// Asked are the workspaces each request for tickets named, in order.
func (d *Desk) Asked() [][]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.asked)
}

func (d *Desk) serve(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	status, ignoring, tickets := d.status, d.ignoring, slices.Clone(d.tickets)
	if r.URL.Path == "/tickets" {
		d.asked = append(d.asked, r.URL.Query()["workspace"])
	}
	d.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if status != 0 {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":"the desk is down"}`))
		return
	}
	switch r.URL.Path {
	case "/health":
		_, _ = w.Write([]byte(`{}`))
	case "/tickets":
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		out := []Ticket{}
		for _, t := range tickets {
			if !ignoring && len(q["workspace"]) > 0 && !slices.Contains(q["workspace"], t.Workspace) {
				continue
			}
			if s := q.Get("status"); s != "" && t.Status != s {
				continue
			}
			if limit > 0 && len(out) == limit {
				break
			}
			out = append(out, t)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tickets": out})
	default:
		http.NotFound(w, r)
	}
}
