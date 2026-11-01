package views

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"

	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/protocol/arrowschema"
	"atlas.local/sdk/go/protocol/ijson"
)

// defaultLimit is how many matches a search answers when its request
// names no limit.
const defaultLimit = 10

// match is one row a search keeps.
type match struct {
	rec   *ijson.Object
	exact bool
	score float64
	code  string
}

// better orders matches: an exact code first, then the share of words
// found, then by code.
func better(a, b match) int {
	switch {
	case a.exact != b.exact:
		if a.exact {
			return -1
		}
		return 1
	case a.score != b.score:
		if a.score > b.score {
			return -1
		}
		return 1
	}
	return strings.Compare(a.code, b.code)
}

// search ranks every row in scope by the request's words: an exact code
// first, then the share of the words found in the code or name, then by
// code. Rows that match no word are left out. It keeps only the best
// limit rows as it reads, so its memory is bounded by the limit, not by
// how many rows the scope holds.
func (p *Provider) search(ctx context.Context, v *view, inv *ports.Invocation) (*ports.Answer, error) {
	rd, err := v.read(inv)
	if err != nil {
		return nil, err
	}
	sp := v.model.Search
	text, _ := inv.Request.Get(sp.Words)
	whole, _ := text.(string)
	words := splitWords(whole)
	whole = strings.TrimSpace(whole)
	limit := defaultLimit
	if l, ok := inv.Request.Get(sp.Limit); ok {
		if n, isNumber := l.(ijson.Number); isNumber {
			if x, isInt := n.Int64(); isInt && x > 0 {
				limit = int(min(x, MaxValues))
			}
		}
	}
	res, err := p.run(ctx, v, inv, rd.req)
	if err != nil {
		return nil, err
	}
	stream := &answer{p: p, v: v, rd: &read{req: rd.req, fields: rd.fields, props: rd.fields, row: v.row.Project(rd.fields)}, src: res.Batches, now: p.clock.Now()}
	stream.rd.answer = rd.req.Writer
	defer stream.Close() // on a path that already fails; the end closes it and says so
	top := make([]match, 0, limit+1)
	for {
		b, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		rows, err := arrowschema.ToRows(b, stream.rd.row)
		b.Release()
		if err != nil {
			return nil, internalBecause(err, "a row of %s has no JSON form", v.pinned)
		}
		for _, rec := range rows {
			m, ok := rank(sp, rec, words, whole)
			if !ok {
				continue
			}
			i, _ := slices.BinarySearchFunc(top, m, better)
			if i >= limit {
				continue
			}
			top = slices.Insert(top, i, m)
			if len(top) > limit {
				top = top[:limit]
			}
		}
	}
	if err := stream.Close(); err != nil {
		return nil, err
	}
	out := make([]*ijson.Object, len(top))
	for i, m := range top {
		row := ijson.NewObject()
		for _, prop := range rd.props {
			if val, ok := m.rec.Get(prop); ok {
				row.Set(prop, val)
			}
		}
		out[i] = row
	}
	ans := &ports.Answer{}
	if p.rows {
		ans.Rows = ports.SliceRows(out)
	} else {
		b, err := arrowschema.ToBatch(p.mem, rd.answer, rd.row, out)
		if err != nil {
			return nil, internalBecause(err, "the matches of %s have no Arrow form", v.pinned)
		}
		ans.Batches = &batches{schema: rd.answer, list: []arrow.RecordBatch{b}}
	}
	if res.QueryID != "" {
		ans.Settle = p.settle([]string{res.QueryID})
	}
	return ans, nil
}

// rank is how one row matches the words, and whether it matches at all;
// its score and reason are written into it.
func rank(sp *Search, rec *ijson.Object, words []string, whole string) (match, bool) {
	c, _ := rec.Get(sp.Code)
	n, _ := rec.Get(sp.Name)
	code, _ := c.(string)
	name, _ := n.(string)
	m := match{rec: rec, code: code}
	var inCode, inName []string
	for _, w := range words {
		switch {
		case strings.Contains(strings.ToLower(code), w):
			inCode = append(inCode, w)
		case strings.Contains(strings.ToLower(name), w):
			inName = append(inName, w)
		}
	}
	var why []string
	switch {
	case whole != "" && strings.EqualFold(code, whole):
		m.exact, m.score = true, 1
		why = append(why, "its code is "+code)
	case len(inCode)+len(inName) == 0:
		return m, false
	default:
		m.score = float64(len(inCode)+len(inName)) / float64(len(words))
		if len(inCode) > 0 {
			why = append(why, "the code has "+strings.Join(inCode, ", "))
		}
		if len(inName) > 0 {
			why = append(why, "the name has "+strings.Join(inName, ", "))
		}
	}
	score, err := ijson.ECMAScriptNumber(m.score)
	if err != nil {
		return m, false
	}
	rec.Set(sp.Score, ijson.Number(score))
	rec.Set(sp.Why, strings.Join(why, "; "))
	return m, true
}

// splitWords are the lower-cased words of a text: runs of letters and
// digits, each once.
func splitWords(text string) []string {
	var words []string
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !('a' <= r && r <= 'z' || '0' <= r && r <= '9' || r > 127)
	}) {
		if !slices.Contains(words, w) {
			words = append(words, w)
		}
	}
	return words
}

// batches is a BatchStream of batches already in memory.
type batches struct {
	schema *arrow.Schema
	list   []arrow.RecordBatch
	closed bool
}

func (b *batches) Schema() *arrow.Schema { return b.schema }

func (b *batches) Next(ctx context.Context) (arrow.RecordBatch, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.closed || len(b.list) == 0 {
		return nil, io.EOF
	}
	next := b.list[0]
	b.list = b.list[1:]
	return next, nil
}

func (b *batches) Close() error {
	if !b.closed {
		b.closed = true
		for _, r := range b.list {
			r.Release()
		}
		b.list = nil
	}
	return nil
}
