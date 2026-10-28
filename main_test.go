package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunLogsOnlyErrors(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"git", "docker"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\necho command-output\nexit 1\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	path := filepath.Join(dir, "update-items.json")
	if err := os.WriteFile(path, []byte(`{"items":[{"repo":"repo","target":"project"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), path); err == nil {
		t.Fatal("expected clone failure")
	}
	data, err := os.ReadFile(filepath.Join(dir, "error.log"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "ERROR: clone:") {
		t.Fatalf("expected only the clone error in log, got %q", data)
	}
}

func TestReadConfig(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		valid      bool
	}{
		{"valid", `{"items":[{"repo":"git@example:repo.git","target":"project one"},{"repo":"https://example.org/two.git","target":"project two"}]}`, true},
		{"empty", `{"items":[]}`, false},
		{"missing repo", `{"items":[{"target":"project"}]}`, false},
		{"missing target", `{"items":[{"repo":"repo"}]}`, false},
		{"typo", `{"items":[{"repo:":"repo","target":"project"}]}`, false},
		{"duplicate", `{"items":[{"repo":"a","target":"project"},{"repo":"b","target":"./project"}]}`, false},
		{"trailing JSON", `{"items":[{"repo":"a","target":"project"}]} {}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "update-items.json")
			if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			c, err := readConfig(path)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, err=%v", tc.valid, err)
			}
			if tc.valid && c.Items[0].Target != filepath.Join(dir, "project one") {
				t.Fatalf("unexpected target: %s", c.Items[0].Target)
			}
		})
	}
}
