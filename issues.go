package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/andygrunwald/go-jira"
)

const (
	issueTag  = ` Undo Get Put |fmt `
	issueTmpl = `Summary: 
Project: [%s]
Type: [%s]
Assignee: 
Labels: 
Components: 

<Description goes here>
`
	pickLine = `/^%s: /+-`
)

type headers struct {
	Summary    string
	Type       string
	Status     string
	Assignee   string
	Project    string
	URL        string
	Components []string
	Labels     []string
}

func headersFromIssue(i *jira.Issue) *headers {
	u, _ := url.Parse(i.Self)
	br, _ := u.Parse("/browse/" + i.Key)
	r := headers{
		Summary: i.Fields.Summary,
		Type:    i.Fields.Type.Name,
		Project: i.Fields.Project.Name,
		URL:     br.String(),
		Labels:  i.Fields.Labels,
	}
	for _, c := range i.Fields.Components {
		r.Components = append(r.Components, c.Name)
	}
	if i.Fields.Assignee != nil {
		r.Assignee = i.Fields.Assignee.Name
	}
	if i.Fields.Status != nil {
		r.Status = i.Fields.Status.Name
	}
	sort.Strings(r.Components)
	sort.Strings(r.Labels)
	return &r
}

func headersFromWindow(w *win) *headers {
	var b []byte
	var err error
	h := headers{}

	w.Addr(`#0`)
	w.Addr(pickLine, "Summary")
	b, err = w.ReadAll("xdata")
	if err == nil && len(b) != 0 {
		h.Summary = string(bytes.TrimSpace(b[len("Summary:"):]))
	}
	w.Addr(pickLine, "Project")
	b, err = w.ReadAll("xdata")
	if err == nil && len(b) != 0 {
		h.Project = string(bytes.TrimSpace(b[len("Project:"):]))
	}
	w.Addr(pickLine, "Type")
	b, err = w.ReadAll("xdata")
	if err == nil && len(b) != 0 {
		h.Type = string(bytes.TrimSpace(b[len("Type:"):]))
	}
	w.Addr(pickLine, "Status")
	b, err = w.ReadAll("xdata")
	if err == nil && len(b) != 0 {
		h.Status = string(bytes.TrimSpace(b[len("Status:"):]))
	}
	w.Addr(pickLine, "Assignee")
	b, err = w.ReadAll("xdata")
	if err == nil && len(b) != 0 {
		h.Assignee = string(bytes.TrimSpace(b[len("Assignee:"):]))
	}
	w.Addr(pickLine, "Components")
	b, err = w.ReadAll("xdata")
	if err == nil && len(b) != 0 {
		h.Components = unquote(bytes.TrimSpace(b[len("Components:"):]))
	}
	w.Addr(pickLine, "Labels")
	b, err = w.ReadAll("xdata")
	if err == nil && len(b) != 0 {
		h.Labels = unquote(bytes.TrimSpace(b[len("Labels:"):]))
	}
	w.Addr(pickLine, "URL")
	b, err = w.ReadAll("xdata")
	if err == nil && len(b) != 0 {
		h.URL = string(bytes.TrimSpace(b[len("URL:"):]))
	}

	return &h
}

func (h *headers) WriteTo(w io.Writer) {
	// We don't write out the project, because that's encoded in the issue key.
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
	fmt.Fprintf(w, "Labels:")
	for _, l := range h.Labels {
		if strings.Contains(l, " ") {
			fmt.Fprintf(w, " %q", l)
		} else {
			fmt.Fprintf(w, " %s", l)
		}
	}
	fmt.Fprintf(w, "\n")
	fmt.Fprintf(w, "URL: %s\n", h.URL)
	fmt.Fprintln(w)
}

func (w *win) diff() *issueUpdate {
	u := issueUpdate{}
	added := false
	debug("diff against: %q", w.headers)

	h := headersFromWindow(w)

	if h.Summary != w.headers.Summary {
		debug("summary set: %q", h.Summary)
		added = true
		u.Summary = []issueOp{{Set: h.Summary}}
	}
	if h.Assignee != w.headers.Assignee {
		debug("assignee set: %q", h.Assignee)
		added = true
		u.Assignee = []issueOp{{Set: map[string]string{"name": h.Assignee}}}
	}
	add, rem := diffStrings(h.Components, w.headers.Components)
	debug("components add/remove: %q %q", add, rem)
	for _, x := range add {
		added = true
		u.Components = append(u.Components, issueOp{Add: x})
	}
	for _, x := range rem {
		added = true
		u.Components = append(u.Components, issueOp{Remove: x})
	}
	add, rem = diffStrings(h.Labels, w.headers.Labels)
	debug("label add/remove: %q %q", add, rem)
	for _, x := range add {
		added = true
		u.Labels = append(u.Labels, issueOp{Add: x})
	}
	for _, x := range rem {
		added = true
		u.Labels = append(u.Labels, issueOp{Remove: x})
	}

	if !added {
		return nil
	}
	return &u
}

