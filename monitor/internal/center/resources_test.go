package center

import (
	"github.com/johankoi91/monitor/runtime/internal/model"
	"math"
	"testing"
	"time"
)

func TestResourceFailureAndAgeNeverChangeHealthOrBinding(t *testing.T) {
	s := setup(t)
	session := s.Connect("rtc-a")
	r := report("ap")
	at := time.Now().UTC()
	cpu := 0.5
	r.Containers[0].Container.Resources = &model.ResourceMetrics{Source: "cadvisor", Status: "AVAILABLE", ReasonCode: "OK", SampledAt: &at, FetchedAt: at, CPUUsageCores: &cpu, NetworkScope: "HOST_SHARED"}
	if err := s.Receive("rtc-a", session, r); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(selection("", "rtc-a"), "test"); err != nil {
		t.Fatal(err)
	}
	statuses := s.Statuses("", "").(map[string]any)["services"].([]map[string]any)
	if statuses[0]["status"] != "HEALTHY" {
		t.Fatal("metrics affected health")
	}
	r.Sequence++
	r.Containers[0].Container.Resources = &model.ResourceMetrics{Source: "cadvisor", Status: "UNAVAILABLE", ReasonCode: "CADVISOR_UNAVAILABLE", FetchedAt: at, Stale: true, NetworkScope: "UNAVAILABLE"}
	if err := s.Receive("rtc-a", session, r); err != nil {
		t.Fatal(err)
	}
	if s.Statuses("", "").(map[string]any)["services"].([]map[string]any)[0]["status"] != "HEALTHY" {
		t.Fatal("cAdvisor failure changed healthy container")
	}
	old := at.Add(-time.Minute)
	m := &model.ResourceMetrics{Source: "cadvisor", Status: "AVAILABLE", SampledAt: &old, FetchedAt: at, NetworkScope: "CONTAINER"}
	view := resourceView(m, s.agents["rtc-a"], at)
	if !view.Stale || view.ReasonCode != "RESOURCE_STALE" || m.Stale {
		t.Fatal("read freshness wrong or mutated stored snapshot")
	}
	r.Sequence++
	r.Containers[0].Container.Resources = &model.ResourceMetrics{Source: "cadvisor", Status: "AVAILABLE", FetchedAt: at, CPUUsageCores: &cpu, NetworkScope: "CONTAINER"}
	cpu = math.NaN()
	if err := s.Receive("rtc-a", session, r); err != nil {
		t.Fatal("malformed optional metrics discarded valid Docker snapshot")
	}
	if s.Containers()[0].Resources != nil {
		t.Fatal("malformed metrics retained")
	}
}
