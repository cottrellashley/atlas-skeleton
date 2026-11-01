package schema

import (
	"cmp"
	"encoding/binary"
	"fmt"
	"hash/maphash"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
)

// What keeps validation bounded (R049, assumptions A-190 and A-193). An
// instance inside the body limit may hold a failure in every value, so a
// validation keeps only the first failures by (pointer, keyword), in time
// linear in the instance: a value's path is written as a pointer only for
// a failure that is reported, the failures kept are few and sorted, the
// walk passes over a value under which nothing could be kept, and
// uniqueItems hashes its items rather than comparing every pair. A
// pointer is bounded too: a failure at a value whose pointer passes the
// bound is reported at the deepest value above it within the bound, so
// the bound holds the first failure's pointer, which the byte budget does
// not, and the problem built from it.

// ReportedPointer is the pointer a validation reports a failure at
// pointer under: the pointer itself, or, when it is longer than
// MaxPointerBytes, its deepest ancestor within them (assumption A-193).
func ReportedPointer(pointer string) string { return within(pointer, MaxPointerBytes) }

// within cuts a pointer longer than n bytes back to its deepest ancestor
// at most n long. A member name's "/" is written "~1", so every "/" of a
// pointer begins one of its segments.
func within(pointer string, n int) string {
	if len(pointer) <= n {
		return pointer
	}
	return pointer[:max(strings.LastIndexByte(pointer[:n+1], '/'), 0)]
}

// path is where a value stands in the instance: the member name or the
// array index that leads to it from its parent, and its pointer's length.
// Writing a pointer for every value would copy a long member name once
// per value beneath it, so the pointer is written only for a failure that
// is reported.
type path struct {
	up    *path
	name  string // a member's name, when index < 0
	index int    // an item's index, or -1 for a member
	depth int    // 1 for the instance's own members and items
	bytes int    // the length of its pointer
	// beyond marks a value whose pointer is longer than the validation
	// reports. What fails there or beneath it is reported at up, the
	// deepest value above it whose pointer is not, and every value beneath
	// it shares this path. Only the collector makes one, and it reads one
	// only through reported.
	beyond bool
}

// next is the path of the member called name (index < 0) or of the item
// at index, of the value at p.
func (p *path) next(name string, index int) path {
	q := path{up: p, name: name, index: index, depth: p.level() + 1, bytes: p.size() + 1}
	if index >= 0 {
		q.bytes += decimals(index)
	} else {
		q.bytes += len(name) + strings.Count(name, "~") + strings.Count(name, "/")
	}
	return q
}

// reported is the path a failure at p is reported at: p, or for a value
// beyond the bound, the deepest value above it within.
func (p *path) reported() *path {
	if p != nil && p.beyond {
		return p.up
	}
	return p
}

// level is the path's depth; the instance itself, nil, is 0.
func (p *path) level() int {
	if p == nil {
		return 0
	}
	return p.depth
}

// down appends the path's segments beneath top, from the top down.
func (p *path) down(top *path, buf []*path) []*path {
	start := len(buf)
	for q := p; q != top; q = q.up {
		buf = append(buf, q)
	}
	slices.Reverse(buf[start:])
	return buf
}

// pointer is the path as an RFC 6901 JSON Pointer.
func (p *path) pointer() string {
	var b strings.Builder
	b.Grow(p.size())
	var buf [16]*path
	for _, q := range p.down(nil, buf[:0]) {
		b.WriteByte('/')
		if q.index >= 0 {
			b.WriteString(strconv.Itoa(q.index))
		} else {
			b.WriteString(escape(q.name))
		}
	}
	return b.String()
}

// size is the length of the path's pointer, without writing it.
func (p *path) size() int {
	if p == nil {
		return 0
	}
	return p.bytes
}

// decimals is the number of decimal digits of a non-negative integer.
func decimals(i int) int {
	n := 1
	for ; i >= 10; i /= 10 {
		n++
	}
	return n
}

