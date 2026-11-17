//go:build linux && agora

package main

import (
	"context"
	"github.com/johankoi91/monitor/rtc-probe/agoranative"
	"io"
)

const runtimeDriver = "agora"

func selfCheck() error { return agoranative.SelfCheck() }

func runWorker(ctx context.Context, input io.Reader, output io.Writer) error {
	return agoranative.Run(ctx, input, output)
}
