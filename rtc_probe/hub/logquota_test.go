package hub

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDiagnosticQuotaReclaimsOnlyRTCArchives(t *testing.T) {
	h, err := New(t.TempDir(), testConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	h.state.Settings.LogsMB = 16
	create := func(path string, size int64, age time.Duration) {
		t.Helper()
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.Truncate(size); err != nil {
			t.Fatal(err)
		}
		f.Close()
		at := time.Now().Add(-age)
		os.Chtimes(path, at, at)
	}
	old := filepath.Join(h.dir, "logs", "old.zip")
	recent := filepath.Join(h.dir, "logs", "recent.zip")
	ledger := filepath.Join(h.dir, "v1-ledger.zip")
	create(old, 8<<20, 2*time.Hour)
	create(recent, 8<<20, time.Hour)
	create(ledger, 8<<20, 24*time.Hour)
	if err = h.reserveDiagnostic(4<<20, "incoming"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old RTC archive not reclaimed")
	}
	for _, path := range []string{recent, ledger} {
		if _, err = os.Stat(path); err != nil {
			t.Fatal("quota cleanup deleted unrelated or newer data", path)
		}
	}
}
