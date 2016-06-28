// +build linux

package main

import (
	"fmt"
	"strings"

	ss "github.com/hdonnay/secretservice"
)

func secretsOS(name string) (string, string, error) {
	srv, err := ss.DialService()
	if err != nil {
		return "", "", err
	}
	session, err := srv.OpenSession(ss.AlgoPlain)
	if err != nil {
		return "", "", err
	}
	defer session.Close()
	for _, c := range srv.Collections() {
		for _, i := range c.Items() {
			if i.GetLabel() == name {
				if i.Locked() {
					// TODO: unlock
					return "", "", err
				}
				s, err := i.GetSecret(session)
				if err != nil {
					return "", "", err
				}
				pass, err := s.GetValue(session)
				if err != nil {
					return "", "", err
				}
				upw := strings.SplitN(string(pass), ":", 2)
				return upw[0], upw[1], nil
			}
		}
	}
	return "", "", fmt.Errorf("secret %q not found", name)
}