package center

import "testing"

func TestNotificationNodeUsesHostIPAndPreservesInternalIdentity(t *testing.T) {
	s := setup(t)
	connect(t, s, "rtc-a", "ap")
	connect(t, s, "rtc-b", "ap")
	baseline, err := s.Save(selection("", "rtc-a", "rtc-b"), "test")
	if err != nil {
		t.Fatal(err)
	}
	_, facts := s.NotificationFacts()
	if len(facts) != 2 {
		t.Fatalf("expected only two node facts, got %d", len(facts))
	}
	seen := map[string]bool{}
	for _, f := range facts {
		if f.Node == "" {
			t.Fatal("service aggregate notification still generated")
		}
		if f.Node != "" {
			seen[f.Cluster+"/"+f.Node] = true
		}
	}
	if !seen["rtc-pilot/10.0.0.1-ap"] || !seen["rtc-pilot/10.0.0.2-ap"] {
		t.Fatalf("wrong external node identities: %v", seen)
	}
	if baseline.Definition.Clusters[0].Services[0].Nodes[0].ID != "rtc-a/ap" {
		t.Fatal("notification change rewrote internal baseline identity")
	}
}
