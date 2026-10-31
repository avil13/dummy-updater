package updater

import (
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFreeSizeConfig(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{`null`, true}, {`"0%"`, true}, {`"10%"`, true}, {`"100%"`, true},
		{`""`, false}, {`"101%"`, false}, {`"-1%"`, false}, {`"10"`, false},
		{`"10.5%"`, false}, {`" 10%"`, false}, {`"01%"`, false}, {`10`, false},
	} {
		_, err := validateConfig([]byte(`{"log":{"free_size":`+tc.value+`},"items":[{"repo":"repo","target":"project","run":"true"}]}`), "config.json")
		if (err == nil) != tc.valid {
			t.Errorf("free_size=%s: %v", tc.value, err)
		}
	}
}

func TestLowDiskSpace(t *testing.T) {
	for _, tc := range []struct {
		available, total uint64
		threshold        int
		want             bool
	}{
		{9, 100, 10, true}, {10, 100, 10, false}, {11, 100, 10, false},
		{0, 100, 10, true}, {0, 100, 0, false}, {0, 0, 10, false},
		{1, 11, 10, true}, {1 << 60, 1 << 63, 10, false},
	} {
		if got := lowDiskSpace(tc.available, tc.total, tc.threshold); got != tc.want {
			t.Errorf("%+v: got %v", tc, got)
		}
	}
}

func TestTargetDiskStats(t *testing.T) {
	dir := t.TempDir()
	want, err := targetDiskStats(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := targetDiskStats(filepath.Join(dir, "missing", "project"))
	if err != nil || got.Fsid != want.Fsid || got.Blocks != want.Blocks {
		t.Fatalf("missing target must use existing parent filesystem: %+v, %v", got, err)
	}
	link := filepath.Join(t.TempDir(), "target-link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	got, err = targetDiskStats(link)
	if err != nil || got.Fsid != want.Fsid {
		t.Fatalf("target symlink must resolve to its filesystem: %+v, %v", got, err)
	}
}

func TestDiskSpaceWarning(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"git", "docker"} {
		script := "#!/bin/sh\ncase \"$1\" in\nremote) echo repo;;\nstatus) echo modified;;\nesac\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, name := range []string{"one", "two"} {
		if err := os.MkdirAll(filepath.Join(dir, name, ".git"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	calls := 0
	http.DefaultTransport = telegramTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if message := r.Form.Get("text"); !strings.Contains(message, "Мало свободного места") || !strings.Contains(message, filepath.Join(dir, []string{"one", "two"}[(calls-1)%2])) {
			t.Errorf("unexpected warning: %s", message)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Header: make(http.Header)}, nil
	})
	for _, tc := range []struct {
		log, enabled string
		want         int
	}{
		{`{"free_size":"100%"}`, "true", 2},
		{`{"free_size":"100%"}`, "false", 2},
		{`{"free_size":"0%"}`, "true", 2},
		{`{}`, "true", 2},
	} {
		data := `{"log":` + tc.log + `,"tg":{"enabled":` + tc.enabled + `,"only_errors":true,"bot_token":"secret","chat_id":"123"},"items":[{"repo":"repo","target":"one","run":"true"},{"repo":"repo","target":"two","run":"true"},{"repo":"repo","target":"disabled","run":"true","enabled":false}]}`
		path := filepath.Join(dir, "config.json")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if err := run(context.Background(), path); err != nil {
			t.Fatal(err)
		}
		if calls != tc.want {
			t.Fatalf("expected %d notification(s), got %d", tc.want, calls)
		}
	}
	var output strings.Builder
	if checkFreeSpace(context.Background(), filepath.Join(dir, "git", "not-a-directory"), 10, telegramConfig{}, log.New(&output, "", 0)) || !strings.Contains(output.String(), "WARNING") {
		t.Fatalf("expected stat failure warning: %s", output.String())
	}
}
