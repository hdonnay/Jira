//go:build linux
// +build linux

package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func secretsOS(ctx context.Context, name string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "secret-tool", "lookup", "app_id", "io.github.hdonnay.Jira", "host", name)
	out, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("secret %q not found: %w", name, err)
	}
	u, p, ok := strings.Cut(string(out), ":")
	if !ok {
		return "", "", fmt.Errorf("secret %q not found: bad format", name)
	}
	return u, p, nil
}
