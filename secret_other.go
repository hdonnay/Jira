//go:build !linux
// +build !linux

package main

import "fmt"

func secretsOS(_ string) (string, string, error) {
	return "", "", fmt.Errorf("OS secret retrival unsupported here")
}
