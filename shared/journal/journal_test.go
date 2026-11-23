package journal

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDurabilityQuotaAndSingleWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.jsonl")
	j, err := Open(path, 1024, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = j.Append(map[string]string{"stage": "accepted"}, 256); err != nil {
		t.Fatal(err)
	}
	if second, err := Open(path, 1024, nil); err == nil {
		second.Close()
		t.Fatal("second writer accepted")
	}
	if err = j.Append(map[string]string{"stage": "oversized"}, 1024); !errors.Is(err, ErrCapacity) {
		t.Fatal("quota ignored")
	}
	j.Close()
	count := 0
	j, err = Open(path, 1024, func(json.RawMessage) error { count++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if count != 1 {
		t.Fatal("failed append changed stored records")
	}
}
func TestIncompleteTailFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.jsonl")
	os.WriteFile(path, []byte(`{"accepted":true}`), 0600)
	if j, err := Open(path, 1024, nil); err == nil {
		j.Close()
		t.Fatal("incomplete tail accepted")
	}
}
