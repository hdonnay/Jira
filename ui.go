package main

import (
	"bytes"
	"log"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"

	"9fans.net/go/acme"
	"9fans.net/go/plan9/client"
	"9fans.net/go/plumb"
	jira "github.com/andygrunwald/go-jira"
)

const (
	addrdelim = "/[! \t\\n<>()\\[\\]\"']/"
	myIssues  = `assignee = currentUser() AND resolution = Unresolved order by updated desc`
)

type win struct {
	*acme.Win
	Title string
	// All windows should have put/get
	reload func(*win)
	put    func(*win)

	// If an issue window, all these should exist
	Issue      bool
	tr         map[string]string
	issueState string
	headers    *headers

	Search bool
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
Event:
	for e := range w.EventChan() {
		if e.C2 != 'I' && e.C2 != 'D' {
			debug("event: %q %04b %q %q\n", e.C2, e.Flag, string(e.Text), string(e.Arg))
		}
		switch e.C2 {
		case 'x', 'X': // button 2
			cmd := strings.TrimSpace(string(e.Text))
			switch cmd {
			case "Put":
				w.Put()
				fallthrough
			case "Get":
				w.Reload()
				continue
			case "New":
				ui.issueTemplate()
				continue
			case "Clear":
				if w.Search {
					w.Ctl("nomark")
					w.Addr("2,")
					w.Write("data", []byte{})
					w.Ctl("mark")
					w.Ctl("clean")
					eol(w, 1)
					continue
				}
			case "Search":
				ui.look("search")
				w := ui.show("search")
				if len(e.Arg) != 0 {
					w.Ctl("nomark")
					w.Addr(`1`)
					w.Fprintf("data", "Search %s\n", jqlSan.Replace(string(e.Arg)))
					w.Ctl("mark")
				}
				w.Reload()
				continue
			}
			if w.Issue {
				if id, ok := w.tr[cmd]; ok {
					debug("transition: %q %q\n", cmd, id)
					ui.transitionIssue(w, id)
					w.Reload()
					continue
				}
			}
			arg0, argv, ok := strings.Cut(cmd, " ")
			if !ok {
				break
			}
			switch arg0 {
			case "New":
				//ui.issueTemplate(string(e.Arg))
				ui.err("need to improve the issue template")
				continue
			case "Search":
				ui.look("search")
				w := ui.show("search")
				w.Ctl("nomark")
				w.Addr(`1`)
				w.Fprintf("data", "Search %s %s\n", jqlSan.Replace(argv), jqlSan.Replace(string(e.Arg)))
				w.Ctl("mark")
				w.Reload()
				continue
			}
			if strings.HasPrefix(cmd, "Search") {
				ui.look("search")
				w := ui.show("search")
				if len(cmd) != 6 {
					w.Ctl("nomark")
					w.Addr(`1`)
					w.Fprintf("data", "%s", cmd)
					if len(e.Arg) != 0 {
						w.Fprintf("data", " %s", string(e.Arg))
					}
					w.Fprintf("data", "\n")
					w.Ctl("mark")
				}
				w.Reload()
				continue
			}
		case 'l', 'L': // button 3
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
			if w.Issue {
				// Check if this is an attachment link, transform it and send to the plumber.
				i, _, err := ui.j.Issue.Get(w.Title, &jira.GetQueryOptions{
					Fields: "attachment",
				})
				if err != nil {
					ui.err(err.Error())
					continue
				}
				name := getFilename(e.Text)
				for _, a := range i.Fields.Attachments {
					if a.Filename != name {
						continue
					}
					debug("found %q: id %s", string(e.Text), a.ID)
					base := ui.j.GetBaseURL()
					rel, err := url.Parse(path.Join("secure", "attachment", a.ID))
					if err != nil {
						ui.err(err.Error())
						continue
					}
					rel.Path = strings.TrimLeft(rel.Path, "/")
					tgt := base.ResolveReference(rel).String() + "/"
					debug("plumbing %q", tgt)
					if ui.plumb == nil {
						continue
					}
					m := plumb.Message{
						Src:  "Jira",
						Type: "text",
						Data: []byte(tgt),
					}
					if err := m.Send(ui.plumb); err != nil {
						ui.err(err.Error())
					}
					continue Event
				}
			}
		}
		w.WriteEvent(e)
	}
}

func getFilename(b []byte) string {
	b = bytes.TrimLeft(b, "^")
	b, _, _ = bytes.Cut(b, []byte("|"))
	return string(b)
}

func (w *win) comment() string {
	w.Addr(`#0`)
	w.Addr(`/^\n/,/^\nReported by/`)
	q0, q1, err := w.ReadAddr()
	if err != nil {
		log.Println(err)
		return ""
	}
	q1 -= len("\nReported by")
	w.Addr(`#%d,#%d`, q0, q1)
	b, err := w.ReadAll("xdata")
	if err != nil {
		log.Println(err)
		return ""
	}
	s := strings.TrimSpace(string(b))
	if s != "" {
		s += "\n"
	}
	return s
}

