package agent

import (
	"errors"
	b "github.com/johankoi91/monitor/runtime/shared/bootstrap"
	"net"
	"net/url"
	"os"
	"path/filepath"
)

func ContainerEnvironment(environment []string) ([]string, error) {
	base := b.Map(environment)
	config := base["AVOPS_AGENT_CONFIG"]
	if config == "" {
		config = "/config/agent.env"
	}
	values, e := b.ReadEnv(config)
	if e != nil {
		return nil, e
	}
	overlay := b.Overrides(environment, "AVOPS_CENTER_URL", "AVOPS_HOST_ADDRESS", "AVOPS_COLLECT_SECONDS", "AVOPS_TLS_SERVER_NAME", "AVOPS_DOCKER_DATA_ROOT")
	merged := b.Map(b.Merge(environment, values, overlay))
	u, e := url.Parse(merged["AVOPS_CENTER_URL"])
	if e != nil || u.Scheme != "wss" || u.Hostname() == "" || u.User != nil || u.Path != "/api/v1/agent/connect" || u.RawQuery != "" || u.Fragment != "" || len(merged["AVOPS_AGENT_ENROLL_TOKEN"]) < 24 {
		return nil, errors.New("valid private Agent URL and enrollment credential required")
	}
	if host := merged["AVOPS_HOST_ADDRESS"]; host != "" && net.ParseIP(host) == nil {
		return nil, errors.New("valid reported Agent IP required")
	}
	dir := merged["AVOPS_AGENT_DATA_DIR"]
	if dir == "" {
		dir = "/state"
	}
	owner := -1
	if os.Geteuid() == 0 {
		owner = 0
	}
	if e = b.PrivateDirectory(dir, owner); e != nil {
		return nil, e
	}
	merged["AVOPS_AGENT_DATA_DIR"] = dir
	merged["AVOPS_CADVISOR_URL"] = "http://127.0.0.1:18089"
	merged["AVOPS_HOST_ROOT"] = "/host"
	rules := merged["AVOPS_RESTART_RULES_FILE"]
	if rules == "" {
		rules = filepath.Join(filepath.Dir(config), "restart-rules.yaml")
	}
	if _, e = os.Stat(rules); os.IsNotExist(e) {
		rules = ""
	} else if e != nil {
		return nil, e
	}
	if _, e = ReadRules(rules); e != nil {
		return nil, errors.New("invalid private Agent restart rules")
	}
	merged["AVOPS_RESTART_RULES_FILE"] = rules
	return b.Environment(merged), nil
}
