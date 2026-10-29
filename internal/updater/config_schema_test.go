package updater

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigSchemaMetadata(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	c, err := readConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	const schemaURL = "https://raw.githubusercontent.com/avil13/dummy-updater/master/schema/v1/schema.json"
	if c.Schema != schemaURL {
		t.Fatalf("unexpected schema URL: %q", c.Schema)
	}
	if err := addItem(path, "", strings.NewReader("repo\nproject-schema\ntrue\n0\n\n"), io.Discard); err != nil {
		t.Fatal(err)
	}
	c, err = readConfig(path)
	if err != nil || c.Schema != schemaURL || len(c.Items) != 2 {
		t.Fatalf("schema metadata lost during add: %+v, err=%v", c, err)
	}
	invalid := `{"$schema":"` + schemaURL + `","typo":true,"items":[{"repo":"repo","target":"project","run":"true"}]}`
	if _, err := validateConfig([]byte(invalid), path); err == nil {
		t.Fatal("unknown fields must still be rejected")
	}
}
