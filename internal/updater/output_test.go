package updater

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunProjectTranscripts(t *testing.T) {
	for _, tc := range []struct {
		name, setting string
		fail          bool
	}{
		{"success", "", false},
		{"inspection failure", "", true},
		{"custom limit", `"log":{"max_project_mb":0.001},`, true},
		{"disabled", `"log":{"max_project_mb":0,"max_err_mb":0},`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fail := tc.fail
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, "project", ".git"), 0700); err != nil {
				t.Fatal(err)
			}
			git := "#!/bin/sh\ncase \"$1\" in\nremote) echo " + strings.Repeat("x", 2000) + " >&2; echo inspection-stderr >&2; "
			if fail {
				git += "exit 1"
			} else {
				git += "echo repo"
			}
			git += ";;\nrev-parse) echo commit;;\nesac\n"
			for name, script := range map[string]string{"git": git, "docker": "#!/bin/sh\nexit 0\n"} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("TELEGRAM_ENABLED", "false")
			path := filepath.Join(dir, "config.json")
			if err := os.WriteFile(path, []byte(`{`+tc.setting+`"items":[{"repo":"repo","target":"project","run":"echo deployment-output"}]}`), 0600); err != nil {
				t.Fatal(err)
			}
			console, err := os.CreateTemp(dir, "console-*")
			if err != nil {
				t.Fatal(err)
			}
			stdout, stderr := os.Stdout, os.Stderr
			os.Stdout, os.Stderr = console, console
			t.Cleanup(func() {
				os.Stdout, os.Stderr = stdout, stderr
				if err := console.Close(); err != nil {
					t.Error(err)
				}
			})
			if err := run(context.Background(), path); (err != nil) != fail {
				t.Fatalf("run: %v", err)
			}
			info, err := os.Stat(filepath.Join(dir, "logs"))
			if err != nil || info.Mode().Perm() != 0700 {
				t.Fatalf("expected private logs directory: %v, err=%v", info, err)
			}
			logs, err := filepath.Glob(filepath.Join(dir, "logs", "project-output-*.log"))
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "disabled" {
				if _, err := os.Stat(filepath.Join(dir, "logs", "error.log")); !os.IsNotExist(err) || len(logs) != 0 {
					t.Fatalf("disabled logs: files=%v, stat error=%v", logs, err)
				}
				data, err := os.ReadFile(console.Name())
				if err != nil || !strings.Contains(string(data), "ERROR:") {
					t.Fatalf("missing stderr error with logs disabled: %q, %v", data, err)
				}
			} else if fail {
				if len(logs) != 1 {
					t.Fatalf("missing transcript: %v", logs)
				}
				data, err := os.ReadFile(logs[0])
				if err != nil || !strings.Contains(string(data), "inspection-stderr") {
					t.Fatalf("missing Git stderr: %q, %v", data, err)
				}
				if tc.name == "custom limit" && len(data) != 1000 {
					t.Fatalf("expected 1000-byte transcript, got %d", len(data))
				}
				info, err := os.Stat(logs[0])
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatalf("transcript permissions: %v, %v", info, err)
				}
			} else {
				if len(logs) != 0 {
					t.Fatalf("successful output was not removed: %v", logs)
				}
				data, err := os.ReadFile(console.Name())
				if err != nil || !strings.Contains(string(data), "✅ ["+filepath.Join(dir, "project")+"]") {
					t.Fatalf("missing success summary: %q, %v", data, err)
				}
			}
		})
	}
}
