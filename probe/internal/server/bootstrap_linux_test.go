package server

import (
	"testing"
)

func TestComposeBudgetRejectsOversubscribedClients(t *testing.T) {
	values := map[string]string{"AVOPS_PROBE_CPU_BUDGET": "2", "AVOPS_PROBE_MEMORY_BUDGET": "2048m", "AVOPS_PROBE_NATIVE_CAPACITY": "8", "AVOPS_PROBE_WEB_CAPACITY": "2"}
	if e := validateBudget(values); e == nil {
		t.Fatal("oversubscribed CPU accepted")
	}
	values["AVOPS_PROBE_CPU_BUDGET"] = "10"
	if e := validateBudget(values); e == nil {
		t.Fatal("oversubscribed memory accepted")
	}
	values["AVOPS_PROBE_MEMORY_BUDGET"] = "6g"
	if e := validateBudget(values); e != nil {
		t.Fatal(e)
	}
}
