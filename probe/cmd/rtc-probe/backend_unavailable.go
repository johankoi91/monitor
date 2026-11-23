//go:build !linux || !agora

package main

import (
	"context"
	"errors"
	"io"
)

const runtimeDriver = "unavailable"

func selfCheck() error { return errors.New("native SDK not linked") }

func runWorker(context.Context, io.Reader, io.Writer) error {
	return errors.New("native SDK not linked")
}
