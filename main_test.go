package main

import (
	"os"
	"path/filepath"
	"testing"
)

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
