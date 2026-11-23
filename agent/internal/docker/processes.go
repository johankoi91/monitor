package docker

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/johankoi91/monitor/runtime/shared/model"
)

// Ask only for PID and executable name, never process arguments (which can
// contain credentials). top includes child services behind shell entrypoints.
func (c *Collector) processes(ctx context.Context, raw inspect) ([]model.ContainerProcess, error) {
	var top struct {
		Titles    []string
		Processes [][]string
	}
	if err := c.get(ctx, "/containers/"+url.PathEscape(raw.ID)+"/top?ps_args="+url.QueryEscape("-eo pid,comm"), &top); err != nil {
		return nil, err
	}
	if len(top.Processes) == 0 || len(top.Processes) > 128 {
		return nil, errors.New("container process list is empty or oversized")
	}
	pidColumn, nameColumn := -1, -1
	for i, title := range top.Titles {
		switch strings.ToUpper(strings.TrimSpace(title)) {
		case "PID":
			pidColumn = i
		case "COMMAND", "COMM":
			nameColumn = i
		}
	}
	if pidColumn < 0 || nameColumn < 0 {
		return nil, errors.New("container process columns unavailable")
	}
	processes := []model.ContainerProcess{}
	seen := map[int]bool{}
	for _, row := range top.Processes {
		if pidColumn >= len(row) || nameColumn >= len(row) {
			return nil, errors.New("invalid container process row")
		}
		pid, err := strconv.Atoi(strings.TrimSpace(row[pidColumn]))
		name := filepath.Base(strings.TrimSpace(row[nameColumn]))
		// Linux comm is truncated and may differ after prctl. Docker's actual
		// executable path is authoritative for the container's initial process.
		if raw.State != nil && pid == raw.State.Pid && raw.Path != "" {
			name = filepath.Base(raw.Path)
		}
		if err != nil || pid <= 0 || name == "" || name == "." || len(name) > 255 || seen[pid] {
			return nil, errors.New("invalid container process identity")
		}
		seen[pid] = true
		processes = append(processes, model.ContainerProcess{PID: pid, Name: name})
	}
	sort.Slice(processes, func(i, j int) bool { return processes[i].PID < processes[j].PID })
	return processes, nil
}
