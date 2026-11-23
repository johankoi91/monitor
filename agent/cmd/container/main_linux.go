// Agent and cAdvisor share one deployment unit and one lifecycle.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	agent "github.com/johankoi91/monitor/runtime/agent/internal/agent"
	"github.com/johankoi91/monitor/runtime/shared/supervisor"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "health" {
		client := http.Client{Timeout: 2 * time.Second}
		response, err := client.Get("http://127.0.0.1:18089/healthz")
		if err != nil {
			os.Exit(1)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err := run(ctx); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func run(ctx context.Context) error {
	environment, err := agent.ContainerEnvironment(os.Environ())
	if err != nil {
		return err
	}
	g := supervisor.New(ctx)
	defer g.Stop()
	args := []string{"--listen_ip=127.0.0.1", "--port=18089", "--docker_only=true", "--housekeeping_interval=5s", "--allow_dynamic_housekeeping=false", "--store_container_labels=false", "--enable_metrics=cpu,memory,network,disk,diskIO,oom_event"}
	if err := g.Start("cAdvisor", "/cadvisor", args, []string{"PATH=/usr/bin:/bin", "HOME=/tmp"}, 0, 0); err != nil {
		return err
	}
	ready, cancel := context.WithTimeout(g.Context, 45*time.Second)
	defer cancel()
	client := http.Client{Timeout: time.Second}
	for {
		req, _ := http.NewRequestWithContext(ready, http.MethodGet, "http://127.0.0.1:18089/healthz", nil)
		response, err := client.Do(req)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				break
			}
		}
		select {
		case <-ready.Done():
			return errors.New("cAdvisor startup did not become ready")
		case <-time.After(250 * time.Millisecond):
		}
	}
	if err := g.Start("Agent", "/agent", nil, environment, 0, 0); err != nil {
		return err
	}
	log.Print("Agent container started with embedded cAdvisor")
	return g.Wait()
}
