//go:build linux

package server

import (
	"strings"
	"testing"
)

func TestClientsSeparateCapacityStorageAndNativeLibraries(t *testing.T) {
	env := []string{"AVOPS_PROBE_CENTER_URL=wss://center.example/api/v1/probes/connect", "AVOPS_PROBE_ENROLL_TOKEN=" + strings.Repeat("s", 32), "AVOPS_PROBE_NATIVE_CAPACITY=8", "AVOPS_PROBE_WEB_CAPACITY=2", "LD_PRELOAD=foreign", "LD_LIBRARY_PATH=foreign"}
	clients, err := Clients(env)
	if err != nil {
		t.Fatal(err)
	}
	if len(clients) != 2 {
		t.Fatal("expected two service clients")
	}
	value := func(client Client, key string) string {
		for _, entry := range client.Environment {
			if strings.HasPrefix(entry, key+"=") {
				return strings.TrimPrefix(entry, key+"=")
			}
		}
		return ""
	}
	if value(clients[0], "AVOPS_PROBE_CAPACITY") != "8" || value(clients[1], "AVOPS_PROBE_CAPACITY") != "2" || value(clients[0], "AVOPS_PROBE_DATA_DIR") == value(clients[1], "AVOPS_PROBE_DATA_DIR") || value(clients[0], "AVOPS_PROBE_HOST_DIR") != value(clients[1], "AVOPS_PROBE_HOST_DIR") {
		t.Fatal("clients share execution settings or lose server identity")
	}
	if value(clients[0], "LD_LIBRARY_PATH") == "" || value(clients[1], "LD_LIBRARY_PATH") != "" || value(clients[0], "LD_PRELOAD") != "" {
		t.Fatal("native libraries leaked into browser runtime")
	}
	for _, bad := range []string{"AVOPS_PROBE_CAPACITY=auto", "RTC_PROBE_ID=old", "AVOPS_PROBE_WEB_CAPACITY=0"} {
		if _, err := Clients(append(env, bad)); err == nil {
			t.Fatal("invalid server configuration accepted", bad)
		}
	}
}
