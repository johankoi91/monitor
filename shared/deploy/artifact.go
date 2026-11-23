package deploy

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type VerifiedImage struct {
	Image    string `json:"image"`
	ID       string `json:"id"`
	Archive  string `json:"archive"`
	Platform string `json:"platform"`
}

func tarMember(archive, name string, max int64) ([]byte, error) {
	f, e := os.Open(archive)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	r := tar.NewReader(f)
	for {
		h, e := r.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		if h.Name == name {
			if h.Typeflag != tar.TypeReg || h.Size > max {
				return nil, errors.New("invalid image metadata member")
			}
			return io.ReadAll(io.LimitReader(r, max+1))
		}
	}
	return nil, fmt.Errorf("image metadata missing: %s", name)
}
func VerifyImage(archive, role string) (VerifiedImage, error) {
	b, e := tarMember(archive, "manifest.json", 1<<20)
	if e != nil {
		return VerifiedImage{}, e
	}
	var entries []struct {
		Config   string
		RepoTags []string
	}
	if json.Unmarshal(b, &entries) != nil || len(entries) != 1 || len(entries[0].RepoTags) != 1 || entries[0].RepoTags[0] != Image(role) {
		return VerifiedImage{}, errors.New("archive must contain exactly one formal image/tag")
	}
	name := entries[0].Config
	if !fs.ValidPath(name) {
		return VerifiedImage{}, errors.New("invalid image metadata path")
	}
	b, e = tarMember(archive, name, 16<<20)
	if e != nil {
		return VerifiedImage{}, e
	}
	var cfg struct {
		Architecture string
		OS           string `json:"os"`
		Config       struct {
			Labels     map[string]string
			Env        []string
			Entrypoint []string
		}
	}
	if json.Unmarshal(b, &cfg) != nil || cfg.Architecture != "amd64" || cfg.OS != "linux" || cfg.Config.Labels["org.opencontainers.image.version"] != Version || cfg.Config.Labels["avops.layout"] != "three-images" || cfg.Config.Labels["avops.role"] != role || cfg.Config.Labels["avops.deployment"] != "compose-v1" {
		return VerifiedImage{}, errors.New("image platform, version or deployment role mismatch")
	}
	expected := map[string]string{"center": "/center-init", "agent": "/agent-init", "probe": "/probe-server"}[role]
	if len(cfg.Config.Entrypoint) != 1 || cfg.Config.Entrypoint[0] != expected {
		return VerifiedImage{}, errors.New("self-contained role entrypoint required")
	}
	if role == "probe" && (cfg.Config.Labels["avops.probe.schema"] != "2" || cfg.Config.Labels["avops.probe.clients"] != "native,web") {
		return VerifiedImage{}, errors.New("combined Native/Web Probe image required")
	}
	return VerifiedImage{Image: Image(role), ID: "sha256:" + digestBytes(b), Archive: Archive(role), Platform: "linux/amd64"}, nil
}
func VerifyImages(dir string) ([]VerifiedImage, error) {
	entries, e := filepath.Glob(filepath.Join(dir, "*.tar"))
	if e != nil || len(entries) != 3 {
		return nil, errors.New("exactly three image archives required")
	}
	result := []VerifiedImage{}
	for _, role := range Roles {
		v, e := VerifyImage(filepath.Join(dir, Archive(role)), role)
		if e != nil {
			return nil, fmt.Errorf("%s: %w", role, e)
		}
		result = append(result, v)
	}
	return result, nil
}
func VerifyBundle(root string) ([]VerifiedImage, error) {
	root, e := filepath.EvalSymlinks(root)
	if e != nil {
		return nil, e
	}
	root, e = filepath.Abs(root)
	if e != nil {
		return nil, e
	}
	b, e := os.ReadFile(filepath.Join(root, "VERSION"))
	if e != nil || strings.TrimSpace(string(b)) != Version {
		return nil, errors.New("V1.0 bundle required")
	}
	f, e := os.Open(filepath.Join(root, "SHA256SUMS"))
	if e != nil {
		return nil, e
	}
	defer f.Close()
	seen := map[string]bool{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.SplitN(line, "  ", 2)
		if len(fields) != 2 || len(fields[0]) != 64 || !fs.ValidPath(fields[1]) || seen[fields[1]] {
			return nil, errors.New("invalid checksum manifest")
		}
		path := filepath.Join(root, filepath.FromSlash(fields[1]))
		if e = NoSymlinks(path); e != nil {
			return nil, e
		}
		actual, e := Digest(path)
		if e != nil || actual != fields[0] {
			return nil, fmt.Errorf("bundle checksum mismatch: %s", fields[1])
		}
		seen[fields[1]] = true
	}
	if e = scanner.Err(); e != nil {
		return nil, e
	}
	if e = filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if rel != "SHA256SUMS" && !seen[rel] {
			return errors.New("bundle file missing from checksum manifest: " + rel)
		}
		return nil
	}); e != nil {
		return nil, e
	}
	for _, role := range Roles {
		for _, name := range []string{"compose.yaml", ".env.example"} {
			if !seen["deploy/"+role+"/"+name] {
				return nil, errors.New("Compose deployment file missing")
			}
		}
		data, e := os.ReadFile(filepath.Join(root, "deploy", role, "compose.yaml"))
		if e != nil {
			return nil, e
		}
		var compose struct {
			Services map[string]struct {
				Image      string
				PullPolicy string `yaml:"pull_policy"`
			}
		}
		if yaml.Unmarshal(data, &compose) != nil || len(compose.Services) != 1 {
			return nil, errors.New("one role service per Compose file required")
		}
		service, ok := compose.Services[role]
		if !ok || service.Image != Image(role) || service.PullPolicy != "never" {
			return nil, errors.New("Compose role image mismatch")
		}
	}

	return VerifyImages(filepath.Join(root, "images"))
}
func Checksums(root string) error {
	lines := []string{}
	e := filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if rel == "SHA256SUMS" {
			return nil
		}
		h, e := Digest(path)
		if e != nil {
			return e
		}
		lines = append(lines, h+"  "+rel)
		return nil
	})
	if e != nil {
		return e
	}
	sort.Strings(lines)
	return os.WriteFile(filepath.Join(root, "SHA256SUMS"), []byte(strings.Join(lines, "\n")+"\n"), 0644)
}
func TarGzip(source, output string) error {
	f, e := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if e != nil {
		return e
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	e = filepath.WalkDir(source, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(filepath.Dir(source), path)
		if e != nil {
			return e
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return errors.New("unsupported bundle file")
		}
		header, e := tar.FileInfoHeader(info, "")
		if e != nil {
			return e
		}
		header.Name = filepath.ToSlash(rel)
		if e = tw.WriteHeader(header); e != nil {
			return e
		}
		if info.Mode().IsRegular() {
			in, e := os.Open(path)
			if e != nil {
				return e
			}
			_, e = io.Copy(tw, in)
			in.Close()
			return e
		}
		return nil
	})
	if e != nil {
		tw.Close()
		gz.Close()
		return e
	}
	if e = tw.Close(); e != nil {
		return e
	}
	if e = gz.Close(); e != nil {
		return e
	}
	return f.Sync()
}
