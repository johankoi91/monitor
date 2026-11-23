package deploy

import (
	"context"
	"errors"
	d "github.com/johankoi91/monitor/runtime/shared/deploy"
	"os"
	"path/filepath"
)

const cadvisorURL = "https://github.com/google/cadvisor/releases/download/v0.60.6/cadvisor-v0.60.6-linux-amd64"
const cadvisorSHA = "c381c2c911bc43d465d1e0eaff60f96d58c031c409bddf06da0316fdde8a9296"

func FetchCAdvisor(ctx context.Context, path string) error {
	if e := d.Fetch(ctx, cadvisorURL, path, cadvisorSHA); e != nil {
		return e
	}
	return os.Chmod(path, 0755)
}
func Binaries(ctx context.Context, repo, output string) error {
	if e := os.MkdirAll(output, 0755); e != nil {
		return e
	}
	for name, path := range map[string]string{"agent": "agent/cmd/agent", "agent-init": "agent/cmd/container"} {
		if e := d.GoBuild(ctx, repo, path, filepath.Join(output, name), false, ""); e != nil {
			return e
		}
	}
	return nil
}
func Image(ctx context.Context, engine d.Docker, repo, bin, ca, output string) error {
	sum, e := d.Digest(filepath.Join(bin, "cadvisor"))
	if e != nil || sum != cadvisorSHA {
		return errors.New("pinned official cAdvisor binary required")
	}
	return d.TemplateImage(ctx, engine, repo, "agent", bin, ca, output, []string{"agent", "agent-init", "cadvisor"})
}
func Build(ctx context.Context, engine d.Docker, repo, output, ca, cadvisor string) error {
	bin := filepath.Join(output, "bin/agent")
	if e := Binaries(ctx, repo, bin); e != nil {
		return e
	}
	if cadvisor != "" {
		if e := d.CopyFile(cadvisor, filepath.Join(bin, "cadvisor"), 0755); e != nil {
			return e
		}
	} else if e := FetchCAdvisor(ctx, filepath.Join(bin, "cadvisor")); e != nil {
		return e
	}

	return Image(ctx, engine, repo, bin, ca, output)
}
