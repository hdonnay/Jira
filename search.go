package main

import (
	"bytes"
	"fmt"
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

	buf := &bytes.Buffer{}
	fmt.Fprint(buf, "\n")
	for _, i := range r {
		fmt.Fprintf(buf, issueFmt,
			i.Key,
			i.Fields.Type.Name,
			i.Fields.Status.Name,
			i.Fields.Summary,
		)

	}

	w.Ctl("nomark")
	w.Addr("2,")
	w.Write("data", buf.Bytes())
	w.Ctl("mark")
	w.Ctl("clean")
	eol(w, 1)
	w.Ctl("show")
}