package main

import (
	"bufio"
	"log"

	"9fans.net/go/plumb"
)

func (ui *UI) plumber() {
	if *noPlumber {
		return
	}
	msg := make(chan *plumb.Message)
	port, err := plumb.Open("jira", 0) // 0 is OREAD
	if err != nil {
		log.Println(err)
		return
	}
	go func() {
		defer func() { msg <- nil }()

		r := bufio.NewReader(port)
		for {
			m := &plumb.Message{}
			if err := m.Recv(r); err != nil {
				log.Println(err)
				return
			}
			msg <- m
		}
	}()
	var m *plumb.Message
	for {
		select {
		case <-ui.exited:
			return
		case m = <-msg:
			if m == nil {
				return
			}
		}
		switch m.Type {
		case "text":
			ui.look(string(m.Data))
		case "exit":
			debug("told to leave via plumber")
			ui.leave()
			return
		}
	}
}
