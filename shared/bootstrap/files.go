// Package bootstrap supplies private configuration primitives for container entrypoints.
package bootstrap

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/johankoi91/monitor/runtime/shared/durable"
)

func ReadEnv(path string) (map[string]string, error) {
	info, e := os.Stat(path)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("private config %s must exist with mode 0600", filepath.Base(path))
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	scanner := bufio.NewScanner(io.LimitReader(f, 1<<20))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	out := map[string]string{}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || key == "" || strings.TrimSpace(key) != key {
			return nil, errors.New("invalid private environment file")
		}
		if _, ok = out[key]; ok {
			return nil, errors.New("duplicate private environment field")
		}
		out[key] = value
	}
	return out, scanner.Err()
}
func Environment(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+values[key])
	}
	return out
}
func Map(environment []string) map[string]string {
	out := map[string]string{}
	for _, value := range environment {
		key, v, ok := strings.Cut(value, "=")
		if ok {
			out[key] = v
		}
	}
	return out
}
func Merge(base []string, overlays ...map[string]string) []string {
	values := Map(base)
	for _, overlay := range overlays {
		for key, value := range overlay {
			values[key] = value
		}
	}
	return Environment(values)
}
func PrivateDirectory(path string, uid int) error {
	if !filepath.IsAbs(path) {
		return errors.New("absolute persistent directory required")
	}
	if info, e := os.Lstat(path); e == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("persistent directory must not be a symlink")
	}
	if e := os.MkdirAll(path, 0700); e != nil {
		return e
	}
	if e := os.Chmod(path, 0700); e != nil {
		return e
	}
	if uid >= 0 {
		return os.Chown(path, uid, uid)
	}
	return nil
}
func CopyOwned(source, target string, mode os.FileMode, uid int) error {
	b, e := os.ReadFile(source)
	if e != nil {
		return e
	}
	if e = durable.Bytes(target, b); e != nil {
		return e
	}
	if e = os.Chmod(target, mode); e != nil {
		return e
	}
	if uid >= 0 {
		return os.Chown(target, uid, uid)
	}
	return nil
}
func Overrides(environment []string, keys ...string) map[string]string {
	values := Map(environment)
	out := map[string]string{}
	for _, key := range keys {
		if value := values[key]; value != "" {
			out[key] = value
		}
	}
	return out
}
