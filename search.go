package main

import (
	"bytes"
)

func (u *UI) search(w *win) {
	w.Addr("1")
	b, err := w.ReadAll("xdata")
	if err != nil {
		u.err(err.Error())
		return
	}
	q := string(bytes.TrimSpace(b[6:]))
	if q == "" {
		return
	}
	debug("searching: %q", q)

	r, _, err := u.j.Issue.Search(q, nil)
	if err != nil {
		u.err(err.Error())
		return
	}

	var buf bytes.Buffer
	if err := tmpls.ExecuteTemplate(&buf, "issues", r); err != nil {
		u.err(err.Error())
		return
	}

	w.Ctl("nomark")
	w.Addr("2,")
	w.Fprintf("data", "\n")
	w.Write("data", buf.Bytes())
	w.Ctl("mark")
	w.Ctl("clean")
	eol(w, 1)
	w.Ctl("show")
}
