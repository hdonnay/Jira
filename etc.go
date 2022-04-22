package main

import (
	"bufio"
	"bytes"
	"io"
	"log"
	"strings"
	"unicode"
	"unicode/utf8"
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
			if err != nil {
				break
			}
			if ch != '"' {
				s[i] += `\`
			}
			s[i] += string(ch)
		case ch == '"':
			in = !in
		case ch == '\'':
			ch, _, err = r.ReadRune()
			if err != nil {
				break
			}
			if ch == '\'' {
				s[i] += `'`
				break
			}
			s[i] += string(ch)
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

func eol(w *win, l int) {
	w.Addr("%d", l)
	_, q1, _ := w.ReadAddr()
	q1--
	w.Addr("#%d", q1)
	w.Ctl("dot=addr")
}

var (
	codestart = []byte("{code")
	codeend   = []byte("{code}")
)

func wrap(t, prefix string) string {
	raw := false
	max := *wrapWidth
	var out strings.Builder
	s := bufio.NewScanner(strings.NewReader(strings.TrimSpace(t)))
	for s.Scan() {
		out.WriteString(prefix)
		line := s.Bytes()
		// try to handle code blocks nicely
		if bytes.HasPrefix(line, codestart) || bytes.HasSuffix(line, codeend) {
			raw = !raw
		}
		if !raw {
			line = bytes.TrimSpace(line)
			for len(line) > max {
				i := bytes.LastIndexFunc(line[:max], unicode.IsSpace)
				if i < 0 {
					// Managed to construct a run of text longer than our wrap,
					// so just grab the first space.
					i = bytes.IndexFunc(line, unicode.IsSpace)
					if i < 0 {
						break
					}
				}
				out.Write(line[:i])
				out.WriteByte('\n')
				out.WriteString(prefix)
				_, sz := utf8.DecodeRune(line[i:])
				line = line[i+sz:] // skip the space
			}
		}
		out.Write(line)
		out.WriteByte('\n')
	}
	return out.String()
}

var jqlSan = strings.NewReplacer("\n\t", " ", "\n", " ", "\t", " ")
