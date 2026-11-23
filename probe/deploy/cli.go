package deploy

import (
	"context"
	"errors"
	"flag"
	d "github.com/johankoi91/monitor/runtime/shared/deploy"
	"os"
)

func CLI(ctx context.Context, args []string) error {
	commands := []string{"build", "deps", "binaries", "native-context", "web-context", "image-context"}
	if len(args) == 0 || args[0] == "--help" {
		d.Usage(os.Stdout, "probectl", commands)
		return nil
	}
	switch args[0] {

	case "deps":
		return d.FlagSet("probectl deps", args[1:], func(f *flag.FlagSet) func() error {
			output := f.String("output", "", "verified dependency cache")
			return func() error {
				out, e := d.Absolute(*output)
				if e != nil {
					return e
				}
				return Dependencies(ctx, out)
			}
		})
	case "build", "binaries", "native-context", "web-context", "image-context":
		return d.FlagSet("probectl "+args[0], args[1:], func(f *flag.FlagSet) func() error {
			repo := f.String("repo", ".", "repository directory")
			output := f.String("output", "", "new output directory")
			cache := f.String("cache", "", "verified dependency cache")
			ca := f.String("ca", "", "optional center CA trust bundle")
			nativeBinary := f.String("native-binary", "", "precompiled real Native worker")
			webImage := f.String("web-image", "", "existing build runtime image")
			sdk := f.String("sdk", "", "pinned native SDK dynamic library directory")
			media := f.String("media", "", "fixed media directory")
			webBinary := f.String("web-binary", "", "compiled Web worker")
			native := f.String("native-context", "", "prepared Native runtime context")
			supervisor := f.String("supervisor", "", "compiled probe-server")
			return func() error {
				root, e := d.FindRepo(*repo)
				if e != nil {
					return e
				}
				out, e := d.Absolute(*output)
				if e != nil {
					return e
				}
				switch args[0] {
				case "binaries":
					return Binaries(ctx, root, out, *nativeBinary, *cache)
				case "native-context":
					return NativeContext(ctx, root, *nativeBinary, *sdk, *media, out)
				case "web-context":
					return WebContext(root, *cache, *webBinary, out)
				case "image-context":
					return ImageContext(ctx, d.Engine{}, root, *native, *webImage, *supervisor, *webBinary, *ca, out)
				default:
					return Build(ctx, d.Engine{}, BuildOptions{Repo: root, Output: out, Cache: *cache, CA: *ca, NativeBinary: *nativeBinary, WebImage: *webImage})
				}
			}
		})
	default:
		return errors.New("unknown probectl command")
	}
}
