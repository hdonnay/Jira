package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/signal"
	"strings"

	jira "github.com/andygrunwald/go-jira"
	"golang.org/x/net/publicsuffix"
)

var (
	authStr     = flag.String("a", "", "`username:personal_access_token` combination")
	debugEnable = flag.Bool("D", false, "enable debug output")
	noPlumber   = flag.Bool("p", false, "disable plumber integration and don't linger")
	wrapWidth   = flag.Int("w", 80, "set wrap width")

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
	fmt.Fprintf(os.Stderr, "If a window name is supplied, it will be opened instead of \"my-issues\".\n")
	fmt.Fprintf(os.Stderr, "Some special names include:\n\n")
	fmt.Fprintf(os.Stderr, "\t- my-issues\n")
	fmt.Fprintf(os.Stderr, "\t- search\n")
	fmt.Fprintf(os.Stderr, "\t- filters\n")
	fmt.Fprintf(os.Stderr, "\n")
}

func init() {
	flag.Usage = usage
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

func main() {
	var auth struct {
		Err  error
		User string
		Pass string
	}
	var err error
	ctx, done := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer done()
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
	auth.User, auth.Pass, auth.Err = secretsOS(ctx, jURL.Host)
	if auth.Err != nil {
		debug("OS secret error: %v", auth.Err)
		auth.User, auth.Pass, auth.Err = secretsFile()
	}
	if *authStr != "" {
		var ok bool
		auth.User, auth.Pass, ok = strings.Cut(*authStr, ":")
		if !ok {
			log.Fatal(fmt.Errorf("unable to make sense of supplied username:password"))
		}
	}

	pat := jira.PATAuthTransport{
		Token: auth.Pass,
	}
	c := pat.Client()
	c.Jar, err = cookiejar.New(&cookiejar.Options{
		PublicSuffixList: publicsuffix.List,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return
	}
	j, err := jira.NewClient(c, jURL.String())
	if err != nil {
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
	case <-ctx.Done():
	}
	debug("bye")
}
