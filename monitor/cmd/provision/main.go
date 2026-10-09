// provision creates private runtime configuration outside the source repository.
// It does not install services, start containers, or print credentials.
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"github.com/johankoi91/monitor/runtime/internal/center"
	"github.com/johankoi91/monitor/runtime/internal/model"
	"github.com/johankoi91/monitor/runtime/internal/notify"
	"gopkg.in/yaml.v3"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func secret() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		log.Fatal(err)
	}
	return hex.EncodeToString(b[:])
}
func main() {
	dir := flag.String("out", "", "private output directory outside the repository")
	updateCenter := flag.String("enable-notifications", "", "existing private center YAML to update without changing credentials")
	receiverEnv := flag.String("receiver-env", "", "private notification receiver environment file")
	acceptance := flag.Bool("acceptance", false, "generate private isolated canary acceptance settings")
	flag.Parse()
	if *updateCenter != "" {
		if *receiverEnv == "" {
			log.Fatal("receiver-env required")
		}
		if err := enableNotifications(*updateCenter, *receiverEnv); err != nil {
			log.Fatal("notification configuration update failed")
		}
		log.Print("notification target configured; existing management/Agent credentials preserved")
		return
	}
	if *dir == "" {
		log.Fatal("-out is required")
	}
	if err := os.MkdirAll(*dir, 0700); err != nil {
		log.Fatal(err)
	}
	admin, agent := secret(), secret()
	files := map[string]string{
		"center.env":      fmt.Sprintf("AVOPS_ADMIN_ID=rtc-admin\nAVOPS_ADMIN_SECRET=%s\nAVOPS_CENTER_ADDR=0.0.0.0:19443\nAVOPS_CENTER_LOCAL_ADDR=127.0.0.1:18084\nAVOPS_CENTER_CONFIG=/etc/avops-monitor/center.yaml\nAVOPS_DATA_DIR=/var/lib/avops-monitor\nAVOPS_TLS_CERT=/etc/avops-monitor/server.pem\nAVOPS_TLS_KEY=/etc/avops-monitor/server-key.pem\nAVOPS_ALLOWED_IPS=111.230.192.59,111.230.108.76,127.0.0.1,::1\n", admin),
		"center.yaml":     fmt.Sprintf("site:\n  code: rtc-pilot\n  name: RTC 首期试点\nagents:\n  - id: rtc-192-59\n    secret: %s\n    host_name: rtc-192-59\n    host_address: 111.230.192.59\n", agent),
		"agent.env":       fmt.Sprintf("AVOPS_AGENT_ID=rtc-192-59\nAVOPS_AGENT_SECRET=%s\nAVOPS_CENTER_URL=wss://111.230.108.76:19443/api/v1/agent/connect\nAVOPS_TLS_SERVER_NAME=edge.rtcdevelopers.com\nAVOPS_DOCKER_SOCKET=/var/run/docker.sock\nAVOPS_COLLECT_SECONDS=15\nAVOPS_PUBLIC_ARGS=port,listen,host,bind,log-level,config,threads\nAVOPS_AGENT_DATA_DIR=/var/lib/avops-agent\n", agent),
		"admin-curl.conf": fmt.Sprintf("user = \"rtc-admin:%s\"\n", admin),
	}
	if *acceptance {
		cfg := center.Config{Site: model.Site{Code: "rtc-v1-acceptance", Name: "V1.0 隔离验收"}, Agents: []center.AgentConfig{{ID: "rtc-v1-qa", Secret: agent, HostName: "rtc-v1-acceptance", HostAddress: "111.230.192.59"}}, RestartRules: []model.RestartRule{{AgentID: "rtc-v1-qa", ContainerName: "avops-v1-canary", ServiceCode: "rtc-pilot-canary", Image: "avops-canary:1.0.0", Approved: true, ApprovalRef: "本次授权试点节点上的独立验收容器；不含 RTC 业务"}}}
		data, err := yaml.Marshal(cfg)
		if err != nil {
			log.Fatal(err)
		}
		rules, _ := yaml.Marshal(cfg.RestartRules)
		files["center.yaml"] = string(data)
		files["restarts.yaml"] = string(rules)
		files["center.env"] = fmt.Sprintf("AVOPS_ADMIN_ID=rtc-qa-admin\nAVOPS_ADMIN_SECRET=%s\nAVOPS_CENTER_ADDR=0.0.0.0:29443\nAVOPS_CENTER_LOCAL_ADDR=127.0.0.1:28084\nAVOPS_CENTER_CONFIG=/etc/avops-monitor-acceptance/center.yaml\nAVOPS_DATA_DIR=/var/lib/avops-monitor-acceptance\nAVOPS_TLS_CERT=/etc/avops-monitor/server.pem\nAVOPS_TLS_KEY=/etc/avops-monitor/server-key.pem\nAVOPS_ALLOWED_IPS=111.230.192.59,111.230.108.76,127.0.0.1,::1\n", admin)
		files["agent.env"] = fmt.Sprintf("AVOPS_AGENT_ID=rtc-v1-qa\nAVOPS_AGENT_SECRET=%s\nAVOPS_CENTER_URL=wss://111.230.108.76:29443/api/v1/agent/connect\nAVOPS_TLS_SERVER_NAME=edge.rtcdevelopers.com\nAVOPS_DOCKER_SOCKET=/var/run/docker.sock\nAVOPS_COLLECT_SECONDS=10\nAVOPS_AGENT_DATA_DIR=/var/lib/avops-agent-acceptance\nAVOPS_RESTART_RULES_FILE=/etc/avops-monitor-acceptance/restarts.yaml\nAVOPS_PUBLIC_ARGS=port,listen,host,bind,log-level,config,threads\n", agent)
		files["admin-curl.conf"] = fmt.Sprintf("user = \"rtc-qa-admin:%s\"\n", admin)
	}
	// Do not overwrite an existing deployment's credentials on a repeated run.
	for name := range files {
		if _, err := os.Stat(filepath.Join(*dir, name)); !os.IsNotExist(err) {
			log.Fatal("output already exists; keep existing deployment configuration")
		}
	}
	for name, data := range files {
		f, err := os.OpenFile(filepath.Join(*dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			log.Fatal(err)
		}
		_, err = f.WriteString(data)
		if err == nil {
			err = f.Sync()
		}
		f.Close()
		if err != nil {
			log.Fatal(err)
		}
	}
	log.Print("private center/Agent configuration and curl credentials created; no secret printed")
}

func enableNotifications(path, receiverPath string) error {
	cfg, err := center.ReadConfig(path)
	if err != nil {
		return err
	}
	f, err := os.Open(receiverPath)
	if err != nil {
		return err
	}
	defer f.Close()
	values := map[string]string{}
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		key, value, ok := strings.Cut(scan.Text(), "=")
		if ok {
			values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), "\"'")
		}
	}
	if err = scan.Err(); err != nil {
		return err
	}
	if values["AVOPS_RECEIVER_ID"] == "" || values["AVOPS_RECEIVER_SECRET"] == "" {
		return fmt.Errorf("receiver credentials missing")
	}
	signingSecret := values["AVOPS_WEBHOOK_SECRET"]
	if signingSecret == "" {
		signingSecret = values["AVOPS_RECEIVER_SECRET"]
	}
	cfg.Notifications = notify.Config{AuthScheme: "webhook_hmac", Enabled: true, URL: "https://114.132.190.201:18443/api/v1/notifications", TLSServerName: "edge.rtcdevelopers.com", Secret: signingSecret, TimeoutSeconds: 10, MaxAttempts: 4, MaxPending: 1000}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".center-private-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(data); err == nil {
		err = temp.Sync()
	}
	if e := temp.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	if err = os.Rename(temp.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
