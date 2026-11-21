package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/johankoi91/monitor/rtc-probe/probe"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(os.Args) > 1 && os.Args[1] == "self-check" {
		if err := selfCheck(); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "worker" {
		pipe := os.NewFile(3, "worker-evidence")
		if pipe == nil {
			log.Fatal("worker evidence pipe required")
		}
		defer pipe.Close()
		if e := runWorker(ctx, os.Stdin, pipe); e != nil {
			log.Fatal("SDK worker failed")
		}
		return
	}
	if runtimeDriver == "unavailable" {
		log.Fatal("build the Linux SDK probe with -tags agora")
	}
	cfg, e := probe.EnvironmentConfig(runtimeDriver)
	if e != nil {
		log.Fatal(e)
	}
	c, e := probe.New(cfg)
	if e != nil {
		log.Fatal(e)
	}
	log.Print("RTC runtime starting: driver=", runtimeDriver)
	if e = c.Run(ctx); e != nil {
		log.Fatal("RTC runtime stopped: ", e)
	}
}
