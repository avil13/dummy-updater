package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAddItem(t *testing.T) {
	for _, retry := range []string{"", "0", "4"} {
		t.Run("retry="+retry, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			for i := 0; i < 2; i++ {
				target := []string{"project one", "project two"}[i]
				input := "git@example.org:repo.git\n" + target + "\nprintf '%s' \"hello\"\n" + retry + "\n"
				if err := addItem(path, "", strings.NewReader(input), io.Discard); err != nil {
					t.Fatal(err)
				}
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var c config
			if err := json.Unmarshal(data, &c); err != nil {
				t.Fatal(err)
			}
			if len(c.Items) != 2 || c.Items[0].Target != "project one" || c.Items[1].Target != "project two" || c.Items[1].Run != `printf '%s' "hello"` {
				t.Fatalf("unexpected config: %+v", c)
			}
			if retry == "" && (c.Items[1].Retry == nil || *c.Items[1].Retry != 3) {
				t.Fatal("expected default retry")
			}
			if retry != "" && (c.Items[1].Retry == nil || strconv.Itoa(*c.Items[1].Retry) != retry) {
				t.Fatal("expected explicit retry")
			}
			if _, err := readConfig(path); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatalf("expected private config, info=%v, err=%v", info, err)
			}
		})
	}
}

func TestAddItemDefaultRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	var output strings.Builder
	if err := addItem(path, "", strings.NewReader("repo\nproject\n\n\n"), &output); err != nil {
		t.Fatal(err)
	}
	c, err := readConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	const want = "docker compose up -d --build --remove-orphans"
	if c.Items[0].Run != want {
		t.Fatalf("expected default run %q, got %q", want, c.Items[0].Run)
	}
	for _, text := range []string{"repository URL", "project directory", "run — command", "additional attempts", "Project added to " + path} {
		if !strings.Contains(output.String(), text) {
			t.Fatalf("missing English message %q in %q", text, output.String())
		}
	}
	if strings.Contains(output.String(), "\x1b[") {
		t.Fatal("redirected output must not contain ANSI colors")
	}
	if !strings.Contains(output.String(), "["+want+"]") {
		t.Fatalf("default command missing from prompt: %q", output.String())
	}
}

func TestAddItemDoesNotOverwriteOnError(t *testing.T) {
	original := `{"items":[{"repo":"repo","target":"project","run":"true"}]}`
	for _, tc := range []struct {
		name, data, input string
		locked            bool
	}{
		{"duplicate target", original, "other\n./project\ntrue\n\n", false},
		{"missing repo", original, "\nnew\ntrue\n\n", false},
		{"missing target", original, "repo\n\ntrue\n\n", false},
		{"blank run", original, "repo\nnew\n \t\n\n", false},
		{"negative retry", original, "repo\nnew\ntrue\n-1\n", false},
		{"invalid retry", original, "repo\nnew\ntrue\n1.5\n", false},
		{"interrupted", original, "repo\nnew\n", false},
		{"invalid config", `{"items":`, "repo\nnew\ntrue\n\n", false},
		{"unknown field", `{"items":[],"typo":true}`, "repo\nnew\ntrue\n\n", false},
		{"locked", original, "repo\nnew\ntrue\n\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			if tc.locked {
				lock, err := acquireConfigLock(path)
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Close()
			}
			if err := addItem(path, "", strings.NewReader(tc.input), io.Discard); err == nil {
				t.Fatal("expected error")
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != tc.data {
				t.Fatalf("config changed: %q, err=%v", data, err)
			}
		})
	}
}
