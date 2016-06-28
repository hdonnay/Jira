package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"path"
	"regexp"
	"strings"
	"sync"
	"text/tabwriter"

	"9fans.net/go/acme"
	jira "github.com/andygrunwald/go-jira"
)

type win struct {
	*acme.Win
	Title string

	reload func(*win)
	put    func(*win)
}

func (w *win) Clear() {
	w.Addr(",")
	w.Write("data", nil)
}

func (w *win) Reload() {
	if w.reload != nil {
		w.reload(w)
	}
}

func (w *win) Put() {
	if w.put != nil {
		w.put(w)
	}
}

const addrdelim = "/[ \t\\n<>()\\[\\]]/"

func (w *win) loadText(e *acme.Event) {
	//	if len(e.Text) == 0 && e.Q0 < e.Q1 {
	var err error
	w.Addr("#%d,#%d+%s", e.Q0, e.Q0, addrdelim)
	e.Q0, e.Q1, err = w.ReadAddr()
	if err != nil {
		log.Println(err)
	}
	e.Q1--
	w.Addr("#%d,#%d", e.Q0, e.Q1)

	data, err := w.ReadAll("xdata")
	if err != nil {
		log.Println(err)
	}
	e.Text = data
}

func (w *win) loop(ui *UI) {
	defer ui.exit(w.Title)
	for e := range w.EventChan() {
		//log.Printf("event: %q %q\n", e.C2, string(e.Text))
		switch e.C2 {
		case 'x', 'X': // button 2
			switch cmd := strings.TrimSpace(string(e.Text)); cmd {
			case "Get":
				w.Reload()
			case "Put":
				w.Put()
			case "Del":
				w.Ctl("del")
			case "New":
				ui.createIssue()
			case "Search":
				// more complicated
				log.Println("asked to search, going to slink away...")
			default:
				w.WriteEvent(e)
			}
		case 'l', 'L': // button 3
			w.loadText(e)
			if !ui.look(string(e.Text)) {
				w.WriteEvent(e)
			}
		}
	}
}

type UI struct {
	sync.Mutex
	win    map[string]*win
	exited chan struct{}
	projRe *regexp.Regexp

	j      *jira.Client
	prefix string
}

func (u *UI) err(s string) {
	if !strings.HasSuffix(s, "\n") {
		s = s + "\n"
	}
	w := u.show("+Errors")
	if w == nil {
		w = u.new("+Errors")
	}
	w.Fprintf("body", "%s", s)
	w.Addr("$")
	w.Ctl("dot=addr")
	w.Ctl("show")
}

func (u *UI) start(prefix string, j *jira.Client) {
	if prefix == "" {
		prefix = "/jira/"
	}

	u.Lock()
	if u.win == nil {
		u.win = make(map[string]*win)
	}
	if u.prefix == "" {
		u.prefix = prefix
	}
	u.exited = make(chan struct{})

	l, _, err := j.Project.GetList()
	if err != nil {
		log.Println(err)
		close(u.exited)
		return
	}

	var r []string
	for _, p := range *l {
		r = append(r, "("+p.Key+")")
	}

	u.j = j
	u.projRe = regexp.MustCompile("^(" + strings.Join(r, "|") + ")-[0-9]+")
	u.Unlock()
	u.look("my-issues")
}

func (u *UI) new(title string) *win {
	u.Lock()
	defer u.Unlock()
	var err error
	w := &win{}
	w.Win, err = acme.New()
	if err != nil {
		u.err(err.Error())
		return nil
	}
	w.Title = title
	w.Name(path.Join(u.prefix, title))
	u.win[title] = w
	go w.loop(u)
	return w
}

func (u *UI) show(title string) *win {
	u.Lock()
	defer u.Unlock()
	if w, ok := u.win[title]; ok {
		w.Ctl("show")
		return w
	}
	return nil
}

