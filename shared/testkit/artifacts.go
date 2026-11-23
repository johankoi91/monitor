package testkit

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	d "github.com/johankoi91/monitor/runtime/shared/deploy"
	"os"
	"path/filepath"
	"testing"
)

// Artifact fixtures are intentionally non-executable and used only by offline
// package guards. They never stand in for an SDK or business-media result.
func ImageArtifact(t *testing.T, dir, role string) {
	t.Helper()
	if e := os.MkdirAll(dir, 0755); e != nil {
		t.Fatal(e)
	}
	f, e := os.Create(filepath.Join(dir, d.Archive(role)))
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	w := tar.NewWriter(f)
	defer w.Close()
	labels := map[string]string{"avops.role": role, "avops.layout": "three-images", "avops.deployment": "compose-v1", "org.opencontainers.image.version": d.Version}
	entry := map[string]string{"center": "/center-init", "agent": "/agent-init", "probe": "/probe-server"}[role]
	if role == "probe" {
		labels["avops.probe.schema"] = "2"
		labels["avops.probe.clients"] = "native,web"
	}
	for name, value := range map[string]any{"manifest.json": []any{map[string]any{"Config": "config.json", "RepoTags": []string{d.Image(role)}}}, "config.json": map[string]any{"architecture": "amd64", "os": "linux", "config": map[string]any{"Labels": labels, "Entrypoint": []string{entry}}}} {
		b, e := json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		if e = w.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(b))}); e != nil {
			t.Fatal(e)
		}
		if _, e = bytes.NewReader(b).WriteTo(w); e != nil {
			t.Fatal(e)
		}
	}
}
func BundleArtifacts(t *testing.T, root string) {
	t.Helper()
	if e := os.MkdirAll(root, 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "VERSION"), []byte(d.Version+"\n"), 0644); e != nil {
		t.Fatal(e)
	}
	for _, role := range d.Roles {
		ImageArtifact(t, filepath.Join(root, "images"), role)
	}
	for _, role := range d.Roles {
		ComposeArtifact(t, root, role)
	}

	if e := d.Checksums(root); e != nil {
		t.Fatal(e)
	}
}

func ComposeArtifact(t *testing.T, root, role string) {
	t.Helper()
	dir := filepath.Join(root, "deploy", role)
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services:\n  "+role+":\n    image: "+d.Image(role)+"\n    pull_policy: never\n"), 0644)
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte("CONTAINER_NAME=avops-"+role+"\n"), 0644)
}
