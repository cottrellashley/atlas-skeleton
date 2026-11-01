package clock

import (
	"testing"
	"time"
)

func TestManual(t *testing.T) {
	at := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	m := NewManual(at)
	if !m.Now().Equal(at) {
		t.Fatal(m.Now())
	}
	if got := m.Advance(90 * time.Second); !got.Equal(at.Add(90*time.Second)) || !m.Now().Equal(got) {
		t.Fatal(got)
	}
	m.Set(at)
	if !m.Now().Equal(at) {
		t.Fatal(m.Now())
	}
	var c Clock = Func(func() time.Time { return at })
	if !c.Now().Equal(at) {
		t.Fatal(c.Now())
	}
	if System.Now().IsZero() {
		t.Fatal("the system clock")
	}
}