type UI struct {
	sync.Mutex
	win    map[string]*win
	exited chan struct{}

	j      *jira.Client
	prefix string

	types   map[string]*jira.IssueType
	typesMu *sync.Mutex

	proj   jira.ProjectList
	projMu *sync.Mutex
	projRe *regexp.Regexp

	plumb *client.Fid
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

func New(prefix string, j *jira.Client) (*UI, error) {
	prefix = path.Join("/jira", prefix)
	u := &UI{
		j:      j,
		prefix: prefix,

		typesMu: &sync.Mutex{},
		projMu:  &sync.Mutex{},

		types:  make(map[string]*jira.IssueType),
		win:    make(map[string]*win),
		exited: make(chan struct{}),
	}
	pc, err := plumb.Open("send", 1) // WRONLY
	if err != nil {
		log.Printf("unable to open connection to plumber: %v", err)
	} else {
		u.plumb = pc
	}
	u.updateCaches()
	return u, nil
}

func (u *UI) updateCaches() {
	// TODO(hank) figure out best time to refresh these
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		u.typesMu.Lock()
		defer u.typesMu.Unlock()

		req, err := u.j.NewRequest("GET", "/rest/api/2/issuetype", nil)
		if err != nil {
			u.err(err.Error())
			return
		}
		var typesRes []jira.IssueType
		if _, err := u.j.Do(req, &typesRes); err != nil {
			u.err(err.Error())
			return
		}
		for i, t := range typesRes {
			u.types[t.Name] = &typesRes[i]
		}
	}()

	go func() {
		defer wg.Done()
		u.projMu.Lock()
		defer u.projMu.Unlock()
		l, _, err := u.j.Project.GetList()
		if err != nil {
			close(u.exited)
			return
		}
		var r []string
		for _, p := range *l {
			r = append(r, "("+p.Key+")")
		}
		u.projRe = regexp.MustCompile("^(" + strings.Join(r, "|") + ")-[0-9]+")
		u.proj = *l
	}()

	wg.Wait()
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
	w.Ctl("mark")
	w.Ctl("clean")
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
	case "my-issues", "mine", "Mine", "", "/":
		if w := u.show("my-issues"); w == nil {
			w = u.new("my-issues")
			if w == nil {
				return false
			}
			w.Ctl("cleartag")
			w.Fprintf("tag", " Get New Filters Search ")
			w.reload = u.fetchMine
			w.reload(w)
		}
		return true
	case "new-issue":
		if w := u.show("new-issue"); w == nil {
			if w = u.issueTemplate(); w == nil {
				return false
			}
		}
		return true
	case "Search", "search":
		if w := u.show("search"); w == nil {
			w = u.new("search")
			if w == nil {
				return false
			}
			w.Ctl("cleartag")
			w.Fprintf("tag", " Get Clear ")
			w.Fprintf("data", "Search %s\n", myIssues)
			eol(w, 1)
			w.Ctl("mark")
			w.Ctl("clean")
			w.Search = true
			w.reload = u.search
		}
		return true
	case "Filters", "filters":
		w := u.show("filters")
		if w != nil {
			return true
		}
		w = u.new("filters")
		if w == nil {
			return false
		}
		w.Ctl("cleartag")
		w.Fprintf("tag", " Get Search ")
		w.reload = u.fetchFilters
		w.reload(w)
		return true
	}
	if u.projRe.MatchString(title) {
		if w := u.show(title); w == nil {
			// open the issue
			w = u.new(title)

			w.Ctl("cleartag")
			w.Fprintf("tag", issueTag)
			w.reload = u.fetchIssue
			w.put = u.putIssue
			w.Issue = true
			w.reload(w)
		}
		return true
	}
	return false
}

func (u *UI) fetchMine(w *win) {
	l, _, err := u.j.Issue.Search(myIssues, nil)
	if err != nil {
		u.err(err.Error())
		return
	}

	w.Clear()
	var buf bytes.Buffer
	if err := tmpls.ExecuteTemplate(&buf, "issues", l); err != nil {
		u.err(err.Error())
		return
	}

	w.Write("data", buf.Bytes())
	w.Ctl("clean")
	w.Addr("0")
	w.Ctl("dot=addr")
	w.Ctl("show")
}

func (u *UI) fetchFilters(w *win) {
	fs, _, err := u.j.Filter.GetFavouriteList()
	if err != nil {
		u.err(err.Error())
		return
	}

	w.Clear()
	var buf bytes.Buffer
	if err := tmpls.ExecuteTemplate(&buf, "filters", fs); err != nil {
		u.err(err.Error())
		return
	}
	w.Write("data", buf.Bytes())
	w.Ctl("clean")
	w.Addr("0")
	w.Ctl("dot=addr")
	w.Ctl("show")
}

func (u *UI) exit(title string) {
	u.Lock()
	defer u.Unlock()
	delete(u.win, title)
	if len(u.win) == 0 {
		close(u.exited)
	}
}

func (u *UI) rename(old, new string) {
	u.Lock()
	defer u.Unlock()
	w, ok := u.win[old]
	if !ok {
		return
	}
	delete(u.win, old)
	u.win[new] = w
	w.Title = new
}

func (u *UI) leave() {
	u.Lock()
	defer u.Unlock()
	for title, w := range u.win {
		delete(u.win, title)
		w.Del(true)
	}
	close(u.exited)
}
