package agoralogs

import (
	"github.com/johankoi91/monitor/runtime/shared/model"
	"reflect"
	"testing"
	"time"
)

func TestScriptBuckets(t *testing.T) {
	for _, tc := range []struct {
		mode, input string
		want        []string
	}{
		{"point", "20260522001500", []string{"20260521-21", "20260522-00"}},
		{"point", "2026-05-22T10:30:00+08:00", []string{"20260522-00", "20260522-03"}},
		{"point", "2026-05-22T01:30:00Z", []string{"20260522-00"}},
		{"day", "20260228", []string{"20260228-00", "20260228-03", "20260228-06", "20260228-09", "20260228-12", "20260228-15", "20260228-18", "20260228-21"}},
	} {
		t.Run(tc.input, func(t *testing.T) {
			tags, _, err := Buckets(tc.mode, tc.input, time.UTC)
			if err != nil || !reflect.DeepEqual(tags, tc.want) {
				t.Fatalf("%v, %v; want %v", tags, err, tc.want)
			}
		})
	}
	for _, input := range []string{"1747894980", "1747894980123"} {
		at, err := ParsePoint(input, time.UTC)
		if err != nil || at.Unix() != 1747894980 {
			t.Fatalf("%s: %v %v", input, at, err)
		}
	}
	local := time.FixedZone("test", 8*3600)
	at, err := ParsePoint("2026-05-22 10:30:00", local)
	if err != nil || at.Hour() != 2 {
		t.Fatalf("local parsing: %v %v", at, err)
	}
	for _, input := range []string{"20260229", "20261301", "2026052", "invalid"} {
		if _, _, err := Buckets("day", input, time.UTC); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}

func TestServiceFilenameMatchesBothProcessNameAndExactPID(t *testing.T) {
	services := []model.LogService{
		{Processes: []model.ContainerProcess{{PID: 30543, Name: "media_edge_1"}}},
		{Processes: []model.ContainerProcess{{PID: 123, Name: "local_ap.exe"}}},
	}
	for name, want := range map[string]bool{
		"media_edge_1.log.DEBUG.20261117-09.30543":  true,
		"media_edge_1.log.DEBUG.20261117-09.30544":  false,
		"media_edge_2.log.DEBUG.20261117-09.30543":  false,
		"media_edge_1.log.DEBUG.20261117-09.305430": false,
		"local_ap.exe.log.DEBUG.20261117-09.123":    true,
		"media_edge_1.log.INFO.20261117-09.30543":   false,
	} {
		if got := MatchesServices(name, services); got != want {
			t.Errorf("%s: %v", name, got)
		}
	}
}
