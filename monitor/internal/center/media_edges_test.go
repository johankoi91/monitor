package center

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/johankoi91/monitor/runtime/internal/model"
)

func TestMediaEdgeArguments(t *testing.T) {
	valid := []string{"--ap", "172.16.80.25", "--ip-for-client", "111.230.192.59", "--user-port", "4001"}
	for _, tc := range []struct {
		name   string
		config model.StartupConfig
		reason string
	}{
		{name: "argv", config: model.StartupConfig{Args: valid}},
		{name: "equals", config: model.StartupConfig{Args: []string{"--ap=172.16.80.25", "--ip-for-client=111.230.192.59", "--user-port=4001"}}},
		{name: "shell", config: model.StartupConfig{Path: "/bin/sh", Args: []string{"-c", `exec media_edge --ap '172.16.80.25' --ip-for-client="111.230.192.59" --user-port 4001`}}},
		{name: "cmd fallback", config: model.StartupConfig{Entrypoint: []string{"media_edge"}, Cmd: valid}},
		{name: "actual argv wins", config: model.StartupConfig{Args: valid, Cmd: []string{"--ap", "10.0.0.1"}}},
		{name: "equal repeated flag", config: model.StartupConfig{Args: append(append([]string(nil), valid...), "--ap=172.16.80.25")}},
		{name: "conflicting repeated flag", config: model.StartupConfig{Args: append(append([]string(nil), valid...), "--ap=10.0.0.1")}, reason: "AMBIGUOUS_ARGUMENT"},
		{name: "flag boundary", config: model.StartupConfig{Args: []string{"--ap-extra=172.16.80.25", "--ip-for-client=111.230.192.59", "--user-port=4001"}}, reason: "MISSING_OR_INVALID_AP"},
		{name: "missing value", config: model.StartupConfig{Args: []string{"--ap", "--user-port=4001"}}, reason: "MISSING_ARGUMENT_VALUE"},
		{name: "invalid port", config: model.StartupConfig{Args: []string{"--ap=172.16.80.25", "--ip-for-client=111.230.192.59", "--user-port=70000"}}, reason: "MISSING_OR_INVALID_USER_PORT"},
		{name: "invalid client ip", config: model.StartupConfig{Args: []string{"--ap=172.16.80.25", "--ip-for-client=$CLIENT_IP", "--user-port=4001"}}, reason: "MISSING_OR_INVALID_CLIENT_IP"},
		{name: "malformed shell", config: model.StartupConfig{Path: "/bin/sh", Args: []string{"-c", `media_edge --ap "172.16.80.25`}}, reason: "INVALID_COMMAND"},
		{name: "shell pipeline", config: model.StartupConfig{Path: "/bin/sh", Args: []string{"-c", `echo --ap 10.0.0.1; exec media_edge --ap 172.16.80.25`}}, reason: "INVALID_COMMAND"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ap, ip, port, err := mediaEdgeArguments(tc.config)
			if tc.reason != "" {
				if err == nil || err.Error() != tc.reason {
					t.Fatal("unexpected parse result", ap, ip, port, err)
				}
				return
			}
			if err != nil || ap != "172.16.80.25" || ip != "111.230.192.59" || port != 4001 {
				t.Fatal(ap, ip, port, err)
			}
		})
	}
}
func edgeReport(name, ap, ip, port string) model.Report {
	value := report(name)
	value.Containers[0].Startup.Config.Args = []string{"--ap", ap, "--ip-for-client", ip, "--user-port", port}
	return value
}
func TestMediaEdgeMapGroupsAllHostsAndReplacesReportsWithoutDuplicates(t *testing.T) {
	store := setup(t)
	a, b := store.Connect("rtc-a"), store.Connect("rtc-b")
	first := edgeReport("agora_udp_media_edge_1", "172.16.80.25", "111.230.192.59", "4001")
	second := edgeReport("agora_web_media_edge_2", "172.16.80.25", "111.230.192.60", "4502")
	if err := store.Receive("rtc-a", a, first); err != nil {
		t.Fatal(err)
	}
	second.Host = fixtureHost("10.0.0.2", second.CollectedAt)
	if err := store.Receive("rtc-b", b, second); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		value := store.MediaEdges()
		if len(value.ByAP["172.16.80.25"]) != 2 || value.MatchedContainers != 2 || len(value.Skipped) != 0 || !reflect.DeepEqual(value.Hosts, []string{"10.0.0.1", "10.0.0.2"}) {
			t.Fatal("map missed unmanaged container or host", value)
		}
	}
	first.Sequence = 2
	first.Containers[0].Startup.Config.Args = []string{"--ap=172.16.80.26", "--ip-for-client=111.230.192.59", "--user-port=4002"}
	if err := store.Receive("rtc-a", a, first); err != nil {
		t.Fatal(err)
	}
	value := store.MediaEdges()
	if len(value.ByAP["172.16.80.25"]) != 1 || len(value.ByAP["172.16.80.26"]) != 1 || value.ByAP["172.16.80.26"][0].UserPort != 4002 {
		t.Fatal("old endpoint accumulated", value)
	}
	// Periodic observations omit the large startup payload; keep its last valid data.
	first.Sequence = 3
	first.Containers[0].Startup = nil
	if err := store.Receive("rtc-a", a, first); err != nil {
		t.Fatal(err)
	}
	if len(store.MediaEdges().ByAP["172.16.80.26"]) != 1 {
		t.Fatal("periodic omission removed parameters")
	}
	store.Disconnect("rtc-b", b)
	if !store.MediaEdges().ByAP["172.16.80.25"][0].Stale {
		t.Fatal("offline endpoint presented as fresh")
	}
	// A recreated container must not inherit its predecessor's startup arguments.
	first.Sequence = 4
	first.Containers[0].Container.ContainerID = "replacement"
	first.Containers[0].Container.SnapshotID = "replacement-snapshot"
	if err := store.Receive("rtc-a", a, first); err != nil {
		t.Fatal(err)
	}
	value = store.MediaEdges()
	if len(value.ByAP["172.16.80.26"]) != 0 || len(value.Skipped) != 1 || value.Skipped[0].Reason != "STARTUP_IDENTITY_MISMATCH" {
		t.Fatal("old identity reused", value)
	}
	// Removing a container from discovery removes it from the grouped map.
	empty := report()
	empty.Sequence = 5
	if err := store.Receive("rtc-a", a, empty); err != nil {
		t.Fatal(err)
	}
	if store.MediaEdges().MatchedContainers != 1 {
		t.Fatal("removed container retained")
	}
}
func TestMediaEdgeMapHTTPFiltersAndDoesNotExposeOtherArguments(t *testing.T) {
	store := setup(t)
	session := store.Connect("rtc-a")
	value := edgeReport("agora_aut_media_edge_1", "172.16.80.25", "111.230.192.59", "4701")
	value.Containers[0].Startup.Config.Args = append(value.Containers[0].Startup.Config.Args, "--password", "private-cli-secret")
	value.Containers = append(value.Containers, report("unrelated").Containers[0])
	// Align timestamps with the containing report so its startup record is valid.
	value.Containers[1].Container.CollectedAt = value.CollectedAt
	if err := store.Receive("rtc-a", session, value); err != nil {
		t.Fatal(err)
	}
	server := (&Server{Store: store, AdminID: "admin", AdminSecret: strings.Repeat("x", 32)}).Handler()
	for _, tc := range []struct {
		method string
		auth   bool
		status int
	}{{"GET", false, 401}, {"POST", true, 405}, {"GET", true, 200}} {
		request := httptest.NewRequest(tc.method, "/api/v1/containers/media-edges", nil)
		if tc.auth {
			request.SetBasicAuth("admin", strings.Repeat("x", 32))
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != tc.status {
			t.Fatal(tc, response.Code, response.Body.String())
		}
		if tc.status == 200 {
			var inventory model.MediaEdgeInventory
			if err := json.Unmarshal(response.Body.Bytes(), &inventory); err != nil {
				t.Fatal(err)
			}
			if len(inventory.ByAP["172.16.80.25"]) != 1 || inventory.MatchedContainers != 1 || strings.Contains(response.Body.String(), "private-cli-secret") {
				t.Fatal("incorrect projection", response.Body.String())
			}
		}
	}
	if routePermission("/api/v1/containers/media-edges", http.MethodGet) != "inventory.read" {
		t.Fatal("incorrect inventory permission")
	}
}

func TestMediaEdgeMultipleAPGroups(t *testing.T) {
	store := setup(t)
	session := store.Connect("rtc-a")
	value := edgeReport("agora_udp_media_edge_1", "172.17.80.158,172.17.80.111,172.17.80.158", "183.131.59.146", "4001")
	if err := store.Receive("rtc-a", session, value); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		discovered := store.MediaEdges()
		if discovered.MatchedContainers != 1 || len(discovered.Skipped) != 0 || len(discovered.ByAP) != 2 {
			t.Fatal(discovered)
		}
		for _, group := range []string{"172.17.80.111", "172.17.80.158"} {
			if len(discovered.ByAP[group]) != 1 || discovered.ByAP[group][0].IPForClient != "183.131.59.146" {
				t.Fatal(discovered)
			}
		}
	}
	for _, bad := range []string{"172.17.80.158,invalid", "172.17.80.158,"} {
		if _, _, _, err := mediaEdgeArguments(model.StartupConfig{Args: []string{"--ap", bad, "--ip-for-client", "183.131.59.146", "--user-port", "4001"}}); err == nil {
			t.Fatal("accepted partial AP list", bad)
		}
	}
}

