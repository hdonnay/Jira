package main

import (
	"bytes"
	"io/ioutil"
	"os"
)

func secretsFile() (string, string, error) {
	fn := os.ExpandEnv("${HOME}/.jira-creds")
	b, err := ioutil.ReadFile(fn)
	if err != nil {
		return "", "", err
	}

	upw := bytes.SplitN(bytes.TrimSpace(b), []byte(":"), 2)
	return string(upw[0]), string(upw[1]), nil
}