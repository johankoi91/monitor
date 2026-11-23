package bootstrap

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestComposeFreshRestartAndPartialConfig(t *testing.T) {
	if os.Getenv("AVOPS_COMPOSE_TEST") != "1" {
		t.Skip("explicit isolated Compose acceptance required")
	}
	if os.Geteuid() != 0 {
		t.Fatal("isolated Compose test requires root")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	o := fixture(t)
	root := filepath.Dir(o.Private)
	name := fmt.Sprintf("avops-compose-center-test-%d", time.Now().UnixNano())
	compose, e := filepath.Abs("../../deploy/compose.yaml")
	if e != nil {
		t.Fatal(e)
	}
	envFile := filepath.Join(root, "public.env")
	env := fmt.Sprintf("CENTER_URL=%s\nCONTAINER_NAME=%s\nSITE_CODE=test\nSITE_NAME=测试中心\nTLS_CERT_FILE=%s\nTLS_KEY_FILE=%s\nCONFIG_DIR=%s\nRUNTIME_DIR=%s\nPG_INIT_DIR=%s\nSTATE_DIR=%s\nPOSTGRES_DIR=%s\nRECEIVER_DIR=%s\nCPUS=2\nMEMORY=1024m\n", o.URL, name, o.Cert, o.Key, o.Private, o.Runtime, o.InitSQL, o.State, o.Postgres, o.Receiver)
	os.WriteFile(envFile, []byte(env), 0600)
	override := filepath.Join(root, "bridge.yaml")
	os.WriteFile(override, []byte("services:\n  center:\n    network_mode: bridge\n    restart: 'no'\n    ports: ['127.0.0.1:29443:29443']\n"), 0600)
	base := []string{"compose", "--project-name", name, "--env-file", envFile, "-f", compose, "-f", override}
	run := func(args ...string) ([]byte, error) {
		c := exec.CommandContext(ctx, "docker", append(append([]string{}, base...), args...)...)
		return c.CombinedOutput()
	}
	t.Cleanup(func() { exec.Command("docker", append(base, "down")...).Run() })
	if out, e := run("up", "-d", "--wait", "--wait-timeout", "90"); e != nil {
		t.Fatalf("fresh Compose failed: %v\n%s", e, out)
	}
	snapshot := map[string]string{}
	for _, file := range required {
		body, e := os.ReadFile(filepath.Join(o.Private, file))
		if e != nil {
			t.Fatal(e)
		}
		snapshot[file] = string(body)
	}
	certificate, _ := os.ReadFile(o.Cert)
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(certificate)
	client := http.Client{Timeout: time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}
	request, _ := http.NewRequest("GET", o.URL+"/", nil)
	response, e := client.Do(request)
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("UI not served")
	}
	post := func(path string, body any, token string) (int, string) {
		t.Helper()
		data, e := json.Marshal(body)
		if e != nil {
			t.Fatal(e)
		}
		request, e := http.NewRequest("POST", o.URL+path, bytes.NewReader(data))
		if e != nil {
			t.Fatal(e)
		}
		request.Header.Set("Content-Type", "application/json")
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response, e := client.Do(request)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		var result struct{ Token string }
		if e = json.NewDecoder(response.Body).Decode(&result); e != nil {
			t.Fatal(e)
		}
		return response.StatusCode, result.Token
	}
	status, token := post("/api/v1/auth/login", map[string]string{"username": "admin", "password": "1111"}, "")
	if status != 200 || token == "" {
		t.Fatal("default admin/1111 could not log in")
	}
	if status, _ = post("/api/v1/auth/password", map[string]string{"old_password": "1111", "new_password": "compose-changed-123"}, token); status != 200 {
		t.Fatal("initial admin password could not be changed")
	}
	if out, e := run("restart"); e != nil {
		t.Fatalf("restart: %v %s", e, out)
	}
	ready := false
	for i := 0; i < 40; i++ {
		if _, e = run("exec", "-T", "center", "/center-init", "health"); e == nil {
			ready = true
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !ready {
		t.Fatal("restart did not recover")
	}
	if status, _ = post("/api/v1/auth/login", map[string]string{"username": "admin", "password": "1111"}, ""); status != 401 {
		t.Fatal("restart restored the old initial password")
	}
	if status, _ = post("/api/v1/auth/login", map[string]string{"username": "admin", "password": "compose-changed-123"}, ""); status != 200 {
		t.Fatal("restart lost the changed account password")
	}
	for file, body := range snapshot {
		current, _ := os.ReadFile(filepath.Join(o.Private, file))
		if string(current) != body {
			t.Fatal("restart changed persisted secret", file)
		}
	}
	if _, e = run("stop"); e != nil {
		t.Fatal(e)
	}
	os.Remove(filepath.Join(o.Private, "owner.env"))
	if _, e = run("up", "-d", "--wait", "--wait-timeout", "15"); e == nil {
		t.Fatal("partial config unexpectedly started")
	}
	for file, body := range snapshot {
		if file == "owner.env" {
			continue
		}
		current, _ := os.ReadFile(filepath.Join(o.Private, file))
		if string(current) != body {
			t.Fatal("partial config replaced surviving file", file)
		}
	}
	// The test inspects errors only, never dumps generated env values or passwords.
	logs, _ := run("logs", "--tail=5", "center")
	if !strings.Contains(string(logs), "missing files") {
		t.Fatal("missing-file failure was not explicit")
	}
}
