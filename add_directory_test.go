package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddItemDirectory(t *testing.T) {
	for _, tc := range []struct {
		name, input           string
		edit, noGit, noOrigin bool
	}{
		{name: "defaults", input: "\n\n\n\n"},
		{name: "editable", input: "other-repo\nother-target\ncustom-run\n0\n", edit: true},
		{name: "not a repository", noGit: true},
		{name: "missing origin", noOrigin: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "local repo")
			if err := os.MkdirAll(target, 0700); err != nil {
				t.Fatal(err)
			}
			if !tc.noGit {
				if err := os.Mkdir(filepath.Join(target, ".git"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			script := "#!/bin/sh\n[ \"$*\" = 'remote get-url origin' ] || exit 9\necho git@example.org:repo.git\n"
			if tc.noOrigin {
				script = "#!/bin/sh\nexit 2\n"
			}
			if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			relative, err := filepath.Rel(cwd, target)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "config.json")
			var output strings.Builder
			err = addItem(path, relative, strings.NewReader(tc.input), &output)
			if tc.noGit || tc.noOrigin {
				if err == nil {
					t.Fatal("expected error")
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("config must not be created")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var c config
			if err := json.Unmarshal(data, &c); err != nil {
				t.Fatal(err)
			}
			entry := c.Items[0]
			if tc.edit {
				if entry.Repo != "other-repo" || entry.Target != "other-target" || entry.Run != "custom-run" || entry.Retry == nil || *entry.Retry != 0 {
					t.Fatalf("overrides not saved: %+v", entry)
				}
			} else if entry.Repo != "git@example.org:repo.git" || entry.Target != target || entry.Retry == nil || *entry.Retry != 3 {
				t.Fatalf("defaults not saved: %+v", entry)
			}
			for _, want := range []string{"[" + target + "]", "[git@example.org:repo.git]", "[3]"} {
				if !strings.Contains(output.String(), want) {
					t.Fatalf("missing default %q in %s", want, output.String())
				}
			}
		})
	}
}
