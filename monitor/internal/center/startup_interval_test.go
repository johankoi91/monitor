package center

import (
	"testing"
	"time"
)

func TestStartupAgeUsesParameterPeriodInsteadOfHealthPeriod(t *testing.T) {
	s := setup(t)
	connect(t, s, "rtc-a", "ap")
	if _, err := s.Save(selection("", "rtc-a"), "test"); err != nil {
		t.Fatal(err)
	}
	stale := func() bool {
		return s.StatusesWithStartup("", "").(map[string]any)["services"].([]map[string]any)[0]["nodes"].([]map[string]any)[0]["startup_record"].(map[string]any)["stale"].(bool)
	}
	age := func(d time.Duration) {
		s.mu.Lock()
		r := s.lastStartups["rtc-a"]["ap"]
		r.ReportedAt = time.Now().Add(-d)
		s.lastStartups["rtc-a"]["ap"] = r
		s.mu.Unlock()
	}
	age(9 * time.Minute)
	if stale() {
		t.Fatal("normal parameters marked stale between 10 minute reports")
	}
	age(11*time.Minute + time.Second)
	if !stale() {
		t.Fatal("overdue parameters not marked historical")
	}
	age(time.Minute)
	s.mu.Lock()
	s.agents["rtc-a"].Report.Containers[0].Container.SnapshotID = "changed-config"
	s.mu.Unlock()
	if !stale() {
		t.Fatal("old configuration attributed to current configuration")
	}
}
