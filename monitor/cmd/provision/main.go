// provision creates private, customer-specific configuration without printing secrets.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func secret() string {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		log.Fatal(err)
	}
	return hex.EncodeToString(value[:])
}
func main() {
	out := flag.String("out", "", "new absolute private configuration directory")
	centerURL := flag.String("center-url", "", "public HTTPS center origin, e.g. https://monitor.example.com:19443")
	siteCode := flag.String("site-code", "customer", "customer site code")
	siteName := flag.String("site-name", "客户监控", "customer site name")
	hostAddress := flag.String("agent-host-address", "", "optional reported Agent IP")
	flag.Parse()
	origin, err := url.Parse(*centerURL)
	if err != nil || origin.Scheme != "https" || origin.Hostname() == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" && origin.Path != "/" {
		log.Fatal("valid HTTPS center origin required")
	}
	if !filepath.IsAbs(*out) || !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`).MatchString(*siteCode) || strings.TrimSpace(*siteName) == "" || strings.ContainsAny(*siteName, "\r\n") {
		log.Fatal("absolute output directory and valid site required")
	}
	if *hostAddress != "" && net.ParseIP(*hostAddress) == nil {
		log.Fatal("agent-host-address must be an IP")
	}
	if err = os.Mkdir(*out, 0700); err != nil {
		log.Fatal("new private output directory required")
	}
	admin, agentToken, probeToken, receiverSecret, webhookSecret := secret(), secret(), secret(), secret(), secret()
	endpoint := func(path string) string {
		value := *origin
		value.Scheme = "wss"
		value.Path = path
		return value.String()
	}
	files := map[string]string{
		"center.env":         fmt.Sprintf("AVOPS_ADMIN_ID=system-admin\nAVOPS_ADMIN_SECRET=%s\nAVOPS_AGENT_ENROLL_TOKEN=%s\nAVOPS_PROBE_ENROLL_TOKEN=%s\nAVOPS_CENTER_ADDR=0.0.0.0:19443\nAVOPS_CENTER_LOCAL_ADDR=127.0.0.1:18084\nAVOPS_ALLOWED_IPS=\n", admin, agentToken, probeToken),
		"center.yaml":        fmt.Sprintf("site:\n  code: %s\n  name: %q\nrtc_probe:\n  max_concurrent_runs: 16\n", *siteCode, *siteName),
		"agent.env":          fmt.Sprintf("AVOPS_AGENT_ENROLL_TOKEN=%s\nAVOPS_CENTER_URL=%s\nAVOPS_HOST_ADDRESS=%s\nAVOPS_DOCKER_SOCKET=/var/run/docker.sock\nAVOPS_COLLECT_SECONDS=15\n", agentToken, endpoint("/api/v1/agent/connect"), *hostAddress),
		"probe-client.env":   fmt.Sprintf("AVOPS_PROBE_CENTER_URL=%s\nAVOPS_PROBE_ENROLL_TOKEN=%s\nAVOPS_PROBE_HOST_DIR=/host\nAVOPS_PROBE_DATA_DIR=/data\nAVOPS_PROBE_NATIVE_CAPACITY=1\nAVOPS_PROBE_WEB_CAPACITY=1\n", endpoint("/api/v1/probes/connect"), probeToken),
		"restart-rules.yaml": "[]\n",
		"receiver.env":       fmt.Sprintf("AVOPS_RECEIVER_ID=monitor-admin\nAVOPS_RECEIVER_SECRET=%s\nAVOPS_WEBHOOK_SECRET=%s\nAVOPS_RECEIVER_ADDR=0.0.0.0:18443\n", receiverSecret, webhookSecret),
		"admin-curl.conf":    fmt.Sprintf("user = \"system-admin:%s\"\n", admin),
	}
	for name, data := range files {
		path := filepath.Join(*out, name)
		if err = os.WriteFile(path, []byte(data), 0600); err != nil {
			log.Fatal("private configuration write failed")
		}
	}
	log.Print("V1.0 private site configuration created; no credentials printed")
}
