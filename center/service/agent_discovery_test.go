package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/johankoi91/monitor/runtime/shared/model"
)

func TestAgentDiscoveryEnrollmentAndReportedMetadata(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir, config())
	if err != nil {
		t.Fatal(err)
	}
	token, secret := strings.Repeat("e", 32), strings.Repeat("x", 32)
	server := httptest.NewServer((&Server{Store: store, AgentEnrollmentToken: token, AdminID: "admin", AdminSecret: strings.Repeat("z", 32)}).Handler())
	host := *fixtureHost("10.20.30.40", time.Now().UTC())
	host.HostName = "agent-reported-host"
	body, _ := json.Marshal(map[string]any{"agent_id": "discovered", "agent_secret": secret, "host_info": host})
	enroll := func(key string) *http.Response {
		request, _ := http.NewRequest("POST", server.URL+"/api/v1/agent/enroll", bytes.NewReader(body))
		request.SetBasicAuth("agent-enroll", key)
		request.Header.Set("Content-Type", "application/json")
		response, e := http.DefaultClient.Do(request)
		if e != nil {
			t.Fatal(e)
		}
		return response
	}
	denied := enroll("wrong")
	denied.Body.Close()
	if denied.StatusCode != 401 {
		t.Fatal("unauthenticated enrollment accepted")
	}
	accepted := enroll(token)
	accepted.Body.Close()
	if accepted.StatusCode != 200 || !store.AuthenticateAgent("discovered", secret) {
		t.Fatal("automatic discovery failed")
	}
	request, _ := http.NewRequest("GET", server.URL, nil)
	request.SetBasicAuth("discovered", secret)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/api/v1/agent/connect", request.Header)
	if err != nil {
		t.Fatal(err)
	}
	var plan model.Plan
	if err = conn.ReadJSON(&plan); err != nil || plan.ObservedAddress != "127.0.0.1" {
		t.Fatal("host observation plan missing", plan, err)
	}
	r := report("ap")
	r.Host = &host
	r.Containers[0].Container.HostAddress = "forged-container-host"
	if err = conn.WriteJSON(r); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(store.Containers()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	rows := store.Containers()
	if len(rows) != 1 || rows[0].HostAddress != host.HostAddress || rows[0].HostName != host.HostName || rows[0].AgentID != "discovered" {
		t.Fatal("host information did not come from Agent report", rows)
	}
	baseline, err := store.Save(model.Selection{Additions: []model.Addition{{AgentID: "discovered", ContainerID: "id-ap", SnapshotID: "snapshot-ap", ClusterCode: "rtc", ServiceCode: "ap", ServiceName: "AP"}}, Removals: []string{}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	r.Sequence = 2
	r.Host = &model.HostInfo{HostName: "renamed-host", HostAddress: "10.20.30.41", IPAddresses: []string{"10.20.30.41"}, HostnameSource: "fixture", AddressSource: "fixture", CollectedAt: time.Now().UTC()}
	if err = conn.WriteJSON(r); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for store.Baseline().Revision == baseline.Revision && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	current := store.Baseline()
	node := current.Definition.Clusters[0].Services[0].Nodes[0]
	if node.HostAddress != "10.20.30.41" || node.HostName != "renamed-host" || node.AgentID != "discovered" {
		t.Fatal("baseline host metadata not updated", node)
	}
	public, _ := json.Marshal(store.AgentSummaries())
	if bytes.Contains(public, []byte(secret)) || bytes.Contains(public, []byte("credential_hash")) {
		t.Fatal("credential exposed in discovery response")
	}
	conn.Close()
	server.Close()
	store.Close()
	restored, err := NewStore(dir, config())
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	record, ok := restored.Agent("discovered")
	if !ok || record.Host.HostAddress != "10.20.30.41" || !restored.AuthenticateAgent("discovered", secret) {
		t.Fatal("registry did not survive restart")
	}
	if err = restored.EnrollAgent("discovered", strings.Repeat("y", 32), host); err == nil {
		t.Fatal("another credential replaced Agent identity")
	}
	if err = restored.EnrollAgent("second", secret, host); err == nil {
		t.Fatal("Agent secret reused")
	}
}

func TestRetiredAgentStaysOutOfInventoryAfterRestart(t *testing.T) {
	dir := t.TempDir()
	store, err := newTestStore(dir, config())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	session := connect(t, store, "rtc-a", "agora_local_ap")
	connect(t, store, "rtc-b", "agora_local_ap")
	baseline, err := store.Save(model.Selection{Additions: []model.Addition{{AgentID: "rtc-a", ContainerID: "id-agora_local_ap", SnapshotID: "snapshot-agora_local_ap", ClusterCode: "rtc", ServiceCode: "ap", ServiceName: "AP"}}, Removals: []string{}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	code(t, store.RetireAgent("rtc-a"), "AGENT_CONNECTED")
	store.Disconnect("rtc-a", session)
	code(t, store.RetireAgent("rtc-a"), "AGENT_IN_BASELINE")
	_, err = store.Save(model.Selection{ExpectedRevision: baseline.Revision, Additions: []model.Addition{}, Removals: []string{baseline.Definition.Clusters[0].Services[0].Nodes[0].ID}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	secret := strings.Repeat("z", 32)
	handler := (&Server{Store: store, AdminID: "admin", AdminSecret: secret}).Handler()
	request := httptest.NewRequest("POST", "/api/v1/agents/rtc-a/retire", nil)
	request.SetBasicAuth("admin", secret)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("retire: %d %s", response.Code, response.Body.String())
	}
	verify := func() {
		t.Helper()
		if store.AuthenticateAgent("rtc-a", strings.Repeat("a", 32)) {
			t.Fatal("removed credentials still accepted")
		}
		if _, ok := store.Agent("rtc-b"); !ok {
			t.Fatal("other host removed")
		}
		for _, row := range store.Containers() {
			if row.AgentID == "rtc-a" {
				t.Fatal("removed host still in inventory")
			}
		}
		for _, ap := range store.MediaEdges().AccessPoints {
			if ap.AgentID == "rtc-a" {
				t.Fatal("removed host still in AP discovery")
			}
		}
	}
	verify()
	if routePermission(request.URL.Path, request.Method) != "baseline.write" {
		t.Fatal("retirement must require baseline management permission")
	}
	store.Close()
	store, err = NewStore(dir, config())
	if err != nil {
		t.Fatal(err)
	}
	verify()
}
