package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func apiMinor(value string) (int, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 || parts[0] != "1" || len(parts[1]) > 3 {
		return 0, errors.New("unsupported Docker API version")
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil || minor < 0 {
		return 0, errors.New("invalid Docker API version")
	}
	return minor, nil
}
func chooseAPIVersion(maximum, minimum string) (string, error) {
	max, err := apiMinor(maximum)
	if err != nil {
		return "", err
	}
	min := 12
	if minimum != "" {
		min, err = apiMinor(minimum)
		if err != nil {
			return "", err
		}
	}
	if max < 24 || min > max {
		return "", errors.New("Docker API range unsupported")
	}
	selected := 39
	if max < selected {
		selected = max
	}
	if min > selected {
		selected = min
	}
	return fmt.Sprintf("1.%d", selected), nil
}
func (c *Collector) apiVersion(ctx context.Context) (string, error) {
	c.versionMu.Lock()
	defer c.versionMu.Unlock()
	if c.version != "" && time.Since(c.versionChecked) < time.Minute {
		return c.version, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/version", nil)
	if err != nil {
		return "", err
	}
	response, err := c.client.Do(req)
	if err != nil {
		return "", errors.New("Docker socket unavailable during API negotiation")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return "", fmt.Errorf("Docker API negotiation returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 16385))
	if err != nil || len(data) > 16384 {
		return "", errors.New("Docker version response unavailable or oversized")
	}
	var daemon struct {
		Maximum string `json:"ApiVersion"`
		Minimum string `json:"MinAPIVersion"`
	}
	if json.Unmarshal(data, &daemon) != nil {
		return "", errors.New("invalid Docker version response")
	}
	selected, err := chooseAPIVersion(daemon.Maximum, daemon.Minimum)
	if err != nil {
		return "", err
	}
	c.version, c.versionChecked = selected, time.Now()
	return selected, nil
}
func (c *Collector) invalidateVersion(previous string) {
	c.versionMu.Lock()
	defer c.versionMu.Unlock()
	if c.version == previous {
		c.version = ""
		c.versionChecked = time.Time{}
	}
}
