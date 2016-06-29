package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/andygrunwald/go-jira"
)

const (
	issueTag = ` Undo Get Put |fmt `
)

type headers struct {
	Summary    string
	Type       string
	Status     string
	Assignee   string
	URL        string
	Components []string
}

func headersFromIssue(i *jira.Issue) *headers {
	u, _ := url.Parse(i.Self)
	br, _ := u.Parse("/browse/" + i.Key)
	r := headers{
		Summary:  i.Fields.Summary,
		Type:     i.Fields.Type.Name,
		Status:   i.Fields.Status.Name,
		Assignee: i.Fields.Assignee.Name,
		URL:      br.String(),
	}
	for _, c := range i.Fields.Components {
		r.Components = append(r.Components, c.Name)
	}
	return &r
}

func (h *headers) WriteTo(w io.Writer) {
	fmt.Fprintf(w, "Summary: %s\n", h.Summary)
	fmt.Fprintf(w, "Type: %s\n", h.Type)
	fmt.Fprintf(w, "Status: ")
	if strings.Contains(h.Status, " ") {
		fmt.Fprintf(w, "%q\n", h.Status)
	} else {
		fmt.Fprintf(w, "%s\n", h.Status)
	}
	fmt.Fprintf(w, "Assignee: %s\n", h.Assignee)
	fmt.Fprintf(w, "Components:")
	for _, c := range h.Components {
		if strings.Contains(c, " ") {
			fmt.Fprintf(w, " %q", c)
		} else {
			fmt.Fprintf(w, " %s", c)
		}
	}
	fmt.Fprintf(w, "\n")
	fmt.Fprintf(w, "URL: %s\n", h.URL)
	fmt.Fprint(w, "\n\n")
}

func (u *UI) createIssue() *win {
	return nil
}

func (u *UI) fetchIssue(w *win) {
	id := w.Title
	i, _, err := u.j.Issue.Get(id)
	if err != nil {
		u.err(err.Error())
		w.Del(true)
		return
	}
	w.headers = headersFromIssue(i)

	req, err := u.j.NewRequest("GET", fmt.Sprintf("/rest/api/2/issue/%s/comment", i.ID), nil)
	if err != nil {
		u.err(err.Error())
		w.Del(true)
		return
	}
	comment := &comments{}
	if _, err := u.j.Do(req, comment); err != nil {
		u.err(err.Error())
		w.Del(true)
		return
	}

	buf := &bytes.Buffer{}
	w.headers.WriteTo(buf)
	t, err := time.Parse(jiraDateFmt, i.Fields.Created)
	if err != nil {
		log.Println(err)
	}
	fmt.Fprintf(buf, "\nReported by %s (%s)\n", i.Fields.Reporter.Name, t.Format(time.Stamp))
	fmt.Fprintf(buf, "\n\t%s\n", wrap(i.Fields.Description, "\t"))
	comment.format(buf)

	// If the issue has changed state, re-write the possible actions.
	if i.Fields.Status.Name != w.issueState {
		req, err = u.j.NewRequest("GET", fmt.Sprintf("/rest/api/2/issue/%s/transitions", i.ID), nil)
		if err != nil {
			u.err(err.Error())
			w.Del(true)
			return
		}
		trs := &transitions{}
		if _, err := u.j.Do(req, trs); err != nil {
			u.err(err.Error())
			w.Del(true)
			return
		}
		trs.swap(w)
		w.issueState = i.Fields.Status.Name
	}

	w.Ctl("nomark")
	w.Clear()
	w.Write("data", buf.Bytes())
	w.Ctl("clean")
	w.Addr("0")
	w.Ctl("dot=addr")
	w.Ctl("mark")
	w.Ctl("show")
}

func (u *UI) putIssue(w *win) {
	c := w.comment()
	if len(c) == 0 {
		return
	}

	debug("putIssue add comment: %q\n", c)
	if cr, res, err := u.j.Issue.AddComment(w.Title, &jira.Comment{Body: c}); err != nil {
		debug("returned comment: %#v\n", cr)
		debug("returned response: %#v\n", res)
		u.err(fmt.Sprintf("error posting comment: %v\n", err))
	}
}

func (u *UI) transitionIssue(w *win, id string) {
	c := w.comment()
	// should also diff the headers
	// make some crazy struct
	// do a jira
	_ = c
}

type comments struct {
	Comments []jira.Comment `json:"comments"`
}

func (cs *comments) format(w io.Writer) {
	for _, c := range cs.Comments {
		t, err := time.Parse(jiraDateFmt, c.Updated)
		if err != nil {
			log.Println(err)
			continue
		}
		fmt.Fprintf(w, "\nComment by %s (%s)\n", c.Author.Name, t.Format(time.Stamp))
		fmt.Fprintf(w, "\n\t%s\n", wrap(c.Body, "\t"))
	}
}

type transitions struct {
	Transitions []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"transitions"`
}

func (tr *transitions) swap(w *win) {
	new := tr.set()
	b, err := w.ReadAll("tag")
	if err != nil {
		log.Println(err)
		return
	}
	bar := bytes.IndexRune(b, '|')
	bar++
	tag := strings.TrimRight(string(b[bar:]), " ")
	if w.tr != nil {
		for k := range w.tr {
			debug("removing transition: %q\n", k)
			tag = strings.Replace(tag, " "+k, "", 1)
		}
	}
	for k := range new {
		debug("adding transition: %q\n", k)
		if !strings.Contains(tag, k) {
			tag += " " + k
		}
	}
	w.Ctl("cleartag")
	w.Fprintf("tag", tag+" ")
	w.tr = new
}

func (tr *transitions) set() map[string]string {
	r := make(map[string]string)
	for _, t := range tr.Transitions {
		r[strings.Replace(strings.Title(t.Name), " ", "", -1)] = t.ID
	}
	return r
}