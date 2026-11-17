package testkit

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"

	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

type synchronizedWriter struct {
	mu     sync.Mutex
	output io.Writer
}

func (w *synchronizedWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.output.Write(b)
}

// Run models the production single-probe pair boundary for isolated tests.
func Run(parent context.Context, input io.Reader, output io.Writer) error {
	decoder := json.NewDecoder(input)
	var initial protocol.Message
	if err := decoder.Decode(&initial); err != nil {
		return err
	}
	if initial.Job == nil || initial.Job.Role != "pair" {
		return errors.New("mock pair assignment required")
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	writer := &synchronizedWriter{output: output}
	var controls []*io.PipeWriter
	done := make(chan error, 2)
	for _, role := range []string{"sender", "receiver"} {
		reader, control := io.Pipe()
		controls = append(controls, control)
		defer control.Close()
		go func() { defer reader.Close(); done <- runRole(ctx, reader, writer) }()
		job := *initial.Job
		job.Role = role
		if role == "receiver" {
			job.UID = "10002"
		}
		message := initial
		message.Job = &job
		if err := json.NewEncoder(control).Encode(message); err != nil {
			return err
		}
	}
	go func() {
		for {
			var message protocol.Message
			if decoder.Decode(&message) != nil {
				cancel()
				return
			}
			for _, control := range controls {
				_ = json.NewEncoder(control).Encode(message)
			}
		}
	}()
	first := <-done
	second := <-done
	if first != nil {
		return first
	}
	return second
}
