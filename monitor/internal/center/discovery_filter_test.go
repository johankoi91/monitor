package center

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/johankoi91/monitor/runtime/internal/model"
)

func TestSelectableDiscoveryFiltersBeforePagination(t *testing.T) {
	s := setup(t)
	session := s.Connect("rtc-a")
	snapshot := report("ap", "worker-a", "worker-b")
	snapshot.Containers[1].Container.RuntimeStatus = "exited"
	if err := s.Receive("rtc-a", session, snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(selection("", "rtc-a"), "fixture"); err != nil {
		t.Fatal(err)
	}
	offline := connect(t, s, "rtc-b", "offline-worker")
	s.Disconnect("rtc-b", offline)
	h := (&Server{Store: s, AdminID: "admin", AdminSecret: strings.Repeat("z", 32)}).Handler()
	get := func(query string) (int, struct {
		Total      int               `json:"total"`
		Containers []model.Container `json:"containers"`
	}) { t.Helper(); r := httptest.NewRequest("GET", "http://center/api/v1/containers?"+query, nil); r.SetBasicAuth("admin", strings.Repeat("z", 32)); w := httptest.NewRecorder(); h.ServeHTTP(w, r); var result struct {
		Total      int               `json:"total"`
		Containers []model.Container `json:"containers"`
	}; json.Unmarshal(w.Body.Bytes(), &result); return w.Code, result }
	code, first := get("selectable_only=true&page_size=1&page=1")
	if code != 200 || first.Total != 2 || len(first.Containers) != 1 || first.Containers[0].Managed || !first.Containers[0].AgentOnline || first.Containers[0].Stale {
		t.Fatal("unselectable containers affected pagination")
	}
	_, second := get("selectable_only=true&page_size=1&page=2")
	if second.Total != 2 || len(second.Containers) != 1 || second.Containers[0].ContainerID == first.Containers[0].ContainerID {
		t.Fatal("second candidate page incorrect")
	}
	_, stopped := get("selectable_only=true&runtime_status=exited")
	if stopped.Total != 1 || stopped.Containers[0].ContainerName != "worker-a" {
		t.Fatal("stopped unmanaged container excluded")
	}
	_, all := get("")
	if all.Total != 4 {
		t.Fatal("default discovery API changed")
	}
	// A new Docker ID does not turn an already managed logical node back into
	// an unregistered candidate.
	snapshot.Sequence = 2
	snapshot.Containers[0].Container.ContainerID = "recreated-ap"
	if err := s.Receive("rtc-a", session, snapshot); err != nil {
		t.Fatal(err)
	}
	_, recreated := get("selectable_only=true")
	if recreated.Total != 2 {
		t.Fatal("recreated managed node leaked into discovery")
	}
	if code, _ := get("selectable_only=invalid"); code != 400 {
		t.Fatal("invalid selector accepted")
	}
}
