package probe

import (
	"errors"
	"github.com/johankoi91/monitor/runtime/shared/protocol"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

func (c *Client) resourceStatus() string { c.mu.Lock(); defer c.mu.Unlock(); return c.resourceError }
func (c *Client) storageSize() int64     { c.mu.Lock(); defer c.mu.Unlock(); return c.storageBytes }
func reserveFor(job protocol.Job) int64 {
	reserve := int64(32 << 20)
	if job.RTC != nil {
		reserve += int64(job.RTC.LogSizeKB) * 1024 * 6
	}
	return reserve
}

type retainedRun struct {
	id                string
	bytes             int64
	at                time.Time
	eligible, success bool
	known             bool
}

// The lock protects whole-run deletion against worker start and log collection.
// Unacknowledged results/events are never reclaimed to make space.
func (c *Client) pruneLocked(now time.Time, reserve int64) error {
	policy := c.retention
	if policy.DiskMB == 0 {
		policy = protocol.DefaultRetention()
	}
	root := filepath.Join(c.config.DataDir, "runs")
	runs := map[string]*retainedRun{}
	var total int64
	for _, name := range []string{"outbox.json", "retention.json"} {
		if info, err := os.Stat(filepath.Join(c.config.DataDir, name)); err == nil {
			total += info.Size()
		}
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		id := strings.Split(rel, string(filepath.Separator))[0]
		if !protocol.ValidID(id) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		run := runs[id]
		if run == nil {
			run = &retainedRun{id: id, eligible: true, success: true}
			runs[id] = run
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			run.bytes += info.Size()
			total += info.Size()
		}
		if run.at.IsZero() {
			run.at = info.ModTime()
		}
		return nil
	})
	if err != nil {
		return err
	}
	for id := range c.workers {
		for _, record := range c.records {
			if record.Job.RunID == id {
				remaining := reserveFor(record.Job)
				if run := runs[id]; run != nil {
					remaining -= run.bytes
				}
				if remaining > 0 {
					reserve += remaining
				}
				break
			}
		}
	}
	for _, record := range c.records {
		run := runs[record.Job.RunID]
		if run == nil {
			run = &retainedRun{id: record.Job.RunID, eligible: true, success: true}
			runs[run.id] = run
		}
		if !run.known {
			run.at = time.Time{}
			run.known = true
		}
		at := record.Job.Deadline
		if record.Result != nil && !record.Result.FinishedAt.IsZero() {
			at = record.Result.FinishedAt
		}
		if at.After(run.at) {
			run.at = at
		}
		if record.Result == nil || !record.Acked || len(record.Events) > 0 {
			run.eligible = false
		}
		if record.Result == nil || record.Result.Outcome != "SUCCESS" || record.Result.Cleanup != "SUCCESS" {
			run.success = false
		}
	}
	var candidates []*retainedRun
	for id, run := range runs {
		if _, active := c.workers[id]; active {
			run.eligible = false
		}
		for _, collectingRun := range c.collectActive {
			if collectingRun == id {
				run.eligible = false
			}
		}
		if run.eligible {
			candidates = append(candidates, run)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].success != candidates[j].success {
			return candidates[i].success
		}
		return candidates[i].at.Before(candidates[j].at)
	})
	budget := int64(policy.DiskMB) << 20
	for _, run := range candidates {
		ttl := time.Duration(policy.FailureMinutes) * time.Minute
		if run.success {
			ttl = time.Duration(policy.SuccessMinutes) * time.Minute
		}
		if now.Sub(run.at) < ttl && total+reserve <= budget && len(c.records) < 4094 {
			continue
		}
		removed := map[string]*record{}
		for key, v := range c.records {
			if v.Job.RunID == run.id {
				removed[key] = v
				delete(c.records, key)
			}
		}
		if err := c.save(); err != nil {
			for key, v := range removed {
				c.records[key] = v
			}
			return err
		}
		if err := os.RemoveAll(c.runDir(run.id)); err != nil {
			return err
		}
		total -= run.bytes
	}
	c.storageBytes = total
	if total+reserve > budget || len(c.records) >= 4094 {
		return errors.New("local diagnostic budget exhausted; unacknowledged records retained")
	}
	if reserve > 0 {
		var stat syscall.Statfs_t
		if err := syscall.Statfs(c.config.DataDir, &stat); err != nil {
			return err
		}
		if int64(stat.Bavail)*int64(stat.Bsize) < reserve {
			return errors.New("filesystem free space below worker reserve")
		}
	}
	return nil
}
