package main

import (
	"bytes"
	"io"
	"log"
	"unicode"
)

// This is something like n^2 worst-case.
//
// []string must be sorted
func diffStrings(new, old []string) (added, removed []string) {
Add:
	for _, x := range new {
		for _, y := range old {
			if x == y {
				continue Add
			}
		}
		added = append(added, x)
	}
Rem:
	for _, x := range old {
		for _, y := range new {
			if x == y {
				continue Rem
			}
		}
		removed = append(removed, x)
	}

	return added, removed
}

func unquote(b []byte) []string {
	r := bytes.NewReader(b)
	var err error
	var ch rune
	var in bool
	var i int
	s := []string{""}
	for ch, _, err = r.ReadRune(); err == nil; ch, _, err = r.ReadRune() {
		switch {
		case ch == '\\':
			ch, _, err = r.ReadRune()
			if ch != '"' {
				s[i] += `\`
			}
			s[i] += string(ch)
		case ch == '"':
			in = !in
		case unicode.IsSpace(ch):
			if in {
				s[i] += string(ch)
				continue
			}
			s = append(s, "")
			i++
		default:
			s[i] += string(ch)
		}
	}
	if err != io.EOF {
		log.Println(err)
	}
	return s
}
