package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Docker interface {
	Output(context.Context, ...string) ([]byte, error)
	Run(context.Context, ...string) error
}
type Engine struct{}

func (Engine) Output(ctx context.Context, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, "docker", args...)
	b, e := c.Output()
	if e != nil {
		return nil, fmt.Errorf("Docker %s failed", args[0])
	}
	return b, nil
}
func (Engine) Run(ctx context.Context, args ...string) error {
	c := exec.CommandContext(ctx, "docker", args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if c.Run() != nil {
		return fmt.Errorf("Docker %s failed", args[0])
	}
	return nil
}
func Run(ctx context.Context, dir string, env []string, name string, args ...string) error {
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	if env != nil {
		c.Env = env
	}
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if c.Run() != nil {
		return fmt.Errorf("%s failed", name)
	}
	return nil
}
func GoBuild(ctx context.Context, repo, path, output string, cgo bool, tags string) error {
	env := append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
	if cgo {
		env = append(env, "CGO_ENABLED=1")
	}
	args := []string{"build", "-trimpath", "-ldflags", "-s -w", "-o", output}
	if tags != "" {
		args = append(args, "-tags", tags)
	}
	return Run(ctx, repo, env, "go", append(args, "./"+path)...)
}

type ImageInfo struct {
	ID           string `json:"Id"`
	Architecture string
	OS           string `json:"Os"`
	Config       struct {
		Labels     map[string]string
		Env        []string
		Entrypoint []string
	}
}

func InspectImage(ctx context.Context, d Docker, image string) (ImageInfo, error) {
	b, e := d.Output(ctx, "image", "inspect", image)
	if e != nil {
		return ImageInfo{}, e
	}
	var list []ImageInfo
	if json.Unmarshal(b, &list) != nil || len(list) != 1 {
		return ImageInfo{}, errors.New("invalid Docker image metadata")
	}
	return list[0], nil
}
func ExportImage(ctx context.Context, d Docker, role, contextDir, output string) error {
	if e := os.MkdirAll(output, 0755); e != nil {
		return e
	}
	archive := filepath.Join(output, Archive(role))
	if _, e := os.Stat(archive); !os.IsNotExist(e) {
		return errors.New("image archive already exists")
	}
	if e := d.Run(ctx, "build", "--platform", "linux/amd64", "--pull=false", "--network", "none", "-t", Image(role), contextDir); e != nil {
		return e
	}
	info, e := InspectImage(ctx, d, Image(role))
	if e != nil {
		return e
	}
	if info.Architecture != "amd64" || info.OS != "linux" {
		return errors.New("Linux amd64 image required")
	}
	if e = d.Run(ctx, "save", "-o", archive, Image(role)); e != nil {
		return e
	}
	if e = os.Chmod(archive, 0644); e != nil {
		return e
	}
	verified, e := VerifyImage(archive, role)
	if e != nil {
		return e
	}
	return WriteJSON(filepath.Join(output, "avops-"+role+"-manifest.json"), verified, 0644)
}
func TemplateImage(ctx context.Context, d Docker, repo, role, bin, ca, output string, names []string) error {
	dir, e := os.MkdirTemp("", "avops-"+role+"-image-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	for _, name := range names {
		if e = CopyFile(filepath.Join(bin, name), filepath.Join(dir, name), 0755); e != nil {
			return e
		}
	}
	if e = CopyFile(ca, filepath.Join(dir, "ca-bundle.crt"), 0644); e != nil {
		return e
	}
	b, e := os.ReadFile(filepath.Join(repo, role, "Dockerfile"))
	if e != nil {
		return e
	}
	b = append(b, []byte("\nLABEL org.opencontainers.image.version=\""+Version+"\"\n")...)
	if e = os.WriteFile(filepath.Join(dir, "Dockerfile"), b, 0644); e != nil {
		return e
	}
	return ExportImage(ctx, d, role, dir, output)
}
func ContainerAbsent(ctx context.Context, d Docker, name string) error {
	b, e := d.Output(ctx, "ps", "-aq", "--filter", "name=^/"+name+"$")
	if e != nil {
		return e
	}
	if strings.TrimSpace(string(b)) != "" {
		return errors.New("container already exists")
	}
	return nil
}
func CopyReader(r io.Reader, path string, mode os.FileMode) error {
	if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if e != nil {
		return e
	}
	_, e = io.Copy(f, r)
	ce := f.Close()
	if e != nil {
		return e
	}
	return ce
}
