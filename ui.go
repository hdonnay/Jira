package main

import (
	"bytes"
	"flag"
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
	Issue bool

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

const addrdelim = "/[ \t\\n<>()\\[\\]\"']/"

// This is modeled on a similar set of functions that seem to be in every acme program.
func (w *win) expand(e *acme.Event) {
	w.Addr("#%d-%s", e.Q0, addrdelim)
	l, _, err := w.ReadAddr()
	if err != nil {
		log.Println(err)
	}

	w.Addr("#%d+%s", e.Q0, addrdelim)
	r, _, err := w.ReadAddr()
	if err != nil {
		log.Println(err)
	}

	if r < l {
		l = 0
	} else {
		l++
	}

	w.Addr("#%d,#%d", l, r)
	data, err := w.ReadAll("xdata")
	if err != nil {
		log.Println(err)
	}
	e.Q0 = l
	e.Q1 = r
	e.Text = data
}

func (w *win) loop(ui *UI) {
	defer ui.exit(w.Title)
	for e := range w.EventChan() {
		switch e.C2 {
		case 'x', 'X': // button 2
			debug("event: %q %q\n", e.C2, string(e.Text))
			cmd := strings.TrimSpace(string(e.Text))
			switch cmd {
			case "Put":
				w.Put()
				fallthrough
			case "Get":
				w.Reload()
				continue
			case "New":
				ui.createIssue()
				continue
			case "Search":
				// launch a search window
				ui.err("Asked to Search, but I'm too dumb D:\n")
				continue
			case "Advance":
				if !w.Issue {
					ui.err("Can't Advance something other than a ticket.\n")
					continue
				}
				ui.err("Asked to Advance, but I'm too dumb D:\n")
				continue
			}
			if strings.HasPrefix(cmd, "Search") {
				query := strings.TrimSpace(cmd[6:])
				// do a search and return results
				_ = query
			}
		case 'l', 'L': // button 3
			debug("event: %x %q %q\n", e.Flag, e.C2, string(e.Text))
			if ui.look(string(e.Text)) {
				// we found it, or made it!
				continue
			}
			// this could be a built-in, punt without expanding
			if e.Flag&0x1 != 0 {
				break
			}
			w.expand(e)
			debug("expanded: %d-%d %q\n", e.Q0, e.Q1, string(e.Text))
			if ui.look(string(e.Text)) {
				continue
			}
		}
		w.WriteEvent(e)
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

	if flag.NArg() < 2 {
		u.look("my-issues")
		return
	}
	u.look(strings.Join(flag.Args()[1:], " "))
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
	debug("spawning: %q\n", title)
	w.Title = title
	w.Name(path.Join(u.prefix, title))
	u.win[title] = w
	go w.loop(u)
	return w
}

// show forces a window to the top and returns it, if it's found.
func (u *UI) show(title string) *win {
	u.Lock()
	defer u.Unlock()
	if w, ok := u.win[title]; ok {
		debug("showing: %q\n", title)
		w.Ctl("show")
		return w
	}
	return nil
}

// look is show-or-create.
//
// It understands a few magic strings to facilitate this.
func (u *UI) look(title string) bool {
	title = strings.TrimPrefix(title, u.prefix)
	debug("looking: %q\n", title)
	switch title {
	case "my-issues", "mine", "Mine":
		if w := u.show("my-issues"); w == nil {
			w = u.new("my-issues")
			if w == nil {
				return false
			}
			w.Ctl("cleartag")
			w.Fprintf("tag", " New Get Search ")
			w.reload = u.fetchMine
			w.reload(w)
		}
		return true
	case "Projects", "Issues", "Search":
		u.err(fmt.Sprintf("%q not implemented yet\n", title))
	}
	if u.projRe.MatchString(title) {
		if w := u.show(title); w == nil {
			// open the issue
			w = u.new(title)

			w.Ctl("cleartag")
			w.Fprintf("tag", " New Get Search Put Advance ")
			w.reload = u.fetchIssue(title)
			w.put = u.putIssue(title)
			w.Issue = true
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

func (u *UI) putIssue(id string) func(*win) {
	return func(w *win) {
		w.Addr(`#0`)
		w.Addr(`/^\n/,/^\nReported by/`)
		q0, q1, err := w.ReadAddr()
		if err != nil {
			log.Println(err)
			return
		}
		q1 -= len("\nReported by")
		w.Addr(`#%d,#%d`, q0, q1)
		b, err := w.ReadAll("xdata")
		if err != nil {
			log.Println(err)
			return
		}
		if len(b) == 0 {
			return
		}

		c := jira.Comment{
			Body: strings.TrimSpace(string(b)) + "\n",
		}

		debug("putIssue add comment:%q\n", c.Body)
		if cr, res, err := u.j.Issue.AddComment(id, &c); err != nil {
			debug("returned comment: %#v\n", cr)
			debug("returned response: %#v\n", res)
			log.Printf("error posting comment: %v\n", err)
		}
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