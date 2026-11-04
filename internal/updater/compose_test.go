package updater

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateComposeSkip(t *testing.T) {
	const healthy = `{"Service":"web","State":"running","Health":"healthy"}`
	for _, tc := range []struct {
		name, services, status         string
		changed, cloned, failure, skip bool
	}{
		{name: "healthy array", services: "web", status: "[" + healthy + "]", skip: true},
		{name: "healthy stream", services: "web\ndb", status: healthy + "\n" + `{"Service":"db","State":"running","Health":""}`, skip: true},
		{name: "new commits", services: "web", status: healthy, changed: true},
		{name: "clone", services: "web", status: healthy, cloned: true},
		{name: "missing service", services: "web\ndb", status: healthy},
		{name: "stopped replica", services: "web", status: healthy + "\n" + `{"Service":"web","State":"exited"}`},
		{name: "unhealthy", services: "web", status: `{"Service":"web","State":"running","Health":"unhealthy"}`},
		{name: "starting", services: "web", status: `{"Service":"web","State":"running","Health":"starting"}`},
		{name: "restarting", services: "web", status: `{"Service":"web","State":"restarting"}`},
		{name: "no containers", services: "web", status: "[]"},
		{name: "no services", status: healthy},
		{name: "invalid output", services: "web", status: "not json"},
		{name: "docker error", services: "web", status: healthy, failure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "project")
			if !tc.cloned {
				if err := os.MkdirAll(filepath.Join(target, ".git"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("CHANGED", fmt.Sprint(tc.changed))
			t.Setenv("SERVICES", tc.services)
			t.Setenv("STATUS", tc.status)
			t.Setenv("FAILURE", fmt.Sprint(tc.failure))
			scripts := map[string]string{
				"git": `#!/bin/sh
case "$1" in
clone) mkdir -p "$4/.git";;
remote) echo repo;;
rev-parse) if [ "$CHANGED" = true ] && [ -f .git/pulled ]; then echo after; else echo before; fi;;
pull) echo pulled > .git/pulled;;
esac
`,
				"docker": `#!/bin/sh
printf '%s\n' "$*" >> docker-calls
case "$*" in
"compose config --services") printf '%s\n' "$SERVICES";;
"compose ps --all --format json") printf '%s\n' "$STATUS"; [ "$FAILURE" != true ];;
"compose ps") exit 0;;
*) exit 1;;
esac
`,
			}
			for name, script := range scripts {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			result, err := update(context.Background(), item{Repo: "repo", Target: target, Run: "echo ran > ran"}, io.Discard, log.New(io.Discard, "", 0))
			if err != nil {
				t.Fatal(err)
			}
			_, err = os.Stat(filepath.Join(target, "ran"))
			if tc.skip != os.IsNotExist(err) || tc.skip != strings.Contains(result, "run пропущен") {
				t.Fatalf("skip=%v, stat=%v, result=%s", tc.skip, err, result)
			}
			if !tc.cloned {
				if _, err := os.Stat(filepath.Join(target, ".git", "pulled")); err != nil {
					t.Fatal("pull was not executed:", err)
				}
			}
			if tc.skip {
				calls, err := os.ReadFile(filepath.Join(target, "docker-calls"))
				if err != nil || string(calls) != "compose config --services\ncompose ps --all --format json\n" {
					t.Fatalf("unexpected Docker operations: %q, %v", calls, err)
				}
			}
		})
	}
}