func (u *UI) look(title string) bool {
	switch title {
	case "my-issues":
		if w := u.show("my-issues"); w == nil {
			w = u.new("my-issues")
			if w == nil {
				return false
			}
			w.Ctl("cleartag")
			w.Fprintf("tag", " New Get Search ")
			w.reload = u.fetchMine
			w.Fprintf("data", "Loading...\n")
			w.reload(w)
		}
		return true
	case "Projects":
		//
	case "Issues", "Search":
		//
	}
	if u.projRe.MatchString(title) {
		if w := u.show(title); w == nil {
			// open the issue
			w = u.new(title)

			w.Ctl("cleartag")
			w.Fprintf("tag", " New Get Search Put Advance ")
			w.reload = u.fetchIssue(title)
			w.Fprintf("data", "Loading...\n")
			w.reload(w)
		}
		return true
	}
	return false
}

func (u *UI) fetchMine(w *win) {
	l, _, err := u.j.Issue.Search(`assignee = currentUser() AND resolution = Unresolved order by updated DESC`, nil)
	if err != nil {
		u.err(err.Error())
		return
	}

	buf := &bytes.Buffer{}
	wr := tabwriter.NewWriter(buf, 4, 4, 1, '\t', 0)
	for _, i := range l {
		fmt.Fprintf(wr, "%s\t%s/%s\t%s\n",
			i.Key,
			i.Fields.Type.Name,
			i.Fields.Status.Name,
			i.Fields.Summary,
		)
	}
	wr.Flush()

	w.Clear()
	w.Write("data", buf.Bytes())
	w.Ctl("clean")
	w.Addr("0")
	w.Ctl("dot=addr")
	w.Ctl("show")
}

func (u *UI) fetchIssue(id string) func(*win) {
	return func(w *win) {
		i, _, err := u.j.Issue.Get(id)
		if err != nil {
			u.err(err.Error())
			w.Ctl("delete")
			return
		}
		req, err := u.j.NewRequest("GET", fmt.Sprintf("/rest/api/2/issue/%s/comment", i.ID), nil)
		if err != nil {
			u.err(err.Error())
			w.Ctl("delete")
			return
		}
		comment := comments{}
		if _, err := u.j.Do(req, &comment); err != nil {
			u.err(err.Error())
			w.Ctl("delete")
			return
		}

		buf := &bytes.Buffer{}
		u.issueHeader(buf, i)
		fmt.Fprintf(buf, "\nReported by %s (%s)\n", i.Fields.Reporter.Name, i.Fields.Created)
		fmt.Fprintf(buf, "\n\t%s\n", wrap(i.Fields.Description, "\t"))
		comment.format(buf)

		w.Clear()
		w.Write("data", buf.Bytes())
		w.Ctl("clean")
		w.Addr("0")
		w.Ctl("dot=addr")
		w.Ctl("show")
	}
}

func (u *UI) exit(title string) {
	u.Lock()
	defer u.Unlock()
	delete(u.win, title)
	if len(u.win) == 0 {
		close(u.exited)
	}
}

func (u *UI) createIssue() *win {
	return nil
}

func (u *UI) issueHeader(w io.Writer, i *jira.Issue) {
	fmt.Fprintf(w, "Title: %s\n", i.Fields.Summary)
	fmt.Fprintf(w, "Type: %s\n", i.Fields.Type.Name)
	fmt.Fprintf(w, "Status: %q\n", i.Fields.Status.Name)
	fmt.Fprintf(w, "Assignee: %s\n", i.Fields.Assignee.Name)
	fmt.Fprintf(w, "Components:")
	for _, c := range i.Fields.Components {
		if strings.Contains(c.Name, " ") {
			fmt.Fprintf(w, " %q", c.Name)
		} else {
			fmt.Fprintf(w, " %s", c.Name)
		}
	}
	earl := u.j.GetBaseURL()
	br, _ := (&earl).Parse("/browse/" + i.Key)
	fmt.Fprintf(w, "\nURL: %s\n", br.String())
	fmt.Fprint(w, "\n\n")
}