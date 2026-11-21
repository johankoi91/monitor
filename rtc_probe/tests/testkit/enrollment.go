package testkit

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/johankoi91/monitor/rtc-probe/hub"
	"github.com/johankoi91/monitor/rtc-probe/probe"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

// ConfigureClient gives isolated acceptance tests the same server/runtime identity
// and editable-plan boundary as a deployed client, without old fixed credentials.
func ConfigureClient(h *hub.Hub, root string, plan hub.Config, driver string) (probe.Config, error) {
	config := probe.Config{HostDir: filepath.Join(root, "host"), DataDir: filepath.Join(root, "probe"), EnrollmentToken: strings.Repeat("e", 32), Driver: driver, AllowPlaintextLoopback: true}
	host, err := probe.LoadHostIdentity(config.HostDir)
	if err != nil {
		return config, err
	}
	service, client := protocol.DriverService(driver)
	id, secret := protocol.RuntimeIdentityKey(host.Secret, service, client)
	if err = h.EnrollProbe(protocol.ProbeEnrollment{Host: host.Host, HostSecret: host.Secret, RuntimeID: id, RuntimeSecret: secret, ServiceType: service, ClientType: client, Driver: driver}, "127.0.0.1"); err != nil {
		return config, err
	}
	for i := range plan.Tasks {
		plan.Tasks[i].HostID = host.Host.ID
	}
	data, err := json.Marshal(h.Snapshot())
	if err != nil {
		return config, err
	}
	var snapshot struct {
		Revision string `json:"config_revision"`
	}
	if err = json.Unmarshal(data, &snapshot); err != nil {
		return config, err
	}
	err = h.UpdatePlan(snapshot.Revision, hub.Plan{Targets: plan.Targets, Tasks: plan.Tasks})
	return config, err
}

// ServeEnrollment is only used by loopback test servers, never production handlers.
func ServeEnrollment(h *hub.Hub, w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/v1/probes/enroll" {
		return false
	}
	var input protocol.ProbeEnrollment
	if json.NewDecoder(r.Body).Decode(&input) != nil || h.EnrollProbe(input, "127.0.0.1") != nil {
		w.WriteHeader(http.StatusConflict)
		return true
	}
	_ = json.NewEncoder(w).Encode(protocol.ProbeEnrollmentReply{HostID: input.Host.ID, RuntimeID: input.RuntimeID, ObservedAddress: "127.0.0.1"})
	return true
}
