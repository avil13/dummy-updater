package updater

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditItem(t *testing.T) {
	original := []byte(`{"$schema":"schema","tg":{"enabled":false,"bot_token":"secret"},"items":[{"repo":"one","target":"one","run":"true"},{"repo":"two","target":"two","run":"true"}]}`)
	for _, scenario := range []string{"save", "duplicate", "invalid retry", "invalid enabled", "cancel", "changed", "locked"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			answers := []string{"new-repo", "new-target", "echo changed", "0", "false"}
			switch scenario {
			case "duplicate":
				answers[1] = "./two"
			case "invalid retry":
				answers[3] = "-1"
			case "invalid enabled":
				answers[4] = "yes"
			case "locked":
				lock, err := acquireConfigLock(path)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = lock.Close() }()
			}
			wantUnchanged := original
			i := 0
			err := editItem(path, original, 0, func(string) (string, error) {
				if scenario == "cancel" {
					return "", errEditCanceled
				}
				if scenario == "changed" && i == 0 {
					wantUnchanged = append(append([]byte(nil), original...), '\n')
					if err := os.WriteFile(path, wantUnchanged, 0600); err != nil {
						t.Fatal(err)
					}
				}
				answer := answers[i]
				i++
				return answer, nil
			})
			if (err == nil) != (scenario == "save") {
				t.Fatalf("edit: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if scenario != "save" {
				if !bytes.Equal(got, wantUnchanged) {
					t.Fatal("failed edit modified config")
				}
				return
			}
			c, err := validateConfig(got, path)
			if err != nil || c.Items[0].Repo != "new-repo" || *c.Items[0].Retry != 0 || *c.Items[0].Enabled || c.TG.BotToken != "secret" || c.Schema != "schema" || !bytes.Contains(got, []byte(`"target": "two"`)) {
				t.Fatalf("saved config: %s, %v", got, err)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatalf("permissions: %v, %v", info, err)
			}
		})
	}
}

func TestBrowseItems(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"items":[{"repo":"one","target":"one","run":"true"},{"repo":"two","target":"two","run":"true"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, keys := range []string{"q", "\x1b", "\x1b[B\rj\r\rqq", "\r\r\r\r\r\r\r\rq"} {
		var output bytes.Buffer
		m := &menuUI{ctx: context.Background(), input: strings.NewReader(keys), output: &output}
		calls := 0
		err := browseItems(m, path, func(target string) error {
			calls++
			if target != filepath.Join(filepath.Dir(path), "two") {
				t.Fatalf("wrong project: %s", target)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if (calls == 1) != strings.Contains(keys, "[B") {
			t.Fatalf("unexpected calls: %d", calls)
		}
	}
	m := &menuUI{ctx: context.Background(), input: strings.NewReader("éq\x7f!\r"), output: io.Discard}
	if got, err := m.ask("value: "); err != nil || got != "é!" {
		t.Fatalf("UTF-8 edit: %q, %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.ctx = ctx
	if _, err := m.key(); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestProjectLogs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	target := filepath.Join(dir, "one")
	logs := filepath.Join(dir, "logs")
	if err := os.Mkdir(logs, 0700); err != nil {
		t.Fatal(err)
	}
	own := projectLogPrefix(target) + "123.log"
	other := projectLogPrefix(target+"-other") + "123.log"
	legacy := "project-output-old.log"
	for _, name := range []string{own, other, legacy} {
		if err := os.WriteFile(filepath.Join(logs, name), []byte("truncated tail without header"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(logs, other), filepath.Join(logs, projectLogPrefix(target)+"link.log")); err != nil {
		t.Fatal(err)
	}
	text := "[" + target + "] 2026/01/01 ERROR: own; output: " + filepath.Join(logs, legacy) + "\n[" + target + "-other] ERROR: unrelated\n"
	if err := os.WriteFile(filepath.Join(logs, "error.log"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	got, files, err := projectLogs(path, target)
	if err != nil || strings.Contains(got, "unrelated") || !strings.Contains(got, "own") || len(files) != 2 {
		t.Fatalf("logs: %q, %v, %v", got, files, err)
	}
	for _, file := range files {
		if file != own && file != legacy {
			t.Fatalf("unrelated log: %s", file)
		}
	}
}

func TestRunSelected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := `{"items":[{"repo":"one","target":"one","run":"exit 99"},{"repo":"two","target":"two","run":"echo two >> \"$CALLS\"","retry":0}]}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "two", ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, script := range map[string]string{
		"git":    "#!/bin/sh\ncase \"$1\" in\nremote) echo two;;\nrev-parse) echo commit;;\nesac\n",
		"docker": "#!/bin/sh\nexit 0\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TELEGRAM_ENABLED", "false")
	t.Setenv("CALLS", filepath.Join(dir, "calls"))
	if err := runSelected(context.Background(), path, filepath.Join(dir, "two")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil || string(got) != "two\n" {
		t.Fatalf("run calls: %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "one")); !os.IsNotExist(err) {
		t.Fatal("unselected project touched")
	}
	if err := runSelected(context.Background(), path, filepath.Join(dir, "missing")); err == nil {
		t.Fatal("accepted stale selection")
	}
	data = strings.Replace(data, `"retry":0`, `"retry":0,"enabled":false`, 1)
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runSelected(context.Background(), path, filepath.Join(dir, "two")); err == nil {
		t.Fatal("accepted disabled project")
	}
	got, err = os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil || string(got) != "two\n" {
		t.Fatalf("disabled project ran: %q, %v", got, err)
	}
}
