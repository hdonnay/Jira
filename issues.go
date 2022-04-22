package main

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"log"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"
	"unicode"

	"github.com/andygrunwald/go-jira"
)

const (
	issueTag = ` Undo Get Put |fmt `
	pickLine = `/^%s: /+-`
)

var (
	//go:embed templates
	templates embed.FS

	tmpls = template.Must(template.New("").Funcs(tmplFuncs).ParseFS(templates, "templates/*"))

	tmplFuncs = map[string]any{
		// See etc.go:/^func wrap
		"wrap": wrap,
		"join": strings.Join,
		// Quote is actually "quote if contains space."
		"quote": func(in string) (string, error) {
			if strings.IndexFunc(in, unicode.IsSpace) == -1 {
				return in, nil
			}
			return strconv.Quote(in), nil
		},
		// Issuelink prints the URL that a user would use, given an API URL for an issue.
		"issuelink": func(i *jira.Issue) (string, error) {
			u, err := url.Parse(i.Self)
			if err != nil {
				return "", err
			}
			u, err = u.Parse(path.Join("/browse", i.Key))
			if err != nil {
				return "", err
			}
			return u.String(), nil
		},
		// Sort sorts the string.
		"sort": func(s []string) []string {
			sort.Strings(s)
			return s
		},
		"time": func(t jira.Time) string {
			return time.Time(t).Local().Format(time.RFC1123)
		},
		// Some times aren't parsed by the jira package, so this is a dedicated function for it.
		"jiratime": func(in string) (string, error) {
			t, err := time.Parse(jiraDateFmt, in)
			if err != nil {
				return "", err
			}
			return t.Local().Format(time.RFC1123), nil
		},
	}
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

	var data struct {
		Projects []string
		Types    []string
	}
	u.projMu.Lock()
	data.Projects = make([]string, len(u.proj))
	for i := range u.proj {
		data.Projects[i] = u.proj[i].Key
	}
	u.projMu.Unlock()

	u.typesMu.Lock()
	data.Types = make([]string, 0, len(u.types))
	for t := range u.types {
		data.Types = append(data.Types, t)
	}
	u.typesMu.Unlock()

	var buf bytes.Buffer
	if err := tmpls.ExecuteTemplate(&buf, "new", &data); err != nil {
		u.err(err.Error())
		return nil
	}
	if _, err := w.Write("data", buf.Bytes()); err != nil {
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
	opts := jira.GetQueryOptions{}
	i, _, err := u.j.Issue.Get(id, &opts)
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

	var buf bytes.Buffer
	if err := tmpls.ExecuteTemplate(&buf, "issue", i); err != nil {
		u.err(err.Error())
		w.Del(true)
		return
	}

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
