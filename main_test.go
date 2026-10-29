package main

import (
	"context"
	"io"
	"log"
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
	if err := os.WriteFile(path, []byte(`{"items":[{"repo":"repo","target":"project","run":"docker compose up -d"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	console, err := os.CreateTemp(dir, "stderr-*")
	if err != nil {
		t.Fatal(err)
	}
	stderr := os.Stderr
	os.Stderr = console
	t.Cleanup(func() {
		os.Stderr = stderr
		console.Close()
	})
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
	consoleData, err := os.ReadFile(console.Name())
	if err != nil {
		t.Fatal(err)
	}
	if string(consoleData) != string(data) || !strings.Contains(string(consoleData), "["+filepath.Join(dir, "project")+"]") {
		t.Fatalf("expected the same error with project path in stderr, got %q", consoleData)
	}
}

func TestUpdateRun(t *testing.T) {
	dir := t.TempDir()
	git := "#!/bin/sh\ncase \"$1\" in\nremote) echo repo;;\nrev-parse) echo commit;;\nesac\n"
	for name, script := range map[string]string{"git": git, "docker": "#!/bin/sh\nexit 0\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	target := filepath.Join(dir, "project one")
	if err := os.MkdirAll(filepath.Join(target, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	entry := item{Repo: "repo", Target: target, Run: `printf '%s\n' 'custom command' >> 'run output'`}
	logger := log.New(io.Discard, "", 0)
	for i := 0; i < 2; i++ {
		if err := update(context.Background(), entry, io.Discard, logger); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(filepath.Join(target, "run output"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "custom command\ncustom command\n" {
		t.Fatalf("unexpected run output: %q", data)
	}
	entry.Run = "exit 7"
	if err := update(context.Background(), entry, io.Discard, logger); err == nil || !strings.Contains(err.Error(), "run: exit status 7") {
		t.Fatalf("expected run failure, got %v", err)
	}
}

func TestValidateConfigRepoWhitespace(t *testing.T) {
	for _, repo := range []string{
		` https://example.org/repo.git`,
		`https://example.org/repo.git `,
		`\thttps://example.org/repo.git`,
		`https://example.org/repo.git\n`,
	} {
		t.Run(repo, func(t *testing.T) {
			data := []byte(`{"items":[{"repo":"` + repo + `","target":"project","run":"true"}]}`)
			_, err := validateConfig(data, filepath.Join(t.TempDir(), "update-items.json"))
			if err == nil || err.Error() != "items[0].repo: leading or trailing whitespace is not allowed" {
				t.Fatalf("expected repo whitespace validation error, got %v", err)
			}
		})
	}
}

func TestReadConfig(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		valid      bool
	}{
		{"valid", `{"items":[{"repo":"git@example:repo.git","target":"project one","run":"docker compose up -d"},{"repo":"https://example.org/two.git","target":"project two","run":"docker compose up -d --build"}]}`, true},
		{"empty", `{"items":[]}`, false},
		{"missing repo", `{"items":[{"target":"project","run":"true"}]}`, false},
		{"missing target", `{"items":[{"repo":"repo","run":"true"}]}`, false},
		{"missing run", `{"items":[{"repo":"repo","target":"project"}]}`, false},
		{"empty run", `{"items":[{"repo":"repo","target":"project","run":""}]}`, false},
		{"blank run", `{"items":[{"repo":"repo","target":"project","run":" \t\n"}]}`, false},
		{"null run", `{"items":[{"repo":"repo","target":"project","run":null}]}`, false},
		{"invalid run type", `{"items":[{"repo":"repo","target":"project","run":true}]}`, false},
		{"typo", `{"items":[{"repo:":"repo","target":"project","run":"true"}]}`, false},
		{"duplicate", `{"items":[{"repo":"a","target":"project","run":"true"},{"repo":"b","target":"./project","run":"true"}]}`, false},
		{"trailing JSON", `{"items":[{"repo":"a","target":"project","run":"true"}]} {}`, false},
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
			if tc.valid && c.Items[0].Run != "docker compose up -d" {
				t.Fatalf("unexpected run: %s", c.Items[0].Run)
			}
		})
	}
}
