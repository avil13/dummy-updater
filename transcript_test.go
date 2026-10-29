package main

import (
	"bytes"
	"os"
	"testing"
)

func TestProjectTranscriptLimit(t *testing.T) {
	for _, sizes := range [][]int{{0}, {10, 20}, {500_000}, {600_000}, {400_000, 200_000, 30}, {500_001, 500_000, 50}} {
		file, err := os.CreateTemp(t.TempDir(), "output-*")
		if err != nil {
			t.Fatal(err)
		}
		transcript := &projectTranscript{File: file, limit: 500_000}
		var full bytes.Buffer
		for i, size := range sizes {
			data := bytes.Repeat([]byte{byte('a' + i)}, size)
			full.Write(data)
			if n, err := transcript.Write(data); err != nil || n != len(data) {
				t.Fatalf("Write: %d, %v", n, err)
			}
			info, err := file.Stat()
			if err != nil || info.Size() > 500_000 {
				t.Fatalf("file exceeded limit: %v, %v", info, err)
			}
		}
		if err := transcript.Close(); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(file.Name())
		if err != nil {
			t.Fatal(err)
		}
		want := full.Bytes()
		if len(want) > 500_000 {
			want = want[len(want)-500_000:]
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("sizes=%v: transcript is not the output tail", sizes)
		}
	}
}

func TestLogConfig(t *testing.T) {
	for _, field := range []string{"max_err_mb", "max_project_mb"} {
		for _, value := range []string{"0", "1", "0.5", "null", "-1", "0.0000001", `"500"`, "9223372036854776"} {
			c, err := validateConfig([]byte(`{"log":{"`+field+`":`+value+`},"items":[{"repo":"repo","target":"project","run":"true"}]}`), "config.json")
			valid := value == "0" || value == "1" || value == "0.5" || value == "null"
			if (err == nil) != valid {
				t.Fatalf("%s=%s: %v", field, value, err)
			}
			if valid {
				errLimit, projectLimit := c.logLimits()
				wantErr, wantProject := int64(1_000_000), int64(500_000)
				if value != "null" {
					var want int64
					switch value {
					case "1":
						want = 1_000_000
					case "0.5":
						want = 500_000
					}
					if field == "max_err_mb" {
						wantErr = want
					} else {
						wantProject = want
					}
				}
				if errLimit != wantErr || projectLimit != wantProject {
					t.Fatalf("%s=%s: limits=%d,%d", field, value, errLimit, projectLimit)
				}
			}
		}
	}
	for _, setting := range []string{`"output_log_max_kb":500`, `"log":{"typo":1}`} {
		if _, err := validateConfig([]byte(`{`+setting+`,"items":[{"repo":"repo","target":"project","run":"true"}]}`), "config.json"); err == nil {
			t.Fatalf("unexpectedly accepted %s", setting)
		}
	}
}
