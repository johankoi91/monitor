package hub

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var errDiagnosticQuota = errors.New("diagnostic archive budget exhausted")

func (h *Hub) reserveDiagnostic(bytes int64, current string) error {
	entries, err := os.ReadDir(filepath.Join(h.dir, "logs"))
	if err != nil {
		return err
	}
	type file struct {
		path string
		size int64
		at   time.Time
	}
	var files []file
	var total int64
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".zip") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.Name() == current+".zip" {
			continue
		}
		total += info.Size()
		files = append(files, file{filepath.Join(h.dir, "logs", entry.Name()), info.Size(), info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].at.Before(files[j].at) })
	budget := int64(h.state.Settings.LogsMB) << 20
	for i, f := range files {
		if total+bytes <= budget {
			break
		}
		if i >= 100 {
			break
		}
		if err := os.Remove(f.path); err != nil {
			return err
		}
		total -= f.size
	}
	if total+bytes > budget {
		return errDiagnosticQuota
	}
	return nil
}
