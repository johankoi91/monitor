package cadvisor

import "testing"

func TestDiskRatesDistinguishMissingFromMeasuredZero(t *testing.T) {
	zero := uint64(0)
	old := uint64(10)
	reset := uint64(1)
	for _, tc := range []struct {
		name          string
		before, after *uint64
		valid         bool
	}{
		{"missing", nil, nil, false}, {"missing baseline", nil, &zero, false}, {"counter reset", &old, &reset, false}, {"real zero", &zero, &zero, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := func(v *uint64) []deviceStats {
				return []deviceStats{{Major: 8, Minor: 0, Stats: map[string]*uint64{"Read": v, "Write": v}}}
			}
			got := diskDevices(d(tc.before), d(tc.after), nil, nil, 5)
			if tc.valid {
				if len(got) != 1 || got[0].ReadBPS == nil || *got[0].ReadBPS != 0 {
					t.Fatal("measured zero lost")
				}
			} else if len(got) != 0 {
				t.Fatal("missing/reset counter rendered as zero")
			}
		})
	}
}
