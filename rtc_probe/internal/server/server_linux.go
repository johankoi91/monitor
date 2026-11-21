//go:build linux

// Package server supervises independently advertised clients on one probe server.
package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Client struct {
	Kind, Binary string
	Environment  []string
}

func Clients(environment []string) ([]Client, error) {
	values := map[string]string{}
	for _, entry := range environment {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
		if strings.HasPrefix(key, "RTC_PROBE_") || key == "RTC_WEB_RUNTIME" || key == "AVOPS_PROBE_ID" || key == "AVOPS_PROBE_SECRET" {
			return nil, errors.New("retired probe environment is unsupported")
		}
	}
	if _, exists := values["AVOPS_PROBE_CAPACITY"]; exists {
		return nil, errors.New("configure independent native/web capacity for the server")
	}
	directory := values["AVOPS_PROBE_DATA_DIR"]
	if directory == "" {
		directory = "/data"
	}
	host := values["AVOPS_PROBE_HOST_DIR"]
	if host == "" {
		host = "/host"
	}
	if !filepath.IsAbs(directory) || !filepath.IsAbs(host) || len(values["AVOPS_PROBE_ENROLL_TOKEN"]) < 24 || values["AVOPS_PROBE_CENTER_URL"] == "" {
		return nil, errors.New("server enrollment configuration and absolute directories required")
	}
	clients := []Client{}
	for _, kind := range []string{"native", "web"} {
		capacity := values["AVOPS_PROBE_"+strings.ToUpper(kind)+"_CAPACITY"]
		if capacity == "" {
			capacity = "1"
		}
		count, err := strconv.Atoi(capacity)
		if err != nil || count < 1 || count > 64 {
			return nil, fmt.Errorf("invalid %s worker capacity", kind)
		}
		runtime := map[string]string{}
		for key, value := range values {
			if key != "LD_LIBRARY_PATH" && key != "LD_PRELOAD" {
				runtime[key] = value
			}
		}
		runtime["AVOPS_PROBE_CAPACITY"] = capacity
		runtime["AVOPS_PROBE_DATA_DIR"] = filepath.Join(directory, kind)
		runtime["AVOPS_PROBE_HOST_DIR"] = host
		binary := "/rtc-web-probe"
		if kind == "native" {
			binary = "/rtc-probe"
			runtime["LD_LIBRARY_PATH"] = "/opt/agora_sdk:/opt/rtc-probe/native-libs"
		}
		env := []string{}
		for key, value := range runtime {
			env = append(env, key+"="+value)
		}
		clients = append(clients, Client{Kind: kind, Binary: binary, Environment: env})
	}
	return clients, nil
}

// A failed client restarts independently; stopping the server first gives each
// client time to cancel workers and flush its own v2 outbox.
func Run(ctx context.Context, clients []Client) error {
	for _, client := range clients {
		if info, err := os.Stat(client.Binary); err != nil || info.Mode()&0111 == 0 {
			return fmt.Errorf("%s runtime binary unavailable", client.Kind)
		}
	}
	var workers sync.WaitGroup
	for _, client := range clients {
		workers.Add(1)
		go func(client Client) { defer workers.Done(); supervise(ctx, client) }(client)
	}
	workers.Wait()
	return nil
}
func supervise(ctx context.Context, client Client) {
	backoff := time.Second
	for ctx.Err() == nil {
		command := exec.Command(client.Binary)
		command.Env = client.Environment
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
		started := time.Now()
		if err := command.Start(); err != nil {
			log.Printf("%s client could not start: %v", client.Kind, err)
		} else {
			log.Printf("%s client started", client.Kind)
			exited := make(chan error, 1)
			go func() { exited <- command.Wait() }()
			select {
			case err := <-exited:
				log.Printf("%s client exited: %v", client.Kind, err)
			case <-ctx.Done():
				_ = command.Process.Signal(syscall.SIGTERM)
				timer := time.NewTimer(25 * time.Second)
				select {
				case <-exited:
					timer.Stop()
				case <-timer.C:
					_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
					<-exited
				}
				return
			}
		}
		if time.Since(started) > time.Minute {
			backoff = time.Second
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if backoff < 16*time.Second {
			backoff *= 2
		}
	}
}
