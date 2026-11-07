package updater

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfirmRun(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  bool
	}{
		{"\r", true}, {"Y\r", true}, {"yes\r", true},
		{"n\r", false}, {"NO\r", false}, {"\x1b", false},
		{"invalid\rn\r", false},
	} {
		t.Run(fmt.Sprintf("%q", tc.input), func(t *testing.T) {
			var output bytes.Buffer
			m := &menuUI{ctx: context.Background(), input: strings.NewReader(tc.input), output: &output}
			got, err := m.confirmRun()
			if err != nil || got != tc.want || !strings.Contains(output.String(), "[Y/n]") {
				t.Fatalf("confirmation: %v, %v, %q", got, err, output.String())
			}
		})
	}
	for _, input := range []string{"", "\x03"} {
		m := &menuUI{ctx: context.Background(), input: strings.NewReader(input), output: &bytes.Buffer{}}
		if approved, err := m.confirmRun(); approved || err == nil {
			t.Fatalf("interrupted confirmation: %v, %v", approved, err)
		}
	}
}

func TestRunSelectedConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		approve, prompt, runs bool
	}{
		{"unchanged yes", true, true, true},
		{"unchanged no", false, true, false},
		{"changed", false, false, true},
		{"clone", false, false, true},
		{"dirty", true, false, false},
		{"pull failure", true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "project")
			if tc.name != "clone" {
				if err := os.MkdirAll(filepath.Join(target, ".git"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(dir, "config.json")
			if err := os.WriteFile(path, []byte(`{"items":[{"repo":"repo","target":"project","run":"echo ran > ran","retry":0}]}`), 0600); err != nil {
				t.Fatal(err)
			}
			for name, script := range map[string]string{
				"git": `#!/bin/sh
case "$1" in
clone) mkdir -p "$4/.git";;
remote) echo repo;;
status) if [ "$SCENARIO" = dirty ]; then echo modified; fi;;
rev-parse) if [ "$SCENARIO" = changed ] && [ -f .git/pulled ]; then echo after; else echo before; fi;;
pull) if [ "$SCENARIO" = 'pull failure' ]; then exit 1; fi; touch .git/pulled;;
esac
`,
				"docker": `#!/bin/sh
case "$*" in
"compose config --services") echo web;;
"compose ps --all --format json") echo '{"Service":"web","State":"running","Health":"healthy"}';;
esac
`,
			} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("TELEGRAM_ENABLED", "false")
			t.Setenv("SCENARIO", tc.name)
			calls := 0
			err := runSelectedWithConfirmation(context.Background(), path, target, func() (bool, error) {
				calls++
				return tc.approve, nil
			})
			if (err != nil) != (tc.name == "pull failure") {
				t.Fatalf("run: %v", err)
			}
			if (calls == 1) != tc.prompt || calls > 1 {
				t.Fatalf("confirmation calls: %d", calls)
			}
			_, err = os.Stat(filepath.Join(target, "ran"))
			if tc.runs && err != nil || !tc.runs && !os.IsNotExist(err) {
				t.Fatalf("runs=%v, stat: %v", tc.runs, err)
			}
		})
	}
}
