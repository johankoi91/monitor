package deploy

import (
	"context"
	"errors"
	"flag"
	d "github.com/johankoi91/monitor/runtime/shared/deploy"
	"os"
)

func CLI(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "--help" {
		d.Usage(os.Stdout, "centerctl", []string{"build", "binaries", "image"})
		return nil
	}
	switch args[0] {

	case "build", "binaries", "image":
		return d.FlagSet("centerctl "+args[0], args[1:], func(f *flag.FlagSet) func() error {
			repo := f.String("repo", ".", "repository directory")
			output := f.String("output", "", "output directory")
			bin := f.String("bin", "", "precompiled center binaries (image command)")
			ca := f.String("ca", "", "trusted CA bundle (build/image)")
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
					return Binaries(ctx, root, out)
				case "image":
					return Image(ctx, d.Engine{}, root, *bin, *ca, out)
				default:
					return Build(ctx, d.Engine{}, root, out, *ca)
				}
			}
		})

	default:
		return errors.New("unknown centerctl command")
	}
}
