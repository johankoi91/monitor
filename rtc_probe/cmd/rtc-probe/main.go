package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
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
	capacity := 1
	autoCapacity := os.Getenv("RTC_PROBE_CAPACITY") == "auto"
	if text := os.Getenv("RTC_PROBE_CAPACITY"); text != "" && !autoCapacity {
		v, e := strconv.Atoi(text)
		if e != nil {
			log.Fatal("invalid capacity")
		}
		capacity = v
	}
	dir := os.Getenv("RTC_PROBE_DATA_DIR")
	if dir == "" {
		dir = "data/rtc-probe"
	}
	c, e := probe.New(probe.Config{Driver: runtimeDriver, URL: os.Getenv("RTC_PROBE_CENTER_URL"), ID: os.Getenv("RTC_PROBE_ID"), Secret: os.Getenv("RTC_PROBE_SECRET"), DataDir: dir, Capacity: capacity, AutoCapacity: autoCapacity, AllowPlaintextLoopback: os.Getenv("RTC_PROBE_DEV_HTTP") == "1"})
	if e != nil {
		log.Fatal(e)
	}
	log.Print("RTC probe starting: driver=", runtimeDriver)
	if e = c.Run(ctx); e != nil {
		log.Fatal("RTC probe stopped: ", e)
	}
}
