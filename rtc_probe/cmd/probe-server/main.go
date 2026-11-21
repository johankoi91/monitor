//go:build linux

package main

import (
	"context"
	"github.com/johankoi91/monitor/rtc-probe/internal/server"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	clients, err := server.Clients(os.Environ())
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err = server.Run(ctx, clients); err != nil {
		log.Fatal(err)
	}
}
