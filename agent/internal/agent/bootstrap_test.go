package agent

import (
	b "github.com/johankoi91/monitor/runtime/shared/bootstrap"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContainerBootstrapKeepsIdentityAndUsesPrivateToken(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "agent.env")
	os.WriteFile(config, []byte("AVOPS_CENTER_URL=wss://center.example/api/v1/agent/connect\nAVOPS_AGENT_ENROLL_TOKEN="+strings.Repeat("s", 32)+"\n"), 0600)
	data := filepath.Join(root, "data")
	os.Mkdir(data, 0700)
	os.WriteFile(filepath.Join(data, "agent-identity.json"), []byte("existing-identity"), 0600)
	env, e := ContainerEnvironment([]string{"AVOPS_AGENT_CONFIG=" + config, "AVOPS_AGENT_DATA_DIR=" + data, "AVOPS_HOST_ADDRESS=192.0.2.2"})
	if e != nil {
		t.Fatal(e)
	}
	values := b.Map(env)
	if values["AVOPS_HOST_ADDRESS"] != "192.0.2.2" || len(values["AVOPS_AGENT_ENROLL_TOKEN"]) != 32 || values["AVOPS_RESTART_RULES_FILE"] != "" {
		t.Fatal("container bootstrap configuration mismatch")
	}
	body, _ := os.ReadFile(filepath.Join(data, "agent-identity.json"))
	if string(body) != "existing-identity" {
		t.Fatal("bootstrap replaced existing identity")
	}
}
