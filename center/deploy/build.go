package deploy

import (
	"context"
	d "github.com/johankoi91/monitor/runtime/shared/deploy"
	"os"
	"path/filepath"
)

var binaries = []string{"center", "dbmigrate", "notification-receiver"}

func Binaries(ctx context.Context, repo, output string) error {
	if e := os.MkdirAll(output, 0755); e != nil {
		return e
	}
	if e := d.Run(ctx, filepath.Join(repo, "center/frontend"), nil, "npm", "run", "build"); e != nil {
		return e
	}
	for _, name := range binaries {
		if e := d.GoBuild(ctx, repo, "center/cmd/"+name, filepath.Join(output, name), false, ""); e != nil {
			return e
		}
	}
	if e := d.GoBuild(ctx, repo, "center/cmd/container", filepath.Join(output, "center-init"), false, ""); e != nil {
		return e
	}
	return nil
}
func Image(ctx context.Context, engine d.Docker, repo, bin, ca, output string) error {
	return d.TemplateImage(ctx, engine, repo, "center", bin, ca, output, append(append([]string{}, binaries...), "center-init"))
}
func Build(ctx context.Context, engine d.Docker, repo, output, ca string) error {
	bin := filepath.Join(output, "bin/center")
	if e := Binaries(ctx, repo, bin); e != nil {
		return e
	}

	return Image(ctx, engine, repo, bin, ca, output)
}
