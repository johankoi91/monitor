package docker

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Target struct {
	ID, Name, Image, SnapshotID, Runtime, StartedAt string
	Incomplete                                      bool
}

func (c *Collector) InspectTarget(ctx context.Context, id string) (Target, error) {
	var raw inspect
	if err := c.get(ctx, "/containers/"+url.PathEscape(id)+"/json", &raw); err != nil {
		return Target{}, errors.New("cannot verify original container")
	}
	s := startup(raw, time.Now().UTC(), c.policy)
	t := Target{ID: raw.ID, Name: strings.TrimPrefix(raw.Name, "/"), Image: s.Config.Image, SnapshotID: s.SnapshotID, Incomplete: s.Incomplete || s.Truncated}
	if raw.State != nil {
		t.Runtime = raw.State.Status
		t.StartedAt = raw.State.StartedAt
	}
	return t, nil
}

// RestartOriginal is the only Docker write exposed by this module. Callers must
// journal EXECUTING and verify exact identity before invoking it. Never retry it.
func (c *Collector) RestartOriginal(ctx context.Context, id string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://docker/v1.39/containers/"+url.PathEscape(id)+"/restart?t=10", nil)
	if err != nil {
		return "FAILED", errors.New("cannot form restart request")
	}
	client := *c.client
	client.Timeout = 20 * time.Second
	resp, err := client.Do(req)
	if err != nil {
		return "UNCERTAIN", errors.New("Docker execution result is unknown; not retried")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 204 {
		return "EXECUTED", nil
	}
	if resp.StatusCode == 400 || resp.StatusCode == 403 || resp.StatusCode == 404 || resp.StatusCode == 409 {
		return "FAILED", errors.New("Docker explicitly rejected restart")
	}
	return "UNCERTAIN", errors.New("Docker execution result is unknown; not retried")
}
