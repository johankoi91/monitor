package hub

import (
	"testing"
	"time"
)

func TestHistoryFiltersApplyBeforePagination(t *testing.T) {
	archive := &fileArchive{dir: t.TempDir()}
	if err := archive.RTCInit(); err != nil {
		t.Fatal(err)
	}
	for i, v := range []Run{{ID: "native-pass", TaskID: "one", Driver: "agora", Status: "SUCCESS"}, {ID: "web-fail", TaskID: "two", Driver: "agora-web", Status: "FAILED"}, {ID: "web-pass", TaskID: "two", Driver: "agora-web", Status: "SUCCESS"}, {ID: "interop-udp", TaskID: "three", Driver: "agora", ClientType: "interop", Status: "SUCCESS"}, {ID: "interop-aut", TaskID: "four", Driver: "agora", ClientType: "interop_aut", Status: "SUCCESS"}, {ID: "web-child", TaskID: "two", Driver: "agora-web", Status: "FAILED", ParentRunID: "web-fail"}} {
		v.CreatedAt = time.Now().Add(time.Duration(i) * time.Second)
		if err := archive.RTCPutRun(v, 1<<20); err != nil {
			t.Fatal(err)
		}
	}
	page, err := archive.RTCListFilteredRuns(HistoryFilter{ClientType: "web"}, 1, 1)
	if err != nil || page.Total != 2 || len(page.Runs) != 1 || page.Runs[0].ID != "web-fail" {
		t.Fatal("filter/pagination mismatch", page, err)
	}
	page, err = archive.RTCListFilteredRuns(HistoryFilter{TaskID: "two", ClientType: "web", Status: "FAILED"}, 10, 0)
	if err != nil || page.Total != 1 || len(page.Runs) != 1 || page.Runs[0].ID != "web-fail" {
		t.Fatal("combined filter or child exclusion failed", page, err)
	}
	page, err = archive.RTCListFilteredRuns(HistoryFilter{ClientType: "native"}, 10, 0)
	if err != nil || page.Total != 1 || page.Runs[0].ID != "native-pass" {
		t.Fatal("native filter failed", page, err)
	}
	for _, client := range []string{"interop", "interop_aut"} {
		page, err = archive.RTCListFilteredRuns(HistoryFilter{ClientType: client}, 10, 0)
		want := 1
		if client == "interop" {
			want = 2
		}
		if err != nil || page.Total != want || (client == "interop_aut" && page.Runs[0].ClientType != client) {
			t.Fatal("mixed interoperability transports", client, page, err)
		}
	}
}
