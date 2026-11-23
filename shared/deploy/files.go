// Package deploy supplies filesystem, Docker and artifact primitives to role-owned tools.
package deploy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const Version = "V1.0"

var Roles = []string{"center", "probe", "agent"}

func Image(role string) string   { return "avops-" + role + ":" + Version }
func Archive(role string) string { return "avops-" + role + "-linux-amd64.tar" }
func Digest(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	_, e = io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), e
}
func CopyFile(source, target string, mode fs.FileMode) error {
	f, e := os.Open(source)
	if e != nil {
		return e
	}
	defer f.Close()
	if e = os.MkdirAll(filepath.Dir(target), 0755); e != nil {
		return e
	}
	out, e := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if e != nil {
		return e
	}
	_, e = io.Copy(out, f)
	if e == nil {
		e = out.Sync()
	}
	closeErr := out.Close()
	if e != nil {
		return e
	}
	return closeErr
}
func CopyTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(source, path)
		if e != nil {
			return e
		}
		to := filepath.Join(target, rel)
		if d.IsDir() {
			return os.MkdirAll(to, 0755)
		}
		if d.Type()&os.ModeSymlink != 0 {
			resolved, e := filepath.EvalSymlinks(path)
			if e != nil {
				return e
			}
			info, e := os.Stat(resolved)
			if e != nil {
				return e
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("non-file dependency symlink: %s", path)
			}
			return CopyFile(resolved, to, info.Mode().Perm())
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return errors.New("unsupported source file")
		}
		return CopyFile(path, to, info.Mode().Perm())
	})
}
func WriteJSON(path string, value any, mode fs.FileMode) error {
	b, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(path, append(b, '\n'), mode)
}
func NewDirectory(path string) error {
	if _, e := os.Lstat(path); !os.IsNotExist(e) {
		return errors.New("output already exists; choose a new directory")
	}
	return os.MkdirAll(path, 0755)
}
func Inside(parent, path string) bool {
	rel, e := filepath.Rel(parent, path)
	return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}
func Absolute(path string) (string, error) {
	if path == "" {
		return "", errors.New("path required")
	}
	return filepath.Abs(path)
}
func NoSymlinks(path string) error {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		info, e := os.Lstat(current)
		if e == nil && info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink paths are not supported")
		}
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}
func FindRepo(path string) (string, error) {
	if path == "" {
		path = "."
	}
	path, e := filepath.Abs(path)
	if e != nil {
		return "", e
	}
	for {
		if _, e = os.Stat(filepath.Join(path, "go.mod")); e == nil {
			if _, e = os.Stat(filepath.Join(path, "center", "Dockerfile")); e == nil {
				return path, nil
			}
		}
		next := filepath.Dir(path)
		if next == path {
			return "", errors.New("repository root not found; pass --repo")
		}
		path = next
	}
}
