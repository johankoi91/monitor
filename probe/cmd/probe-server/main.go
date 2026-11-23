//go:build linux

package main

import (
	"context"
	"github.com/johankoi91/monitor/runtime/probe/internal/server"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "health" {
		if server.Health() != nil {
			os.Exit(1)
		}
		return
	}
	environment, err := server.Bootstrap(os.Environ())
	if err != nil {
		log.Fatal(err)
	}
	clients, err := server.Clients(environment)
	if err != nil {
		log.Fatal(err)
	}
	if err = server.DropPrivileges(); err != nil {
		log.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err = server.Run(ctx, clients); err != nil {
		log.Fatal(err)
	}
}