// comparePaths orders two paths as the code points of their pointers
// order them (Q-VAL-1, assumption A-009), without writing either. From the
// top, the first segment where they part decides, and a path comes before
// every path beneath it. Only that segment is read: a long member name
// both share costs nothing, since both read it from the same value.
func comparePaths(a, b *path) int {
	x, y := a, b
	for x.level() > y.level() {
		x = x.up
	}
	for y.level() > x.level() {
		y = y.up
	}
	if c := fromTop(x, y, x != a, y != b); c != 0 {
		return c
	}
	return cmp.Compare(a.level(), b.level())
}

// fromTop compares two paths of one depth segment by segment from the
// top, each segment followed by a slash when its path goes on beneath it.
// Where both paths are one, so are all the segments above.
func fromTop(x, y *path, xMore, yMore bool) int {
	if x == y {
		return 0
	}
	if c := fromTop(x.up, y.up, true, true); c != 0 {
		return c
	}
	return compareSegments(x, xMore, y, yMore)
}

// compareSegments orders two segments of the same value, each followed by
// a slash when its path goes on beneath it; 0 when they are the same.
func compareSegments(p *path, pMore bool, q *path, qMore bool) int {
	switch {
	case p.index >= 0 && q.index >= 0:
		return compareIndexes(p.index, q.index)
	case p.index < 0 && q.index < 0:
		if p.name == q.name {
			return 0
		}
		return compareNames(p.name, pMore, q.name, qMore)
	}
	// An item and a member never stand in the same value; still, as written.
	return compareNames(p.text(), pMore, q.text(), qMore)
}

func (p *path) text() string {
	if p.index >= 0 {
		return strconv.Itoa(p.index)
	}
	return p.name
}

// compareIndexes orders two array indexes as their decimal digits order
// them: /10 before /2. A slash sorts before every digit, so where one
// index's digits begin the other's, it comes first whatever follows it.
func compareIndexes(a, b int) int {
	if a == b {
		return 0
	}
	da, db := decimals(a), decimals(b)
	pa, pb := a, b
	for i := da; i > db; i-- {
		pa /= 10
	}
	for i := db; i > da; i-- {
		pb /= 10
	}
	if pa != pb {
		return cmp.Compare(pa, pb)
	}
	return cmp.Compare(da, db)
}

