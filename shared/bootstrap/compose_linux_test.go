package bootstrap

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestComposeAgentAndProbeIdentity(t *testing.T) {
	if os.Getenv("AVOPS_COMPOSE_TEST") != "1" {
		t.Skip("explicit isolated Compose acceptance required")
	}
	if os.Geteuid() != 0 {
		t.Fatal("root required")
	}
	root, e := os.Getwd()
	if e != nil {
		t.Fatal(e)
	}
	for {
		if _, e = os.Stat(filepath.Join(root, "go.mod")); e == nil {
			break
		}
		next := filepath.Dir(root)
		if next == root {
			t.Fatal("source root missing")
		}
		root = next
	}
	for _, role := range []string{"agent", "probe"} {
		t.Run(role, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
			defer cancel()
			work := t.TempDir()
			config := filepath.Join(work, "config")
			os.Mkdir(config, 0700)
			name := fmt.Sprintf("avops-compose-%s-test-%d", role, time.Now().UnixNano())
			secret := "AVOPS_AGENT_ENROLL_TOKEN="
			filename := "agent.env"
			endpoint := "AVOPS_CENTER_URL=wss://go-init.invalid:9/api/v1/agent/connect\n"
			if role == "probe" {
				secret = "AVOPS_PROBE_ENROLL_TOKEN="
				filename = "probe-client.env"
				endpoint = "AVOPS_PROBE_CENTER_URL=wss://go-init.invalid:9/api/v1/probes/connect\n"
			}
			os.WriteFile(filepath.Join(config, filename), []byte(endpoint+secret+strings.Repeat("s", 32)+"\n"), 0600)
			env := fmt.Sprintf("CONTAINER_NAME=%s\nCONFIG_DIR=%s\nDATA_DIR=%s\nHOST_DIR=%s\nCLIENTS_DIR=%s\nHOST_ADDRESS=192.0.2.99\nCENTER_HOST=go-init.invalid\nCENTER_IP=127.0.0.1\n", name, config, filepath.Join(work, "data"), filepath.Join(work, "host"), filepath.Join(work, "clients"))
			if role == "agent" {
				data, e := exec.CommandContext(ctx, "docker", "info", "--format", "{{.DockerRootDir}}").Output()
				if e != nil {
					t.Fatal(e)
				}
				env += "DOCKER_ROOT=" + strings.TrimSpace(string(data)) + "\n"
			}
			envFile := filepath.Join(work, "public.env")
			os.WriteFile(envFile, []byte(env), 0600)
			base := []string{"compose", "--project-name", name, "--env-file", envFile, "-f", filepath.Join(root, role, "deploy/compose.yaml")}
			run := func(args ...string) ([]byte, error) {
				c := exec.CommandContext(ctx, "docker", append(append([]string{}, base...), args...)...)
				return c.CombinedOutput()
			}
			t.Cleanup(func() { exec.Command("docker", append(base, "down")...).Run() })
			if out, e := run("up", "-d", "--wait", "--wait-timeout", "65"); e != nil {
				t.Fatalf("Compose %s failed: %v\n%s", role, e, out)
			}
			identity := filepath.Join(work, "data/agent-identity.json")
			if role == "probe" {
				identity = filepath.Join(work, "host/server-identity-v2.json")
				body, e := exec.CommandContext(ctx, "docker", "top", name, "-eo", "pid,uid,comm").Output()
				if e != nil {
					t.Fatal(e)
				}
				for _, line := range strings.Split(string(body), "\n") {
					for _, program := range []string{"probe-server", "rtc-probe", "rtc-web-probe"} {
						if strings.Contains(line, program) && !strings.Contains(line, "65532") {
							t.Fatal("SDK runtime did not drop privileges", program)
						}
					}
				}
			}
			before, e := os.ReadFile(identity)
			if e != nil {
				t.Fatal(e)
			}
			if out, e := run("restart"); e != nil {
				t.Fatalf("restart: %v %s", e, out)
			}
			time.Sleep(2 * time.Second)
			after, e := os.ReadFile(identity)
			if e != nil || string(before) != string(after) {
				t.Fatal("restart replaced persistent identity")
			}
			t.Log("Compose healthy; identity persisted after restart")
		})
	}
}
