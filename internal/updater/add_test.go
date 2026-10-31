package updater

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
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
				input := "git@example.org:repo.git\n" + target + "\nprintf '%s' \"hello\"\n" + retry + "\n\n"
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
			if c.Items[1].Enabled == nil || !*c.Items[1].Enabled {
				t.Fatal("expected enabled: true for new item")
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

func TestAddItemConfigDefaults(t *testing.T) {
	for _, tc := range []struct {
		name, settings, wantLog, wantTG string
	}{
		{"new config", "", `{"max_err_mb":1,"max_project_mb":0.5,"retention_days":7,"free_size":"10%"}`, `{}`},
		{"missing settings", `{}`, `{"max_err_mb":1,"max_project_mb":0.5,"retention_days":7,"free_size":"10%"}`, `{}`},
		{"partial settings", `{"log":{"max_err_mb":0},"tg":{"only_errors":true}}`, `{"max_err_mb":0,"max_project_mb":0.5,"retention_days":7,"free_size":"10%"}`, `{"only_errors":true}`},
		{"null settings", `{"log":null,"tg":null}`, `{"max_err_mb":1,"max_project_mb":0.5,"retention_days":7,"free_size":"10%"}`, `{}`},
		{"custom settings", `{"$schema":"custom","log":{"max_err_mb":2,"max_project_mb":0,"retention_days":0,"free_size":"0%"},"tg":{"enabled":true,"only_errors":true,"bot_token":"test-token","chat_id":"123"}}`, `{"max_err_mb":2,"max_project_mb":0,"retention_days":0,"free_size":"0%"}`, `{"enabled":true,"only_errors":true,"bot_token":"test-token","chat_id":"123"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			var original config
			if tc.settings != "" {
				if err := json.Unmarshal([]byte(tc.settings), &original); err != nil {
					t.Fatal(err)
				}
				original.Items = []item{{Repo: "old", Target: "old", Run: "true"}}
				data, err := json.Marshal(original)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := addItem(path, "", strings.NewReader("repo\nnew\n\n\n\n"), io.Discard); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var got, want config
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(`{"log":`+tc.wantLog+`,"tg":`+tc.wantTG+`}`), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Log, want.Log) || !reflect.DeepEqual(got.TG, want.TG) {
				t.Fatalf("unexpected defaults: %s", data)
			}
			if original.Schema != "" && got.Schema != original.Schema || got.Schema == "" {
				t.Fatalf("unexpected schema: %q", got.Schema)
			}
			if !reflect.DeepEqual(got.Items[:len(original.Items)], original.Items) && len(original.Items) > 0 {
				t.Fatal("existing items changed")
			}
		})
	}
}

func TestAddItemDefaultRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	var output strings.Builder
	if err := addItem(path, "", strings.NewReader("repo\nproject\n\n\n\n"), &output); err != nil {
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

func TestAddItemEnabled(t *testing.T) {
	for _, value := range []string{"", "true", "false"} {
		t.Run("enabled="+value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			var output strings.Builder
			if err := addItem(path, "", strings.NewReader("repo\nproject\n\n\n"+value+"\n"), &output); err != nil {
				t.Fatal(err)
			}
			c, err := readConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			if c.Items[0].Enabled == nil || *c.Items[0].Enabled != (value != "false") {
				t.Fatalf("unexpected enabled: %v", c.Items[0].Enabled)
			}
			if !strings.Contains(output.String(), "enabled — enable project [true]") {
				t.Fatalf("missing enabled prompt: %s", &output)
			}
		})
	}
}

func TestAddItemDoesNotOverwriteOnError(t *testing.T) {
	original := `{"items":[{"repo":"repo","target":"project","run":"true"}]}`
	for _, tc := range []struct {
		name, data, input string
		locked            bool
	}{
		{"duplicate target", original, "other\n./project\ntrue\n\n\n", false},
		{"missing repo", original, "\nnew\ntrue\n\n\n", false},
		{"missing target", original, "repo\n\ntrue\n\n\n", false},
		{"blank run", original, "repo\nnew\n \t\n\n\n", false},
		{"negative retry", original, "repo\nnew\ntrue\n-1\n", false},
		{"invalid retry", original, "repo\nnew\ntrue\n1.5\n", false},
		{"interrupted", original, "repo\nnew\n", false},
		{"invalid config", `{"items":`, "repo\nnew\ntrue\n\n\n", false},
		{"unknown field", `{"items":[],"typo":true}`, "repo\nnew\ntrue\n\n\n", false},
		{"locked", original, "repo\nnew\ntrue\n\n\n", true},
		{"invalid enabled", original, "repo\nnew\ntrue\n\nyes\n", false},
		{"interrupted enabled", original, "repo\nnew\ntrue\n\n", false},
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
				defer func() {
					if err := lock.Close(); err != nil {
						t.Error(err)
					}
				}()
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
