package updater

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnabledConfig(t *testing.T) {
	for _, tc := range []struct {
		field          string
		valid, enabled bool
	}{
		{"", true, true},
		{`,"enabled":true`, true, true},
		{`,"enabled":false`, true, false},
		{`,"enabled":null`, true, true},
		{`,"enabled":"false"`, false, false},
		{`,"enabled":0`, false, false},
	} {
		c, err := validateConfig([]byte(`{"items":[{"repo":"repo","target":"project","run":"true"`+tc.field+`}]}`), "config.json")
		if (err == nil) != tc.valid {
			t.Fatalf("%s: %v", tc.field, err)
		}
		if tc.valid && (len(c.enabledItems()) == 1) != tc.enabled {
			t.Fatalf("%s: wrong enabled state", tc.field)
		}
	}
}

func TestDisabledItemsSkipped(t *testing.T) {
	for _, allDisabled := range []bool{true, false} {
		t.Run(fmt.Sprint(allDisabled), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.json")
			data := fmt.Sprintf(`{"items":[{"repo":"disabled","target":"disabled","run":"exit 99","enabled":false},{"repo":"active","target":"active","run":"exit 99","enabled":%t}]}`, !allDisabled)
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			calls := filepath.Join(dir, "calls")
			t.Setenv("CALLS", calls)
			// No executables are installed for the all-disabled case.
			if !allDisabled {
				if err := os.MkdirAll(filepath.Join(dir, "active", ".git"), 0700); err != nil {
					t.Fatal(err)
				}
				for name, script := range map[string]string{
					"git": `#!/bin/sh
printf '%s\n' "$*" >> "$CALLS"
case "$1" in
status) exit 0;;
rev-parse) echo commit;;
pull) exit 0;;
esac
`,
					"docker": "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$CALLS\"\n",
				} {
					if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0700); err != nil {
						t.Fatal(err)
					}
				}
			}
			err := run(context.Background(), path)
			if (err == nil) != allDisabled {
				t.Fatalf("run: %v", err)
			}
			var output strings.Builder
			if err := stopItem(context.Background(), path, strings.NewReader("1\n"), &output); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(output.String(), filepath.Join(dir, "disabled")) {
				t.Fatalf("disabled item listed: %s", &output)
			}
			got, err := os.ReadFile(calls)
			if allDisabled {
				if !os.IsNotExist(err) {
					t.Fatalf("unexpected calls: %s, %v", got, err)
				}
			} else {
				callsText := string(got)
				if err != nil || !strings.Contains(callsText, "pull --ff-only") || !strings.Contains(callsText, "compose stop") || strings.Contains(callsText, "disabled") {
					t.Fatalf("calls: %q, %v", got, err)
				}
			}
		})
	}
}
