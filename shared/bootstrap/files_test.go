package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateEnvironmentAndOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.env")
	os.WriteFile(path, []byte("TOKEN=secret-never-print\nHOST=old\n"), 0644)
	if _, e := ReadEnv(path); e == nil || strings.Contains(e.Error(), "secret-never-print") {
		t.Fatal("private env guard failed")
	}
	os.Chmod(path, 0600)
	values, e := ReadEnv(path)
	if e != nil {
		t.Fatal(e)
	}
	out := Map(Merge([]string{"PATH=/bin"}, values, map[string]string{"HOST": "new"}))
	if out["TOKEN"] != "secret-never-print" || out["HOST"] != "new" {
		t.Fatal("private token lost during public overrides")
	}
}
