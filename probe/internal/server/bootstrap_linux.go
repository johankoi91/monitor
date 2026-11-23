package server

import (
	"errors"
	b "github.com/johankoi91/monitor/runtime/shared/bootstrap"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func Bootstrap(environment []string) ([]string, error) {
	base := b.Map(environment)
	config := base["AVOPS_PROBE_CONFIG"]
	if config == "" {
		config = "/config/probe-client.env"
	}
	private, e := b.ReadEnv(config)
	if e != nil {
		return nil, e
	}
	public := b.Overrides(environment, "AVOPS_PROBE_CENTER_URL", "AVOPS_PROBE_HOST_NAME", "AVOPS_PROBE_HOST_ADDRESS", "AVOPS_PROBE_LOCATION", "AVOPS_PROBE_NATIVE_CAPACITY", "AVOPS_PROBE_WEB_CAPACITY")
	values := b.Map(b.Merge(environment, private, public))
	if values["AVOPS_PROBE_HOST_DIR"] == "" {
		values["AVOPS_PROBE_HOST_DIR"] = "/host"
	}
	if values["AVOPS_PROBE_DATA_DIR"] == "" {
		values["AVOPS_PROBE_DATA_DIR"] = "/data"
	}
	u, e := url.Parse(values["AVOPS_PROBE_CENTER_URL"])
	if e != nil || u.Scheme != "wss" || u.User != nil || u.Hostname() == "" || u.Path != "/api/v1/probes/connect" || u.RawQuery != "" || u.Fragment != "" || len(values["AVOPS_PROBE_ENROLL_TOKEN"]) < 24 {
		return nil, errors.New("valid private Probe center URL and credential required")
	}
	if host := values["AVOPS_PROBE_HOST_ADDRESS"]; host != "" && net.ParseIP(host) == nil {
		return nil, errors.New("valid Probe host IP required")
	}
	if _, e = Clients(b.Environment(values)); e != nil {
		return nil, e
	}
	if e = validateBudget(values); e != nil {
		return nil, e
	}
	identity := filepath.Join(values["AVOPS_PROBE_HOST_DIR"], "server-identity-v2.json")
	if _, e = os.Stat(identity); os.IsNotExist(e) {
		for _, kind := range []string{"native", "web"} {
			entries, err := os.ReadDir(filepath.Join(values["AVOPS_PROBE_DATA_DIR"], kind))
			if err == nil && len(entries) > 0 {
				return nil, errors.New("existing client data without server identity; restore original /host")
			}
			if err != nil && !os.IsNotExist(err) {
				return nil, err
			}
		}
	} else if e != nil {
		return nil, e
	}
	for _, path := range []string{values["AVOPS_PROBE_HOST_DIR"], values["AVOPS_PROBE_DATA_DIR"], filepath.Join(values["AVOPS_PROBE_DATA_DIR"], "native"), filepath.Join(values["AVOPS_PROBE_DATA_DIR"], "web")} {
		if e = b.PrivateDirectory(path, 65532); e != nil {
			return nil, e
		}
	}
	return b.Environment(values), nil
}
func DropPrivileges() error {
	if os.Geteuid() != 0 {
		return errors.New("Probe entrypoint must initialize as root")
	}
	if e := syscall.Setgroups([]int{}); e != nil {
		return e
	}
	if e := syscall.Setgid(65532); e != nil {
		return e
	}
	return syscall.Setuid(65532)
}
func Health() error {
	entries, e := os.ReadDir("/proc")
	if e != nil {
		return e
	}
	found := map[string]bool{}
	for _, entry := range entries {
		pid, e := strconv.Atoi(entry.Name())
		if e != nil || pid == os.Getpid() {
			continue
		}
		body, e := os.ReadFile(filepath.Join("/proc", entry.Name(), "comm"))
		if e == nil {
			found[strings.TrimSpace(string(body))] = true
		}
	}
	for _, name := range []string{"probe-server", "rtc-probe", "rtc-web-probe"} {
		if !found[name] {
			return errors.New("required Probe process unavailable")
		}
	}
	return nil
}

func validateBudget(values map[string]string) error {
	cpu := values["AVOPS_PROBE_CPU_BUDGET"]
	memory := values["AVOPS_PROBE_MEMORY_BUDGET"]
	if cpu == "" || memory == "" {
		return errors.New("Compose CPU and memory budgets required")
	}
	cpus, e := strconv.ParseFloat(cpu, 64)
	if e != nil || !(cpus > 0 && cpus <= 64) {
		return errors.New("invalid Probe CPU budget")
	}
	size := strings.ToLower(strings.TrimSpace(memory))
	size = strings.TrimSuffix(size, "b")
	factor := float64(1)
	for suffix, scale := range map[string]float64{"k": 1024, "m": 1024 * 1024, "g": 1024 * 1024 * 1024} {
		if strings.HasSuffix(size, suffix) {
			factor = scale
			size = strings.TrimSuffix(size, suffix)
			break
		}
	}
	bytes, e := strconv.ParseFloat(size, 64)
	if e != nil || !(bytes > 0) {
		return errors.New("invalid Probe memory budget")
	}
	required := float64(512 << 20)
	total := 0
	for kind, per := range map[string]int{"NATIVE": 256, "WEB": 512} {
		value := values["AVOPS_PROBE_"+kind+"_CAPACITY"]
		if value == "" {
			value = "1"
		}
		count, e := strconv.Atoi(value)
		if e != nil || count < 1 || count > 64 {
			return errors.New("invalid client capacity")
		}
		required += float64(count*per) * 1024 * 1024
		total += count
	}
	if float64(total) > cpus || required > bytes*factor {
		return errors.New("client capacities exceed the shared Compose resource budget")
	}
	return nil
}
