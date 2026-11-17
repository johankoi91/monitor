package protocol

import (
	"encoding/json"
	"testing"
)

func TestMediaEdgeRangeValidationAndSingleVOSParameters(t *testing.T) {
	r := MediaEdgeRange{Host: "111.230.192.59", StartPort: 4001, EndPort: 4008}
	if err := r.Normalize(); err != nil {
		t.Fatal(err)
	}
	endpoints := r.Endpoints()
	if len(endpoints) != 8 || endpoints[0] != "111.230.192.59:4001" || endpoints[7] != "111.230.192.59:4008" {
		t.Fatal(endpoints)
	}
	for _, endpoint := range endpoints {
		value, err := VOSParameters(endpoint)
		if err != nil {
			t.Fatal(err)
		}
		var parsed map[string][]string
		if err = json.Unmarshal([]byte(value), &parsed); err != nil {
			t.Fatal(err)
		}
		if len(parsed) != 1 || len(parsed["rtc.vos_list"]) != 1 || parsed["rtc.vos_list"][0] != endpoint {
			t.Fatal("SDK should receive exactly one selected edge", value)
		}
	}
	for _, bad := range []MediaEdgeRange{{Host: "https://111.230.192.59", StartPort: 4001, EndPort: 4008}, {Host: "111.230.192.59:4001", StartPort: 4001, EndPort: 4008}, {Host: "111.230.192.59", StartPort: 4008, EndPort: 4001}, {Host: "111.230.192.59", StartPort: 0, EndPort: 8}, {Host: "111.230.192.59", StartPort: 65535, EndPort: 65536}, {Host: "111.230.192.59", StartPort: 4001, EndPort: 4065}} {
		if err := bad.Normalize(); err == nil {
			t.Fatal("invalid range accepted", bad)
		}
	}
	if r.Contains("111.230.192.59:4009") || r.Contains("111.230.192.60:4001") {
		t.Fatal("out-of-range endpoint accepted")
	}
}
