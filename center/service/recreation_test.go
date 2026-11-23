package center

import (
	"testing"
)

func TestContainerRecreationPreservesBaselineButPinsNewExecutionIdentity(t *testing.T) {
	s, session, b := operational(t)
	r := report("ap")
	r.Sequence = 2
	r.Containers[0].Container.ContainerID = "new-docker-id"
	if err := s.Receive("rtc-a", session, r); err != nil {
		t.Fatal(err)
	}
	current := s.Baseline()
	if current.Revision != b.Revision || nodeCount(current) != 1 || current.Definition.Clusters[0].Services[0].Nodes[0].ID != "rtc-a/ap" {
		t.Fatal("container ID changed logical identity")
	}
	containers := s.Containers()
	if len(containers) != 1 || !containers[0].Managed || containers[0].ContainerID != "new-docker-id" {
		t.Fatal("recreated container lost baseline membership")
	}
	req := request()
	req.NodeID = "10.0.0.1-ap"
	op, err := s.CreateRestart(req, "source", "")
	if err != nil {
		t.Fatal(err)
	}
	if op.NodeID != "rtc-a/ap" || op.Task.ContainerID != "new-docker-id" {
		t.Fatal("restart did not pin current container")
	}
	// The old ID cannot satisfy the new task's preparation checks.
	result := prepared(op)
	result.ContainerID = "id-ap"
	if err := s.ReceiveResult("rtc-a", session, result); err == nil {
		saved, _ := s.Operation(op.OperationID)
		if saved.Phase == "EXECUTE" || saved.Status == "SUCCESS" {
			t.Fatal("old container identity accepted")
		}
	}
}
