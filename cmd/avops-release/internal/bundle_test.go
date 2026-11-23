package release

import (
	"context"
	d "github.com/johankoi91/monitor/runtime/shared/deploy"
	"github.com/johankoi91/monitor/runtime/shared/testkit"
	"os"
	"path/filepath"
	"testing"
)

func TestBundlePreservesFrontendAndExcludesPrivateAndGeneratedTrees(t *testing.T) {
	work := t.TempDir()
	repo := filepath.Join(work, "repo")
	os.MkdirAll(filepath.Join(repo, "center"), 0755)
	for _, name := range append(documents, "go.mod", "center/Dockerfile", "center/frontend/static/index.html", "build/old", "releases/old", "node_modules/package/index.js", "private.key", ".env", "admin-curl.conf") {
		path := filepath.Join(repo, name)
		os.MkdirAll(filepath.Dir(path), 0755)
		body := "fixture"
		if name == "VERSION" {
			body = d.Version
		}
		os.WriteFile(path, []byte(body), 0644)
	}
	images := filepath.Join(work, "images")
	for _, role := range d.Roles {
		testkit.ImageArtifact(t, images, role)
	}
	for _, role := range d.Roles {
		testkit.ComposeArtifact(t, repo, role)
		from := filepath.Join(repo, "deploy", role)
		to := filepath.Join(repo, role, "deploy")
		os.MkdirAll(filepath.Dir(to), 0755)
		os.Rename(from, to)
	}

	archive, e := Bundle(context.Background(), Options{Repo: repo, Images: images, Output: filepath.Join(repo, "delivery")})
	if e != nil {
		t.Fatal(e)
	}
	root := archive[:len(archive)-len(".tar.gz")]
	if _, e = d.VerifyBundle(root); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(root, "source/center/frontend/static/index.html")); e != nil {
		t.Fatal("embedded frontend missing")
	}
	for _, name := range []string{"build", "releases", "node_modules", "private.key", ".env", "admin-curl.conf", "delivery/avops-monitor-V1.0-linux-amd64"} {
		if _, e = os.Stat(filepath.Join(root, "source", name)); !os.IsNotExist(e) {
			t.Fatal("private/generated source copied", name)
		}
	}
	if _, e = os.Stat(archive + ".sha256"); e != nil {
		t.Fatal(e)
	}
	entries, e := os.ReadDir(filepath.Join(root, "deploy"))
	if e != nil || len(entries) != 3 {
		t.Fatal("expected only three role Compose directories")
	}
}
