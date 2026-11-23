// Package release assembles prebuilt role artifacts; it does not build business images.
package release

import (
	"context"
	"errors"
	d "github.com/johankoi91/monitor/runtime/shared/deploy"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var documents = []string{"VERSION", "产品PRD.md", "技术实现.md", "部署说明.md"}

type Options struct{ Repo, Images, Output string }

func privateName(path string) bool {
	base := filepath.Base(path)
	if strings.HasPrefix(base, ".env") && base != ".env.example" {
		return true
	}
	for _, name := range []string{"admin-curl.conf", "receiver-curl.conf", "account-handoff.txt", "center.yaml", "init.sql"} {
		if base == name {
			return true
		}
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".env", ".pem", ".key", ".log", ".jsonl", ".dump", ".sqlite", ".db", ".pid", ".tar", ".gz", ".zip", ".p12", ".pfx":
		return true
	}
	return false
}
func CopySource(repo, target string, exclude ...string) error {
	repo, e := filepath.Abs(repo)
	if e != nil {
		return e
	}
	excluded := map[string]bool{".git": true, "node_modules": true, "build": true, "releases": true, "__pycache__": true, ".DS_Store": true, "data": true, "dist": true}
	exclude = append(exclude, target)
	return filepath.WalkDir(repo, func(path string, entry fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(repo, path)
		if e != nil {
			return e
		}
		if rel == "." {
			return nil
		}
		for _, root := range exclude {
			absolute, _ := filepath.Abs(root)
			if d.Inside(absolute, path) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if excluded[entry.Name()] {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if privateName(path) {
			return nil
		}
		for _, name := range documents[1:] {
			if entry.Name() == name {
				return nil
			}
		}
		source := path
		info, e := entry.Info()
		if e != nil {
			return e
		}
		if entry.Type()&os.ModeSymlink != 0 {
			source, e = filepath.EvalSymlinks(path)
			if e != nil {
				return e
			}
			if !d.Inside(repo, source) || privateName(source) {
				return errors.New("source symlink points outside public source")
			}
			info, e = os.Stat(source)
			if e != nil {
				return e
			}
		}
		if !info.Mode().IsRegular() {
			return errors.New("unsupported public source file")
		}
		return d.CopyFile(source, filepath.Join(target, rel), info.Mode().Perm())
	})
}
func Bundle(ctx context.Context, o Options) (string, error) {
	if e := ctx.Err(); e != nil {
		return "", e
	}
	repo, e := d.FindRepo(o.Repo)
	if e != nil {
		return "", e
	}
	images, e := d.VerifyImages(o.Images)
	if e != nil {
		return "", e
	}
	b, e := os.ReadFile(filepath.Join(repo, "VERSION"))
	if e != nil || strings.TrimSpace(string(b)) != d.Version {
		return "", errors.New("V1.0 source required")
	}
	output, e := filepath.Abs(o.Output)
	if e != nil {
		return "", e
	}
	root := filepath.Join(output, "avops-monitor-V1.0-linux-amd64")
	if e = d.NewDirectory(root); e != nil {
		return "", e
	}
	for _, image := range images {
		if e = d.CopyFile(filepath.Join(o.Images, image.Archive), filepath.Join(root, "images", image.Archive), 0644); e != nil {
			return "", e
		}
	}
	if e = d.WriteJSON(filepath.Join(root, "images/verified-images.json"), map[string]any{"version": d.Version, "images": images}, 0644); e != nil {
		return "", e
	}
	for _, role := range d.Roles {
		for _, name := range []string{"compose.yaml", ".env.example"} {
			if e = d.CopyFile(filepath.Join(repo, role, "deploy", name), filepath.Join(root, "deploy", role, name), 0644); e != nil {
				return "", e
			}
		}
	}
	for _, name := range documents {
		if e = d.CopyFile(filepath.Join(repo, name), filepath.Join(root, name), 0644); e != nil {
			return "", e
		}
	}
	if e = CopySource(repo, filepath.Join(root, "source"), root, o.Images); e != nil {
		return "", e
	}
	if e = d.Checksums(root); e != nil {
		return "", e
	}
	if _, e = d.VerifyBundle(root); e != nil {
		return "", e
	}
	archive := root + ".tar.gz"
	if e = d.TarGzip(root, archive); e != nil {
		return "", e
	}
	sum, e := d.Digest(archive)
	if e != nil {
		return "", e
	}
	if e = os.WriteFile(archive+".sha256", []byte(sum+"  "+filepath.Base(archive)+"\n"), 0644); e != nil {
		return "", e
	}
	return archive, nil
}
