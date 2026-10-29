package updater

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogRetention(t *testing.T) {
	for _, action := range []string{"run", "stop"} {
		for _, tc := range []struct {
			value  string
			remove bool
		}{{"null", true}, {"0", false}, {"1", true}, {"30", false}, {"9223372036854775807", false}} {
			t.Run(action+"/"+tc.value, func(t *testing.T) {
				root := t.TempDir()
				path := filepath.Join(root, "config.json")
				data := `{"log":{"retention_days":` + tc.value + `},"items":[{"repo":"repo","target":"project","run":"true","enabled":false}]}`
				if err := os.WriteFile(path, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
				dir := filepath.Join(root, "logs")
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"project-output-old.log", "project-output-recent.log", "error.log", "other.log"} {
					file := filepath.Join(dir, name)
					if err := os.WriteFile(file, []byte("old output"), 0600); err != nil {
						t.Fatal(err)
					}
					modified := time.Now().Add(-8 * 24 * time.Hour)
					if name == "project-output-recent.log" {
						modified = time.Now()
					}
					if err := os.Chtimes(file, modified, modified); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Mkdir(filepath.Join(dir, "project-output-directory.log"), 0700); err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(root, "project-output-outside.log")
				if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
				old := time.Now().Add(-8 * 24 * time.Hour)
				if err := os.Chtimes(filepath.Join(dir, "project-output-directory.log"), old, old); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(outside, old, old); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(dir, "project-output-link.log")); err != nil {
					t.Fatal(err)
				}
				var err error
				if action == "run" {
					err = run(context.Background(), path)
				} else {
					err = stopItem(context.Background(), path, strings.NewReader(""), io.Discard)
				}
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(filepath.Join(dir, "project-output-old.log")); os.IsNotExist(err) != tc.remove {
					t.Fatalf("unexpected retention result: %v", err)
				}
				for _, name := range []string{"project-output-recent.log", "error.log", "other.log", "project-output-directory.log", "project-output-link.log"} {
					if _, err := os.Lstat(filepath.Join(dir, name)); err != nil {
						t.Fatalf("removed %s: %v", name, err)
					}
				}
				if _, err := os.Stat(outside); err != nil {
					t.Fatalf("removed file outside logs: %v", err)
				}
			})
		}
	}
}

func TestRetentionConfig(t *testing.T) {
	for _, value := range []string{"-1", "1.5", `"7"`, "9223372036854775808"} {
		if _, err := validateConfig([]byte(`{"log":{"retention_days":`+value+`},"items":[{"repo":"repo","target":"project","run":"true"}]}`), "config.json"); err == nil {
			t.Fatalf("accepted invalid retention_days: %s", value)
		}
	}
}

func TestErrorLogConfiguredLimit(t *testing.T) {
	for _, limit := range []int64{0, 10} {
		path := filepath.Join(t.TempDir(), "error.log")
		output, err := openErrorLog(path, limit)
		if err != nil {
			t.Fatal(err)
		}
		data := []byte("0123456789abcdef")
		if n, err := output.Write(data); err != nil || n != len(data) {
			t.Fatalf("Write: %d, %v", n, err)
		}
		if err := output.Close(); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if limit == 0 {
			if !os.IsNotExist(err) {
				t.Fatalf("disabled log created: %v", err)
			}
		} else if err != nil || string(got) != "6789abcdef" {
			t.Fatalf("unexpected log tail: %q, %v", got, err)
		}
	}
}

func TestErrorLogLimit(t *testing.T) {
	const maxErrorLogSize = 1_000_000
	for _, initialSize := range []int{0, 900_000, 1_200_000} {
		path := filepath.Join(t.TempDir(), "error.log")
		want := bytes.Repeat([]byte("old error\n"), initialSize/10)
		if err := os.WriteFile(path, want, 0600); err != nil {
			t.Fatal(err)
		}
		output, err := openErrorLog(path, maxErrorLogSize)
		if err != nil {
			t.Fatal(err)
		}
		check := func() {
			t.Helper()
			if len(want) > maxErrorLogSize {
				want = want[len(want)-maxErrorLogSize:]
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("expected %d tail bytes, got %d, err=%v", len(want), len(got), err)
			}
		}
		check()
		for _, data := range [][]byte{[]byte("new error\n"), bytes.Repeat([]byte("x"), 500_000), bytes.Repeat([]byte("y"), 1_100_000)} {
			if n, err := output.Write(data); err != nil || n != len(data) {
				t.Fatalf("Write: %d, %v", n, err)
			}
			want = append(want, data...)
			check()
		}
		if err := output.Close(); err != nil {
			t.Fatal(err)
		}
		output, err = openErrorLog(path, maxErrorLogSize)
		if err != nil {
			t.Fatal(err)
		}
		data := []byte("after restart\n")
		if _, err := output.Write(data); err != nil {
			t.Fatal(err)
		}
		want = append(want, data...)
		check()
		if err := output.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
