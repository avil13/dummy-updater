package updater

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

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
