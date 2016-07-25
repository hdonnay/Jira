package main

import (
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strings"

	jira "github.com/andygrunwald/go-jira"
)

var (
	authStr     = flag.String("a", "", "`username:password` combination")
	debugEnable = flag.Bool("D", false, "enable debug output")
	noPlumber   = flag.Bool("p", false, "disable plumber integration and don't linger")

	debug func(string, ...interface{}) = func(_ string, _ ...interface{}) {}
)

const jiraDateFmt = "2006-01-02T15:04:05.000-0700"

func usage() {
	fmt.Fprintf(os.Stderr, "Usage of %s:\n\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "\t%s [options] server [win]\n\n", os.Args[0])
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Credentials are looked for in a OS-specific secret store (linux only currently),\n")
	fmt.Fprintf(os.Stderr, "then in ~/.jira-creds. The 'a' flag will override both. They're all expected to\n")
	fmt.Fprintf(os.Stderr, "be in the same format.\n\n")
	fmt.Fprintf(os.Stderr, "If a window name is supplied, it will be opened instead of \"my-issues\".\n\n")
}

func init() {
	flag.Usage = usage
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

func main() {
	var auth struct {
		User string
		Pass string
		Err  error
	}
	var err error
	sig := make(chan os.Signal)
	signal.Notify(sig, os.Interrupt, os.Kill)
	flag.Parse()

	if flag.NArg() == 0 {
		log.Fatal("need to specify jira server")
	}
	if *debugEnable {
		debug = func(f string, v ...interface{}) {
			log.Output(2, fmt.Sprintf(f, v...))
		}
	}

	jURL, err := url.Parse(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}

	debug("hello")
	// Ideally we'd use some OAuth2 stuff, but it requires server-side setup for some reason.
	auth.User, auth.Pass, auth.Err = secretsOS(jURL.Host)
	if auth.Err != nil {
		debug("OS secret error: %v", auth.Err)
		auth.User, auth.Pass, auth.Err = secretsFile()
	}
	if *authStr != "" {
		auth.Err = nil
		upw := strings.SplitN(*authStr, ":", 2)
		if len(upw) != 2 {
			auth.Err = fmt.Errorf("unable to make sense of supplied username:password")
		} else {
			auth.User, auth.Pass = upw[0], upw[1]
		}
	}
	if auth.Err != nil {
		log.Fatal(err)
	}

	j, err := jira.NewClient(nil, jURL.String())
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return
	}
	res, err := j.Authentication.AcquireSessionCookie(auth.User, auth.Pass)
	if err != nil || res == false {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return
	}

	ui, err := New(strings.TrimSuffix(jURL.Host, ".atlassian.net"), j)
	if err != nil {
		log.Fatal(err)
	}
	if flag.NArg() < 2 {
		ui.look("my-issues")
	} else {
		ui.look(strings.Join(flag.Args()[1:], " "))
	}
	go ui.plumber()

	select {
	case <-ui.exited:
	case <-sig:
	}
	debug("bye")
}

func wrap(t, prefix string) string {
	raw := false
	out := ""
	t = strings.TrimSpace(strings.Replace(t, "\r\n", "\n", -1))
	max := 100
	lines := strings.Split(t, "\n")
	for i, line := range lines {
		if i > 0 {
			out += "\n" + prefix
		}
		// try to handle code blocks nicely
		if strings.HasPrefix(line, "{code") || strings.HasSuffix(line, "{code}\n") {
			raw = !raw
		}
		if raw {
			out += line
			continue
		}
		s := line
		for len(s) > max {
			i := strings.LastIndex(s[:max], " ")
			if i < 0 {
				i = max - 1
			}
			out += s[:i] + "\n" + prefix
			i++ // skip the space
			s = s[i:]
		}
		out += s
	}
	return out
}
