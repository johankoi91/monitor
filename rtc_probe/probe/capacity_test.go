package probe

import "testing"

func TestResourceCapacityIsACeilingAndRespectsCgroups(t *testing.T) {
	for _, tc := range []struct {
		cpus                  int
		quota, period, memory int64
		want                  int
	}{
		{16, 800000, 100000, 4 << 30, 8},
		{16, 400000, 100000, 4 << 30, 4},
		{16, 800000, 100000, 1 << 30, 3},
		{16, 100000, 100000, 512 << 20, 1},
		{3, 0, 0, 0, 3},
		{128, 0, 0, 0, 64},
	} {
		if got := workerCapacity(tc.cpus, tc.quota, tc.period, tc.memory); got != tc.want {
			t.Fatal(tc, got)
		}
	}
}
