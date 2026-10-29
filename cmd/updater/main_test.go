package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIHelp(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "updater")
	if output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	for _, action := range []string{"", "add", "stop"} {
		for _, help := range []string{"-h", "--help"} {
			t.Run(action+help, func(t *testing.T) {
				args := []string{"-config", filepath.Join(t.TempDir(), "missing.json"), help}
				if action != "" {
					args = append([]string{action}, args...)
				}
				output, err := exec.Command(binary, args...).CombinedOutput()
				if err != nil {
					t.Fatalf("help: %v\n%s", err, output)
				}
				for _, want := range []string{"Usage:", "add", "stop", "-config", "-h, --help"} {
					if !strings.Contains(string(output), want) {
						t.Errorf("help missing %q: %s", want, output)
					}
				}
			})
		}
	}
}