func (u *UI) issueTemplate() *win {
	w := u.new("new-issue")
	w.Ctl("cleartag")
	if err := w.Fprintf("tag", issueTag); err != nil {
		u.err(err.Error())
		return nil
	}
	w.put = u.createIssue
	w.Issue = true

	u.projMu.Lock()
	var proj []string
	for _, p := range u.proj {
		proj = append(proj, p.Key)
	}
	u.projMu.Unlock()

	u.typesMu.Lock()
	var types []string
	for n := range u.types {
		types = append(types, n)
	}
	u.typesMu.Unlock()

	err := w.Fprintf("data", issueTmpl,
		strings.Join(proj, "|"),
		strings.Join(types, "|"),
	)
	if err != nil {
		u.err(err.Error())
		return nil
	}

	return w
}

func (u *UI) createIssue(w *win) {
	h := headersFromWindow(w)
	switch {
	case h.Summary == "":
		u.err("blank summary")
		return
	case h.Project == "":
		u.err("blank project")
		return
	case h.Type == "":
		u.err("blank type")
		return
	case h.Assignee == "":
		u.err("blank Assignee")
		return
	}

	u.typesMu.Lock()
	var tid string
	if ty, ok := u.types[h.Type]; ok {
		tid = ty.ID
	}
	u.typesMu.Unlock()
	if tid == "" {
		u.err("bad issue type")
		return
	}

	u.projMu.Lock()
	var pid string
	ok := false
	for _, pr := range u.proj {
		if pr.Name == h.Project {
			pid = pr.ID
			ok = true
			break
		}
	}
	u.projMu.Unlock()
	if !ok {
		u.err("bad project name")
		return
	}

	var cmp []*jira.Component
	for _, c := range h.Components {
		cmp = append(cmp, &jira.Component{Name: c})
	}

	w.Addr(`#0`)
	w.Addr(`/^\n/,`)
	b, err := w.ReadAll("xdata")
	if err != nil {
		u.err(err.Error())
		return
	}
	desc := strings.TrimSpace(string(b))
	if desc == "" {
		u.err("empty description")
		return
	}
	desc += "\n"

	// do the jira
	// change the window title
	i := &jira.Issue{
		Fields: &jira.IssueFields{
			Description: desc,
			Summary:     h.Summary,
			Type:        jira.IssueType{ID: tid},
			Project:     jira.Project{ID: pid},
			Assignee:    &jira.User{Name: h.Assignee},
			Labels:      h.Labels,
			Components:  cmp,
		},
	}

	ni, _, err := u.j.Issue.Create(i)
	if err != nil {
		u.err(err.Error())
		return
	}

	u.rename(w.Title, ni.Key)
	w.reload = u.fetchIssue
	w.put = u.putIssue
	w.reload(w)
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
	fmt.Fprintf(buf, "Reported by %s (%s)\n", i.Fields.Reporter.Name, t.Format(time.Stamp))
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
	// check headers
	up := struct {
		Update *issueUpdate `json:"update,omitempty"`
	}{
		Update: w.diff(),
	}
	c := w.comment()
	if len(c) != 0 {
		if up.Update == nil {
			up.Update = &issueUpdate{}
		}
		up.Update.Comment = []issueOp{{Add: map[string]string{"body": c}}}
	}

	if up.Update != nil {
		debug("putIssue update: %v", up.Update)
		req, err := u.j.NewRequest("PUT", fmt.Sprintf("/rest/api/2/issue/%s", w.Title), up)
		if err != nil {
			u.err(err.Error())
			return
		}

		if res, err := u.j.Do(req, nil); err != nil {
			buf := &bytes.Buffer{}
			io.Copy(buf, res.Body)
			res.Body.Close()
			debug("returned response: %s\n", buf.String())
			u.err(fmt.Sprintf("error doing transition: %v\n", err))
		}
	} else {
		debug("putIssue: doing nothing")
	}
}

func (u *UI) transitionIssue(w *win, id string) {
	t := &transitionPut{
		Update: w.diff(),
	}
	t.Transition.ID = id

	c := w.comment()
	if len(c) != 0 {
		if t.Update == nil {
			t.Update = &issueUpdate{}
		}
		t.Update.Comment = []issueOp{{Add: map[string]string{"body": c}}}
	}

	// do a jira
	req, err := u.j.NewRequest("POST", fmt.Sprintf("/rest/api/2/issue/%s/transitions", w.Title), t)
	if err != nil {
		u.err(err.Error())
		return
	}

	if res, err := u.j.Do(req, nil); err != nil {
		debug("returned response: %#v\n", res)
		u.err(fmt.Sprintf("error doing transition: %v\n", err))
	}
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

type transitionPut struct {
	Update     *issueUpdate `json:"update,omitempty"`
	Transition struct {
		ID string `json:"id"`
	} `json:"transition"`
}

type issueUpdate struct {
	Summary    []issueOp `json:"summary,omitempty"`
	Comment    []issueOp `json:"comment,omitempty"`
	Assignee   []issueOp `json:"assignee,omitempty"`
	Components []issueOp `json:"components,omitempty"`
	Labels     []issueOp `json:"labels,omitempty"`
}

type issueOp struct {
	Set    interface{} `json:"set,omitempty"`
	Add    interface{} `json:"add,omitempty"`
	Remove interface{} `json:"remove,omitempty"`
	Edit   interface{} `json:"edit,omitempty"`
}
