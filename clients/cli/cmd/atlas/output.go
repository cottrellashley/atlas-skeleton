package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	atlas "atlas.local/sdk/go"
	"atlas.local/sdk/go/kit/problem"
	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/ijson"
)

// fail prints a problem on standard error, as JSON in machine output and
// in words for a person, and is its exit code (§9.7.6). Every failure is a
// problem: anything else is internal.
func (c *cli) fail(err error) int {
	var p *protocol.Problem
	if !errors.As(err, &p) {
		p = problem.From(err)
		if p.Code == protocol.CodeInternal {
			p = protocol.NewProblem(protocol.CodeInternal, err.Error())
		}
	}
	if c.machine {
		b, _ := json.Marshal(p)
		fmt.Fprintln(c.stderr, string(b))
	} else {
		fmt.Fprintf(c.stderr, "%s: %s\n", p.Code, p.Detail)
		if p.Target != "" {
			fmt.Fprintf(c.stderr, "  target: %s\n", p.Target)
		}
		for _, d := range p.Details {
			fmt.Fprintf(c.stderr, "  - %s %s\n", d.Target, d.Detail)
		}
		if p.Next != "" {
			fmt.Fprintf(c.stderr, "  next: %s\n", p.Next)
		}
	}
	return protocol.ExitCode(p.Code)
}

// warning prints a warning as it arrives, on standard error, on every path
// (§9.7.4 step 11; R001): a JSON line in machine output, a line of words
// for a person.
func (c *cli) warning(w atlas.Warning) {
	if c.machine {
		b, _ := json.Marshal(map[string]atlas.Warning{"warning": w})
		fmt.Fprintln(c.stderr, string(b))
		return
	}
	fmt.Fprintf(c.stderr, "warning: %s: %s\n", w.Code, w.Text)
}

func (c *cli) printJSON(v any) error {
	var b []byte
	var err error
	if c.machine {
		b, err = json.Marshal(v)
	} else {
		b, err = json.MarshalIndent(v, "", "  ")
	}
	if err != nil {
		return protocol.NewProblem(protocol.CodeInternal, "the answer cannot be printed: "+err.Error())
	}
	_, err = fmt.Fprintln(c.stdout, string(b))
	return err
}

// say prints a confirmation: its record in machine output, words for a
// person.
func (c *cli) say(record any, format string, args ...any) error {
	if c.machine {
		return c.printJSON(record)
	}
	_, err := fmt.Fprintf(c.stdout, format, args...)
	return err
}

// list prints a listing: NDJSON items in machine output, a table of id,
// version and summary for a person.
func (c *cli) list(items []*ijson.Object) error {
	w := bufio.NewWriter(c.stdout)
	defer w.Flush()
	if c.machine {
		for _, it := range items {
			b, _ := ijson.Marshal(it)
			w.Write(b)
			w.WriteByte('\n')
		}
		return nil
	}
	t := newTable([]string{"id", "version", "summary"})
	for _, it := range items {
		t.add(it)
	}
	t.flush(w)
	return nil
}

// result prints a call's answer. A record is one JSON document. A table in
// machine output is its rows as NDJSON, then its end line as the protocol
// writes it (chunk-end.schema.json): the completion marker, with
// truncated, usage and any problem after the rows. A table that came as one
// JSON array, which is complete, ends with the end line that says so.
func (c *cli) result(r *atlas.Result) error {
	defer r.Close()
	if !r.Table() {
		return c.printJSON(r.Record)
	}
	w := bufio.NewWriterSize(c.stdout, 64<<10)
	defer w.Flush()
	var t *table
	if !c.machine {
		t = newTable(nil)
	}
	var failed error
	for {
		row, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			failed = err
			break
		}
		if t != nil {
			t.add(row)
			if t.full() {
				t.flush(w)
			}
			continue
		}
		b, _ := ijson.Marshal(row)
		w.Write(b)
		w.WriteByte('\n')
	}
	if t != nil {
		t.flush(w)
		cut := ""
		if r.Truncated() {
			cut = ", cut at your limit"
		}
		fmt.Fprintf(w, "(%d rows%s)\n", r.Rows(), cut)
		return failed
	}
	end := endLine{Atlas: "end", Rows: r.Rows(), Truncated: r.Truncated(), Usage: json.RawMessage("{}")}
	if e := r.End(); e != nil {
		end.Rows, end.Problem = e.Rows, e.Problem
		if len(e.Usage) > 0 {
			end.Usage = json.RawMessage(e.Usage)
		}
	}
	if failed == nil || r.End() != nil {
		b, _ := json.Marshal(end)
		w.Write(b)
		w.WriteByte('\n')
	}
	return failed
}

// endLine is chunk-end.schema.json's end line, its members in the
// protocol's order.
type endLine struct {
	Atlas     string            `json:"$atlas"`
	Rows      int64             `json:"rows"`
	Truncated bool              `json:"truncated"`
	Usage     json.RawMessage   `json:"usage"`
	Problem   *protocol.Problem `json:"problem"`
}

// table prints rows for a person: columns as wide as their first rows'
// values, up to maxWidth, a batch of rows at a time so memory stays
// bounded.
type table struct {
	columns []string
	rows    [][]string
	widths  []int
	header  bool
}

const (
	maxWidth  = 40
	tableRows = 200
)

func newTable(columns []string) *table { return &table{columns: columns} }

func (t *table) add(o *ijson.Object) {
	if t.columns == nil {
		t.columns = o.Names()
	}
	row := make([]string, len(t.columns))
	for i, name := range t.columns {
		v, ok := o.Get(name)
		if !ok || v == nil {
			continue
		}
		switch x := v.(type) {
		case string:
			row[i] = x
		case ijson.Number:
			row[i] = string(x)
		default:
			b, _ := ijson.Marshal(v)
			row[i] = string(b)
		}
	}
	t.rows = append(t.rows, row)
}

func (t *table) full() bool { return len(t.rows) >= tableRows }

func cell(s string, width int) string {
	if utf8.RuneCountInString(s) > width {
		r := []rune(s)
		s = string(r[:width-1]) + "…"
	}
	return s + strings.Repeat(" ", width-utf8.RuneCountInString(s))
}

func (t *table) flush(w io.Writer) {
	if t.widths == nil {
		if t.columns == nil {
			return
		}
		t.widths = make([]int, len(t.columns))
		for i, c := range t.columns {
			t.widths[i] = min(max(t.widths[i], utf8.RuneCountInString(c)), maxWidth)
		}
		for _, r := range t.rows {
			for i, v := range r {
				t.widths[i] = min(max(t.widths[i], utf8.RuneCountInString(v)), maxWidth)
			}
		}
	}
	if !t.header {
		t.header = true
		var head, rule []string
		for i, c := range t.columns {
			head = append(head, cell(c, t.widths[i]))
			rule = append(rule, strings.Repeat("-", t.widths[i]))
		}
		fmt.Fprintln(w, strings.TrimRight(strings.Join(head, "  "), " "))
		fmt.Fprintln(w, strings.Join(rule, "  "))
	}
	for _, r := range t.rows {
		var line []string
		for i, v := range r {
			line = append(line, cell(v, t.widths[i]))
		}
		fmt.Fprintln(w, strings.TrimRight(strings.Join(line, "  "), " "))
	}
	t.rows = t.rows[:0]
}
