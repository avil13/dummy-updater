package updater

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStopItem(t *testing.T) {
	for _, tc := range []struct {
		name, input                               string
		fail, canceled, locked, called, wantError bool
	}{
		{name: "selected project", input: "2\n", called: true},
		{name: "docker failure", input: "2\n", fail: true, called: true, wantError: true},
		{name: "cancel", input: "\n"},
		{name: "invalid", input: "1 extra\n", wantError: true},
		{name: "out of range", input: "3\n", wantError: true},
		{name: "zero", input: "0\n", wantError: true},
		{name: "EOF", input: "2", wantError: true},
		{name: "context canceled", input: "2\n", canceled: true, wantError: true},
		{name: "locked", input: "2\n", locked: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.json")
			for _, target := range []string{"one", "project two"} {
				if err := os.Mkdir(filepath.Join(dir, target), 0700); err != nil {
					t.Fatal(err)
				}
			}
			config := `{"items":[{"repo":"one","target":"one","run":"exit 99"},{"repo":"two","target":"project two","run":"exit 99"}]}`
			if err := os.WriteFile(path, []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
			script := "#!/bin/sh\nprintf '%s\\n' \"$PWD\" \"$*\" >> \"$CALLS\"\n"
			if tc.fail {
				script += "exit 7\n"
			}
			if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			calls := filepath.Join(dir, "calls")
			t.Setenv("CALLS", calls)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.canceled {
				cancel()
			}
			if tc.locked {
				lock, err := acquireConfigLock(path)
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := lock.Close(); err != nil {
						t.Error(err)
					}
				}()
			}
			var output bytes.Buffer
			err := stopItem(ctx, path, strings.NewReader(tc.input), &output)
			if (err != nil) != tc.wantError {
				t.Fatalf("unexpected error: %v", err)
			}
			data, err := os.ReadFile(calls)
			if tc.called {
				target, resolveErr := filepath.EvalSymlinks(filepath.Join(dir, "project two"))
				if resolveErr != nil {
					t.Fatal(resolveErr)
				}
				want := target + "\ncompose stop\n"
				if err != nil || string(data) != want {
					t.Fatalf("calls: %q, %v", data, err)
				}
				if !strings.Contains(output.String(), "1) "+filepath.Join(dir, "one")) || !strings.Contains(output.String(), "2) "+filepath.Join(dir, "project two")) {
					t.Fatalf("missing list: %s", &output)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("unexpected Docker call: %q, %v", data, err)
			}
			logged, _ := os.ReadFile(filepath.Join(dir, "error.log"))
			if tc.fail {
				if !strings.Contains(string(logged), "ERROR: stop: exit status 7") {
					t.Fatalf("missing error log: %q", logged)
				}
			} else if len(logged) != 0 {
				t.Fatalf("unexpected log: %q", logged)
			}
		})
	}
}
