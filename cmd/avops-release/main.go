package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	r "github.com/johankoi91/monitor/runtime/cmd/avops-release/internal"
	d "github.com/johankoi91/monitor/runtime/shared/deploy"
	"os"
	"os/signal"
	"syscall"
)

func run(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "--help" {
		d.Usage(os.Stdout, "avops-release", []string{"bundle", "verify"})
		return nil
	}
	switch args[0] {
	case "bundle":
		return d.FlagSet("avops-release bundle", args[1:], func(f *flag.FlagSet) func() error {
			repo := f.String("repo", ".", "source repository")
			images := f.String("images", "", "exactly three role image archives")
			output := f.String("output", "releases", "new output directory")
			return func() error {
				if *images == "" {
					return errors.New("--images required")
				}
				archive, e := r.Bundle(ctx, r.Options{Repo: *repo, Images: *images, Output: *output})
				if e == nil {
					fmt.Println(archive)
				}
				return e
			}
		})
	case "verify":
		return d.FlagSet("avops-release verify", args[1:], func(f *flag.FlagSet) func() error {
			bundle := f.String("bundle", "", "extracted offline bundle")
			images := f.String("images", "", "image directory (images-only verification)")
			return func() error {
				var verified []d.VerifiedImage
				var e error
				if *bundle != "" {
					verified, e = d.VerifyBundle(*bundle)
				} else if *images != "" {
					verified, e = d.VerifyImages(*images)
				} else {
					return errors.New("--bundle or --images required")
				}
				if e == nil {
					for _, v := range verified {
						fmt.Printf("%s %s\n", v.Image, v.ID)
					}
				}
				return e
			}
		})
	default:
		return errors.New("unknown avops-release command")
	}
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if e := run(ctx, os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
