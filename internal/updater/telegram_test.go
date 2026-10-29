package updater

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type telegramTransport func(*http.Request) (*http.Response, error)

func (f telegramTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTelegramConfig(t *testing.T) {
	for _, tc := range []struct {
		tg    string
		valid bool
	}{
		{`{}`, true}, {`{"enabled":true}`, false},
		{`{"enabled":true,"bot_token":"secret","chat_id":"-123"}`, true},
		{`{"enabled":true,"bot_token":" ","chat_id":"-123"}`, false},
		{`{"unknown":true}`, false},
		{`{"enabled":true,"bot_token":"secret","chat_id":"@alerts","only_errors":true}`, true},
		{`{"only_errors":"true"}`, false},
	} {
		_, err := validateConfig([]byte(`{"tg":`+tc.tg+`,"items":[{"repo":"repo","target":"project","run":"true"}]}`), filepath.Join(t.TempDir(), "config.json"))
		if (err == nil) != tc.valid {
			t.Fatalf("tg=%s: %v", tc.tg, err)
		}
	}
}

func TestTelegramSummary(t *testing.T) {
	dir := t.TempDir()
	for name, script := range map[string]string{
		"git":    "#!/bin/sh\ncase \"$1\" in\nremote) echo repo;;\nstatus) case \"$PWD\" in *dirty) echo modified;; esac;;\nrev-parse) echo commit;;\nesac\n",
		"docker": "#!/bin/sh\nexit 0\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	disabled := false
	zero := 0
	c := config{TG: &telegramConfig{Enabled: true, BotToken: "secret", ChatID: "-1001234567890"}}
	for _, name := range []string{"success", "dirty", "failure", "disabled"} {
		target := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Join(target, ".git"), 0700); err != nil {
			t.Fatal(err)
		}
		entry := item{Repo: "repo", Target: target, Run: "true", Retry: &zero}
		if name == "failure" {
			entry.Run = "exit 1"
		}
		if name == "disabled" {
			entry.Enabled = &disabled
		}
		c.Items = append(c.Items, entry)
	}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	calls := 0
	http.DefaultTransport = telegramTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/botsecret/sendMessage" {
			t.Fatalf("unexpected endpoint")
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("chat_id") != c.TG.ChatID {
			t.Fatal("wrong chat")
		}
		if r.Method != http.MethodPost || r.Form.Get("disable_web_page_preview") != "true" {
			t.Fatal("expected POST with previews disabled")
		}
		message := r.Form.Get("text")
		expected := []string{"без новых коммитов, run выполнен", "локальные изменения", "ошибка", "отключён"}
		if c.TG.OnlyErrors {
			expected = []string{"failure", "ошибка"}
			for _, name := range []string{"success", "dirty", "disabled"} {
				if strings.Contains(message, filepath.Join(dir, name)) {
					t.Errorf("non-error project in report: %s", message)
				}
			}
		}
		for _, status := range expected {
			if !strings.Contains(message, status) {
				t.Errorf("missing %q in %q", status, message)
			}
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Header: make(http.Header)}, nil
	})
	if err := run(context.Background(), path); err == nil {
		t.Fatal("expected project failure")
	}
	if calls != 1 {
		t.Fatalf("expected one report, got %d", calls)
	}
	c.TG.OnlyErrors = true
	c.TG.ChatID = "@alerts"
	save := func() {
		t.Helper()
		data, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	save()
	if err := run(context.Background(), path); err == nil {
		t.Fatal("expected project failure")
	}
	if calls != 2 {
		t.Fatalf("expected error report, got %d calls", calls)
	}
	c.Items[2].Run = "true"
	save()
	if err := run(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("successful run must not send an error report")
	}
	c.TG.Enabled = false
	c.Items[2].Run = "exit 1"
	save()
	if err := run(context.Background(), path); err == nil {
		t.Fatal("expected project failure")
	}
	if calls != 2 {
		t.Fatal("disabled notifications must not send a report")
	}
}

func TestTelegramDoesNotLogToken(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = telegramTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("secret") })
	var output strings.Builder
	notify(context.Background(), telegramConfig{Enabled: true, BotToken: "secret", ChatID: "123"}, "report", log.New(&output, "", 0))
	if output.Len() == 0 || strings.Contains(output.String(), "secret") {
		t.Fatalf("unsafe log: %s", output.String())
	}
}
