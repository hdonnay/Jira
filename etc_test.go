package main

import "testing"

var (
	quotes = []struct {
		In   []byte
		Want []string
	}{
		{
			In:   []byte(""),
			Want: []string{""},
		},
		{
			In:   []byte("simple one"),
			Want: []string{"simple", "one"},
		},
		{
			In:   []byte("complex \"test like\" this\\\""),
			Want: []string{"complex", "test like", "this\""},
		},
		{
			In:   []byte(`'don''t split'`),
			Want: []string{`don't split`},
		},
	}
	strs = []struct {
		Old, New       []string
		Added, Removed []string
	}{
		{
			Old:     []string{"one", "two"},
			New:     []string{"two", "three"},
			Added:   []string{"three"},
			Removed: []string{"one"},
		},
	}
)

func TestUnquote(t *testing.T) {
	for _, q := range quotes {
		have := unquote(q.In)
		for i := range have {
			if have[i] != q.Want[i] {
				t.Fatalf("%q != %q", have[i], q.Want[i])
			}
			t.Logf("%q == %q", have[i], q.Want[i])
		}
	}
}

func TestDiffStrings(t *testing.T) {
	for _, q := range strs {
		a, r := diffStrings(q.New, q.Old)
		if len(a) != len(q.Added) {
			t.Fatalf("%q != %q", a, q.Added)
		}
		if len(r) != len(q.Removed) {
			t.Fatalf("%q != %q", r, q.Removed)
		}
		for i := range a {
			if a[i] != q.Added[i] {
				t.Fatalf("%q != %q", a[i], q.Added[i])
			}
		}
		for i := range r {
			if r[i] != q.Removed[i] {
				t.Fatalf("%q != %q", r[i], q.Removed[i])
			}
		}
		t.Logf("%q == %q", a, q.Added)
		t.Logf("%q == %q", r, q.Removed)
	}
}