func TestAPDiscoveryUsesLocalAPNameAndReportedNodeAddress(t *testing.T) {
	store := setup(t)
	a, b := store.Connect("rtc-a"), store.Connect("rtc-b")
	ap := report("agora_local_ap")
	ap.Host = fixtureHost("111.230.192.59", ap.CollectedAt)
	ap.Host.IPAddresses = []string{"111.230.192.59", "172.16.80.25"}
	edge := edgeReport("agora_udp_media_edge_1", "172.16.80.25", "183.131.59.146", "4001")
	edge.Host = fixtureHost("183.131.59.146", edge.CollectedAt)
	helper := report("agora_local_ap_helper").Containers[0]
	helper.Container.CollectedAt = edge.CollectedAt
	helper.Startup.CollectedAt = edge.CollectedAt
	edge.Containers = append(edge.Containers, helper)
	if err := store.Receive("rtc-a", a, ap); err != nil {
		t.Fatal(err)
	}
	if err := store.Receive("rtc-b", b, edge); err != nil {
		t.Fatal(err)
	}
	inventory := store.MediaEdges()
	if len(inventory.AccessPoints) != 1 {
		t.Fatal("Edge-only/helper host classified as AP", inventory.AccessPoints)
	}
	point := inventory.AccessPoints[0]
	if point.AgentID != "rtc-a" || point.Address != "111.230.192.59" || !reflect.DeepEqual(point.Groups, []string{"172.16.80.25"}) {
		t.Fatal(point)
	}
	native, err := store.NativeInventory()
	if err != nil || len(native.AccessPoints) != 1 || native.Groups["172.16.80.25"][0].Endpoint != "183.131.59.146:4001" {
		t.Fatal(native, err)
	}
	ap.Sequence = 2
	ap.Host = fixtureHost("111.230.192.60", ap.CollectedAt)
	ap.Host.IPAddresses = []string{"111.230.192.60", "172.16.80.25"}
	if err = store.Receive("rtc-a", a, ap); err != nil {
		t.Fatal(err)
	}
	if store.MediaEdges().AccessPoints[0].Address != "111.230.192.60" {
		t.Fatal("AP public address did not follow Agent report")
	}
	store.Disconnect("rtc-a", a)
	if !store.MediaEdges().AccessPoints[0].Stale {
		t.Fatal("offline AP presented as fresh")
	}
}
