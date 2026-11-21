package center

import (
	"testing"

	"github.com/johankoi91/monitor/runtime/internal/model"
)

func TestAutomaticServiceMappingAndLegacyAssignmentPreservation(t *testing.T) {
	s := setup(t)
	connect(t, s, "rtc-a", "ap", "agora_local_ap")
	v := selection("", "rtc-a")
	v.Additions[0].ServiceName = "已保存的服务归属"
	b, err := s.Save(v, "source")
	if err != nil {
		t.Fatal(err)
	}
	v = selection(b.Revision, "rtc-a")
	v.Removals = []string{"rtc-a/ap"}
	v.Additions[0].ServiceCode = ""
	v.Additions[0].ServiceName = ""
	v.Additions[0].ClusterName = ""
	b, err = s.Save(v, "source")
	if err != nil {
		t.Fatal(err)
	}
	if b.Definition.Clusters[0].Services[0].Name != "已保存的服务归属" {
		t.Fatal("legacy assignment changed")
	}
	v = model.Selection{ExpectedRevision: b.Revision, Additions: []model.Addition{{AgentID: "rtc-a", ContainerID: "id-agora_local_ap", SnapshotID: "snapshot-agora_local_ap", ClusterCode: "new-cluster"}}, Removals: []string{}}
	b, err = s.Save(v, "source")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range b.Definition.Clusters {
		for _, service := range c.Services {
			if c.Code == "new-cluster" && service.Code == "rtc-ap" && service.Name == "RTC AP" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("automatic service missing")
	}
}
func TestMultipleChecksUpdatePreservesMembershipAndPermissions(t *testing.T) {
	s := setup(t)
	connect(t, s, "rtc-a", "ap")
	b, err := s.Save(selection("", "rtc-a"), "source")
	if err != nil {
		t.Fatal(err)
	}
	checks := []model.Check{{Type: "tcp", Host: "127.0.0.1", Port: 443, TimeoutMS: 2000}, {Type: "tcp", Host: "127.0.0.1", Port: 8002, TimeoutMS: 2000}}
	b, err = s.UpdateChecks(b.Revision, "rtc-a/ap", "source", checks)
	if err != nil {
		t.Fatal(err)
	}
	node := b.Definition.Clusters[0].Services[0].Nodes[0]
	if nodeCount(b) != 1 || len(node.Checks) != 2 || node.RestartEnabled {
		t.Fatal("port edit changed baseline membership or permission")
	}
	_, err = s.UpdateChecks("stale", "rtc-a/ap", "source", checks)
	code(t, err, "BASELINE_CONFLICT")
	_, err = s.UpdateChecks(b.Revision, "rtc-a/ap", "source", append(checks, checks[0]))
	code(t, err, "DUPLICATE_CHECK")
}
func TestBusyNodeCannotChangeChecks(t *testing.T) {
	s, _, b := operational(t)
	if _, err := s.CreateRestart(request(), "source", ""); err != nil {
		t.Fatal(err)
	}
	_, err := s.UpdateChecks(b.Revision, "rtc-a/ap", "source", []model.Check{})
	code(t, err, "OPERATION_IN_PROGRESS")
}

func TestCleanupEmptyGroupsPreservesMembersAndUsesRevision(t *testing.T) {
	s := setup(t)
	connect(t, s, "rtc-a", "ap")
	before, err := s.Save(selection("", "rtc-a"), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	s.record.Baseline.Definition.Clusters[0].Services = append(s.record.Baseline.Definition.Clusters[0].Services, model.Service{Code: "empty-service", Name: "Empty"})
	s.record.Baseline.Definition.Clusters = append(s.record.Baseline.Definition.Clusters, model.Cluster{Code: "empty-group", Name: "Empty", Product: "RTC"})
	_, err = s.CleanupEmptyGroups("stale", "fixture")
	code(t, err, "BASELINE_CONFLICT")
	after, err := s.CleanupEmptyGroups(before.Revision, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if nodeCount(after) != 1 || len(after.Definition.Clusters) != 1 || len(after.Definition.Clusters[0].Services) != 1 || after.Revision == before.Revision {
		t.Fatal("cleanup changed membership or retained empty groups")
	}
	unchanged, err := s.CleanupEmptyGroups(after.Revision, "fixture")
	if err != nil || unchanged.Revision != after.Revision {
		t.Fatal("cleanup not idempotent", err)
	}
	after, err = s.Save(model.Selection{ExpectedRevision: after.Revision, Additions: []model.Addition{}, Removals: []string{"rtc-a/ap"}}, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Definition.Clusters) != 0 {
		t.Fatal("last removal left an empty group")
	}
}
