package deploy_test

import (
	d "github.com/johankoi91/monitor/runtime/shared/deploy"
	"github.com/johankoi91/monitor/runtime/shared/testkit"
	"os"
	"path/filepath"
	"testing"
)

func TestBundleRequiresThreeImagesAndCompleteChecksums(t *testing.T) {
	root := t.TempDir()
	testkit.BundleArtifacts(t, root)
	if images, e := d.VerifyBundle(root); e != nil || len(images) != 3 {
		t.Fatalf("valid bundle rejected: %v", e)
	}
	path := filepath.Join(root, "deploy/probe/compose.yaml")
	f, e := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if e != nil {
		t.Fatal(e)
	}
	f.WriteString("tampered")
	f.Close()
	if _, e = d.VerifyBundle(root); e == nil {
		t.Fatal("tampered Compose accepted")
	}
}
func TestExtraImageAndWrongRoleAreRejected(t *testing.T) {
	root := t.TempDir()
	for _, role := range d.Roles {
		testkit.ImageArtifact(t, root, role)
	}
	if _, e := d.VerifyImage(filepath.Join(root, d.Archive("center")), "agent"); e == nil {
		t.Fatal("wrong role accepted")
	}
	os.WriteFile(filepath.Join(root, "extra.tar"), nil, 0644)
	if _, e := d.VerifyImages(root); e == nil {
		t.Fatal("fourth image accepted")
	}
}
func TestChecksumPathsCannotEscapeBundle(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "VERSION"), []byte(d.Version), 0644)
	os.WriteFile(filepath.Join(root, "SHA256SUMS"), []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  ../outside\n"), 0644)
	if _, e := d.VerifyBundle(root); e == nil {
		t.Fatal("checksum traversal accepted")
	}
}