// compareNames orders two member names as a pointer writes them, "~" as
// "~0" and "/" as "~1", each followed by a slash when its path goes on.
func compareNames(a string, aMore bool, b string, bMore bool) int {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	// Escaping maps each byte on its own, so what the names share compares
	// equal written either way.
	ea, eb := escaped{s: a[i:], more: aMore}, escaped{s: b[i:], more: bMore}
	for {
		ca, okA := ea.next()
		cb, okB := eb.next()
		switch {
		case !okA || !okB:
			return cmp.Compare(boolInt(okA), boolInt(okB))
		case ca != cb:
			return cmp.Compare(ca, cb)
		}
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// escaped reads a member name byte by byte as a pointer writes it, then
// the slash that follows when the path goes on.
type escaped struct {
	s      string
	i      int
	second byte // the second byte of an escape, or 0
	more   bool
}

func (e *escaped) next() (byte, bool) {
	switch {
	case e.second != 0:
		c := e.second
		e.second = 0
		return c, true
	case e.i < len(e.s):
		c := e.s[e.i]
		e.i++
		switch c {
		case '~':
			e.second = '0'
			return '~', true
		case '/':
			e.second = '1'
			return '~', true
		}
		return c, true
	case e.more:
		e.more = false
		return '/', true
	}
	return 0, false
}

// collector is what one validation has found, within bounds. In full it
// keeps the first max failures by (pointer, keyword), each pair once and
// with the message it was first found with, and whether there were more,
// at pointers of at most pointer bytes; for valid it keeps only whether
// there is one, and the walk stops there.
type collector struct {
	first   bool
	max     int
	pointer int     // the longest pointer reported
	kept    []found // sorted
	more    bool
	steps   int // the work done: values visited, items hashed, comparisons
}

// beneath ends the message of a failure reported above the value it is
// about, whose own pointer is too long to report.
const beneath = " (at a value beneath, whose pointer is too long to report)"

type found struct {
	at      *path
	keyword string
	message string
}

// done reports whether the walk can stop: valid's question is answered.
func (c *collector) done() bool { return c.first && len(c.kept) > 0 }

// enter is the path of the member called name (index < 0) or of the item
// at index, of the value at up, and whether the walk visits it at all
// (skip). A path is made only for a value the walk visits, and valid's
// walk, which reports no pointer, makes none.
func (c *collector) enter(up *path, name string, index int) (*path, bool) {
	if c.first {
		return nil, true
	}
	if up != nil && up.beyond {
		return up, !c.skip(up)
	}
	p := up.next(name, index)
	if p.bytes > c.pointer {
		if c.skip(up) {
			return nil, false
		}
		return &path{up: up, beyond: true}, true
	}
	if c.skip(&p) {
		return nil, false
	}
	q := new(path)
	*q = p
	return q, true
}

// member is the path of a member the object at up may lack, for
// required.
func (c *collector) member(up *path, name string) *path {
	if c.first {
		return nil
	}
	if up != nil && up.beyond {
		return up
	}
	if p := up.next(name, -1); p.bytes <= c.pointer {
		return &p
	}
	return &path{up: up, beyond: true}
}

// skip reports whether the value at p can be passed over, because nothing
// found beneath it could be kept. Once more failures are found than are
// kept, a value whose pointer sorts after the last kept failure's holds
// none that is kept: every pointer reported beneath it begins with its
// own, and a value beyond the bound is reported where it is.
func (c *collector) skip(p *path) bool {
	if c.first {
		return len(c.kept) > 0
	}
	if !c.more {
		return false
	}
	c.steps++
	return comparePaths(p.reported(), c.kept[len(c.kept)-1].at) > 0
}

// add records a failure, at the path it is reported at. Its message is
// written only when it is kept.
func (c *collector) add(at *path, keyword, format string, args ...any) {
	if c.first {
		if len(c.kept) == 0 {
			c.kept = append(c.kept, found{at: at, keyword: keyword})
		}
		return
	}
	above := at != nil && at.beyond
	at = at.reported()
	i, same := c.search(at, keyword)
	switch {
	case same:
		return // a pair is reported once, with its first message
	case len(c.kept) < c.max:
	case i == len(c.kept):
		c.more = true
		return
	default:
		c.more = true
		c.kept = c.kept[:len(c.kept)-1]
	}
	message := fmt.Sprintf(format, args...)
	if above {
		message = clip(message, MaxMessageBytes-len(beneath)) + beneath
	} else {
		message = clip(message, MaxMessageBytes)
	}
	c.kept = slices.Insert(c.kept, i, found{at, keyword, message})
}

// search finds where a failure belongs among those kept, and whether the
// same pair is there already.
func (c *collector) search(at *path, keyword string) (int, bool) {
	order := func(f found) int {
		c.steps++
		if o := comparePaths(f.at, at); o != 0 {
			return o
		}
		return strings.Compare(f.keyword, keyword)
	}
	n := len(c.kept)
	if n == 0 {
		return 0, false
	}
	// A failure after every one kept is the common case, while the list
	// fills in order and once it is full.
	switch o := order(c.kept[n-1]); {
	case o < 0:
		return n, false
	case o == 0:
		return n - 1, true
	}
	i := sort.Search(n-1, func(i int) bool { return order(c.kept[i]) >= 0 })
	return i, order(c.kept[i]) == 0
}

// report writes the failures kept, in order, while their pointers and
// keywords stay within budget bytes, the first always, and reports
// whether the instance has failures beyond those written.
func (c *collector) report(budget int) ([]Failure, bool) {
	if len(c.kept) == 0 {
		return nil, false
	}
	out := make([]Failure, 0, len(c.kept))
	used := 0
	for i, f := range c.kept {
		used += f.at.size() + len(f.keyword)
		if i > 0 && used > budget {
			return out, true
		}
		out = append(out, Failure{f.at.pointer(), f.keyword, f.message})
	}
	return out, c.more
}

// clip cuts text longer than n bytes at a character boundary, and marks
// the cut.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:cut(s, n)] + "…"
}

// cut is the largest i ≤ n at which s may be cut between characters.
func cut(s string, n int) int {
	i := n
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return i
}

// smallArray is the most items uniqueItems compares pair by pair: fewer
// comparisons than a hash table costs to build.
const smallArray = 16

// repeated finds the first item equal to an earlier one, by ijson.Equal,
// and that earlier one: the indexes j < i of uniqueItems' message. It
// takes time linear in the items. Each is hashed consistently with Equal,
// and only items whose hashes agree are compared; the seed is random, so
// no instance can be built to collide.
func repeated(a []ijson.Value, c *collector) (int, int, bool) {
	if len(a) <= smallArray {
		for i := 1; i < len(a); i++ {
			for j := range i {
				c.steps++
				if ijson.Equal(a[j], a[i]) {
					return j, i, true
				}
			}
		}
		return 0, 0, false
	}
	seed := maphash.MakeSeed()
	hashes := make([]uint64, 0, smallArray)
	first := map[uint64]int{}
	for i, e := range a {
		c.steps++
		h, comparable := hashOf(seed, e)
		hashes = append(hashes, h)
		if !comparable {
			continue // it equals nothing, not even itself
		}
		j, seen := first[h]
		if !seen {
			first[h] = i
			continue
		}
		// The first earlier item with this hash is all but always the equal
		// one; past a collision, the others that share the hash, in order.
		for ; j < i; j++ {
			if hashes[j] == h {
				c.steps++
				if ijson.Equal(a[j], e) {
					return j, i, true
				}
			}
		}
	}
	return 0, 0, false
}

// hashOf hashes a value so that values ijson.Equal holds equal hash alike:
// numbers by their binary64 value (0 and -0 alike), objects whatever the
// order of their members. A value Equal holds equal to nothing, because a
// number in it is no binary64 value, is not hashed.
func hashOf(seed maphash.Seed, v ijson.Value) (uint64, bool) {
	switch x := v.(type) {
	case nil:
		return mix(seed, 1, 0), true
	case bool:
		if x {
			return mix(seed, 2, 1), true
		}
		return mix(seed, 2, 0), true
	case string:
		return mix(seed, 3, maphash.String(seed, x)), true
	case ijson.Number:
		f, err := x.Float64()
		if err != nil || math.IsNaN(f) {
			return 0, false
		}
		if f == 0 {
			f = 0 // -0 is 0
		}
		return mix(seed, 4, math.Float64bits(f)), true
	case []ijson.Value:
		h := mix(seed, 5, uint64(len(x)))
		for _, e := range x {
			eh, ok := hashOf(seed, e)
			if !ok {
				return 0, false
			}
			h = mix(seed, h, eh)
		}
		return h, true
	case *ijson.Object:
		var sum uint64
		ok := true
		x.Each(func(name string, m ijson.Value) bool {
			var mh uint64
			mh, ok = hashOf(seed, m)
			sum += mix(seed, maphash.String(seed, name), mh)
			return ok
		})
		return mix(seed, 6, sum), ok
	}
	return 0, false
}

func mix(seed maphash.Seed, a, b uint64) uint64 {
	var buf [16]byte
	binary.LittleEndian.PutUint64(buf[:8], a)
	binary.LittleEndian.PutUint64(buf[8:], b)
	return maphash.Bytes(seed, buf[:])
}
