package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestChildFailureCancelsGroupAndStopsSibling(t *testing.T) {
	g := New(context.Background())
	defer g.Stop()
	uid, gid := uint32(os.Getuid()), uint32(os.Getgid())
	if err := g.Start("sibling", "/bin/sh", []string{"-c", "exec sleep 60"}, os.Environ(), uid, gid); err != nil {
		t.Fatal(err)
	}
	if err := g.Start("failing", "/bin/sh", []string{"-c", "exit 7"}, os.Environ(), uid, gid); err != nil {
		t.Fatal(err)
	}
	select {
	case <-g.Context.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("required child's exit did not cancel the deployment unit")
	}
	if err := g.Wait(); err == nil || !strings.Contains(err.Error(), "failing") {
		t.Fatalf("missing child failure: %v", err)
	}
	g.Stop()
	for _, c := range g.children {
		select {
		case <-c.done:
		default:
			t.Fatal("child remained running")
		}
	}
}
func TestPrivateEnvironmentRejectsReadableFilesWithoutLeakingSecrets(t *testing.T) {
	file := filepath.Join(t.TempDir(), "owner.env")
	if err := os.WriteFile(file, []byte("PASSWORD=secret-never-print\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := EnvFile(file); err == nil || strings.Contains(err.Error(), "secret-never-print") {
		t.Fatal("insecure environment accepted or secret leaked")
	}
	os.Chmod(file, 0600)
	values, err := EnvFile(file)
	if err != nil || Value(values, "PASSWORD") != "secret-never-print" {
		t.Fatal("valid private file rejected")
	}
}
