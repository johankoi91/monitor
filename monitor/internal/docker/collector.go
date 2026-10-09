// Package docker collects from Docker's Unix socket and supports a guarded
// original-container restart. It exposes no stop/delete/recreate/exec API.
package docker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/johankoi91/monitor/runtime/internal/model"
)

const masked = "[REDACTED]"

var sensitive = regexp.MustCompile(`(?i)pass(word|wd)?|secret|token|credential|authorization|api[-_]?key|private[-_]?key|access[-_]?key`)
var urls = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s]+`)

type Policy struct {
	EnvAllow map[string]bool
	ArgAllow map[string]bool
}

// SafeText strips URL credentials and all query values even in otherwise allowed fields.
func SafeText(value string) string {
	return urls.ReplaceAllStringFunc(value, func(s string) string {
		u, err := url.Parse(s)
		if err != nil {
			return masked
		}
		if u.User != nil {
			u.User = url.User(masked)
		}
		if u.RawQuery != "" {
			q := u.Query()
			for k := range q {
				q.Set(k, masked)
			}
			u.RawQuery = q.Encode()
		}
		if u.Fragment != "" {
			u.Fragment = masked
		}
		return u.String()
	})
}

func redactArgs(args []string, executable bool, allow map[string]bool, field string, fields *[]string) []string {
	result := make([]string, len(args))
	previous := ""
	for i, arg := range args {
		value := masked
		if previous != "" && sensitive.MatchString(previous) {
			previous = ""
		} else if i == 0 && executable && !strings.ContainsAny(arg, " \t\n=") && !strings.Contains(arg, "://") {
			value = arg
		} else if strings.HasPrefix(arg, "-") {
			key, val, hasValue := strings.Cut(arg, "=")
			name := strings.TrimLeft(key, "-")
			previous = name
			value = key
			if hasValue {
				if allow[name] && !sensitive.MatchString(name) {
					value += "=" + SafeText(val)
				} else {
					value += "=" + masked
				}
				previous = ""
			}
		} else if previous != "" && allow[previous] && !sensitive.MatchString(previous) {
			value = SafeText(arg)
			previous = ""
		} else {
			previous = ""
		}
		if value != arg {
			*fields = append(*fields, fmt.Sprintf("%s[%d]", field, i))
		}
		result[i] = value
	}
	return result
}

type inspect struct {
	ID           string `json:"Id"`
	Name         string
	Image        string
	Path         string
	Args         []string
	RestartCount int
	Created      string
	Config       *struct {
		Image        string
		Entrypoint   []string
		Cmd          []string
		Env          []string
		WorkingDir   string
		User         string
		ExposedPorts map[string]json.RawMessage
	}
	HostConfig *struct {
		Memory        int64
		NanoCpus      int64
		CpuQuota      int64
		CpuPeriod     int64
		Binds         []string
		NetworkMode   string
		RestartPolicy struct {
			Name              string
			MaximumRetryCount int
		}
		PortBindings map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string
		}
	}
	Mounts []struct {
		Source      string
		Destination string
		Type        string
		RW          bool
	}
	State *struct {
		Status     string
		StartedAt  string
		FinishedAt string
		OOMKilled  *bool
		ExitCode   int
		Health     *struct{ Status string }
	}
}

func startup(raw inspect, at time.Time, policy Policy) *model.Startup {
	s := &model.Startup{CollectedAt: at, RedactedFields: []string{}}
	c := &s.Config
	c.ImageID, c.Path = SafeText(raw.Image), SafeText(raw.Path)
	c.Args = redactArgs(raw.Args, false, policy.ArgAllow, "args", &s.RedactedFields)
	c.Env, c.Mounts, c.PortBindings, c.ExposedPorts = []model.Env{}, []model.Mount{}, []model.PortBinding{}, []string{}
	c.Entrypoint, c.Cmd = []string{}, []string{}
	if raw.Config == nil {
		s.Incomplete = true
	} else {
		r := raw.Config
		c.Image, c.WorkingDir, c.User = SafeText(r.Image), SafeText(r.WorkingDir), SafeText(r.User)
		c.Entrypoint = redactArgs(r.Entrypoint, true, policy.ArgAllow, "entrypoint", &s.RedactedFields)
		c.Cmd = redactArgs(r.Cmd, len(r.Entrypoint) == 0, policy.ArgAllow, "cmd", &s.RedactedFields)
		for _, item := range r.Env {
			name, value, _ := strings.Cut(item, "=")
			redacted := !policy.EnvAllow[name] || sensitive.MatchString(name)
			if redacted {
				value = masked
				s.RedactedFields = append(s.RedactedFields, "env."+name)
			} else {
				value = SafeText(value)
			}
			c.Env = append(c.Env, model.Env{Name: name, Value: value, Redacted: redacted})
		}
		for port := range r.ExposedPorts {
			c.ExposedPorts = append(c.ExposedPorts, port)
		}
		sort.Strings(c.ExposedPorts)
	}
	if raw.HostConfig == nil {
		s.Incomplete = true
	} else {
		h := raw.HostConfig
		c.NetworkMode = SafeText(h.NetworkMode)
		c.RestartPolicy = model.RestartPolicy{Name: h.RestartPolicy.Name, MaximumRetryCount: h.RestartPolicy.MaximumRetryCount}
		keys := []string{}
		for key := range h.PortBindings {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			for _, p := range h.PortBindings[key] {
				c.PortBindings = append(c.PortBindings, model.PortBinding{ContainerPort: key, HostIP: p.HostIP, HostPort: p.HostPort})
			}
		}
		// Mounts is present on Docker 18.09. Fall back to Binds if its summary is absent.
		if len(raw.Mounts) == 0 {
			for _, bind := range h.Binds {
				parts := strings.Split(bind, ":")
				if len(parts) < 2 {
					s.Incomplete = true
					continue
				}
				ro := len(parts) > 2 && strings.Contains(parts[2], "ro")
				c.Mounts = append(c.Mounts, model.Mount{Source: SafeText(parts[0]), Target: SafeText(parts[1]), Type: "bind", ReadOnly: ro})
			}
		}
	}
	for _, m := range raw.Mounts {
		c.Mounts = append(c.Mounts, model.Mount{Source: SafeText(m.Source), Target: SafeText(m.Destination), Type: m.Type, ReadOnly: !m.RW})
	}
	sort.Slice(c.Env, func(i, j int) bool { return c.Env[i].Name < c.Env[j].Name })
	sort.Slice(c.Mounts, func(i, j int) bool { return c.Mounts[i].Target < c.Mounts[j].Target })
	data, _ := json.Marshal(c)
	if len(data) > 64<<10 {
		s.Truncated = true
		s.Incomplete = true
		s.Config = model.StartupConfig{Image: c.Image, ImageID: c.ImageID, Entrypoint: []string{}, Cmd: []string{}, Args: []string{}, Env: []model.Env{}, Mounts: []model.Mount{}, ExposedPorts: []string{}, PortBindings: []model.PortBinding{}}
		s.RedactedFields = append(s.RedactedFields, "oversized_config")
		data, _ = json.Marshal(s.Config)
	}
	hash := sha256.Sum256(data)
	s.Fingerprint = hex.EncodeToString(hash[:])
	identity := sha256.Sum256([]byte(raw.ID + "/" + s.Fingerprint))
	s.SnapshotID = hex.EncodeToString(identity[:])
	return s
}

type Collector struct {
	client *http.Client
	policy Policy
}

func New(socket string, policy Policy) *Collector {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", socket)
	}, MaxIdleConnsPerHost: 4}
	return &Collector{client: &http.Client{Transport: transport, Timeout: 5 * time.Second}, policy: policy}
}
func (c *Collector) get(ctx context.Context, path string, target any) error {
	r, err := http.NewRequestWithContext(ctx, "GET", "http://docker/v1.39"+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(r)
	if err != nil {
		return errors.New("Docker socket unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("Docker returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil || len(data) > 4<<20 {
		return errors.New("Docker response unavailable or oversized")
	}
	if json.Unmarshal(data, target) != nil {
		return errors.New("invalid Docker response")
	}
	return nil
}
func (c *Collector) Collect(ctx context.Context, plan model.Plan) model.Report {
	at := time.Now().UTC()
	report := model.Report{Type: "snapshot", CollectedAt: at, Complete: true, BaselineRevision: plan.Revision, Containers: []model.Observation{}}
	var list []struct {
		ID string `json:"Id"`
	}
	if err := c.get(ctx, "/containers/json?all=true", &list); err != nil {
		report.Complete = false
		report.Error = err.Error()
		return report
	}
	if len(list) > 512 {
		report.Complete = false
		report.Error = "container discovery exceeds 512-container budget"
		return report
	}
	checks := map[string][]model.Check{}
	for _, n := range plan.Nodes {
		checks[n.ContainerName] = n.Checks
	}
	observations := make([]model.Observation, len(list))
	jobs := make(chan int)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				var raw inspect
				if err := c.get(ctx, "/containers/"+url.PathEscape(list[i].ID)+"/json", &raw); err != nil {
					mu.Lock()
					report.Complete = false
					report.Error = "container inspection incomplete"
					mu.Unlock()
					continue
				}
				name := strings.TrimPrefix(raw.Name, "/")
				s := startup(raw, time.Now().UTC(), c.policy)
				o := model.Observation{Container: model.Container{ContainerID: raw.ID, ContainerName: name, Image: s.Config.Image, SnapshotID: s.SnapshotID, CollectedAt: s.CollectedAt}, Startup: s, RestartCount: raw.RestartCount, Checks: []model.CheckResult{}}
				o.Container.Limits = limits(raw)
				o.Container.Lifecycle = lifecycle(raw, s.CollectedAt)
				if raw.State == nil {
					o.Container.RuntimeStatus = "unknown"
				} else {
					o.Container.RuntimeStatus = raw.State.Status
					o.StartedAt = raw.State.StartedAt
					o.ExitCode = raw.State.ExitCode
					if raw.State.Health != nil {
						o.DockerHealth = raw.State.Health.Status
					}
				}
				for _, check := range checks[name] {
					ok := false
					if LocalTarget(check.Host) && check.Port > 0 && check.Port <= 65535 && check.TimeoutMS > 0 && check.TimeoutMS <= 5000 {
						conn, err := net.DialTimeout("tcp", net.JoinHostPort(check.Host, strconv.Itoa(check.Port)), time.Duration(check.TimeoutMS)*time.Millisecond)
						if err == nil {
							ok = true
							conn.Close()
						}
					}
					o.Checks = append(o.Checks, model.CheckResult{Check: check, OK: ok, CollectedAt: time.Now().UTC()})
				}
				observations[i] = o
			}
		}()
	}
	for i := range list {
		select {
		case jobs <- i:
		case <-ctx.Done():
			mu.Lock()
			report.Complete = false
			report.Error = "collection timeout"
			mu.Unlock()
		}
	}
	close(jobs)
	wg.Wait()
	if !report.Complete {
		return report
	}
	report.Containers = observations
	sort.Slice(report.Containers, func(i, j int) bool {
		return report.Containers[i].Container.ContainerName < report.Containers[j].Container.ContainerName
	})
	return report
}

func parsedTime(value string) *time.Time {
	at, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || at.IsZero() {
		return nil
	}
	at = at.UTC()
	return &at
}
func limits(raw inspect) *model.ResourceLimits {
	if raw.HostConfig == nil {
		return nil
	}
	result := &model.ResourceLimits{}
	if raw.HostConfig.Memory > 0 {
		value := uint64(raw.HostConfig.Memory)
		result.MemoryLimitBytes = &value
	}
	if raw.HostConfig.NanoCpus > 0 {
		value := float64(raw.HostConfig.NanoCpus) / 1e9
		result.CPUQuotaCores = &value
	} else if raw.HostConfig.CpuQuota > 0 && raw.HostConfig.CpuPeriod > 0 {
		value := float64(raw.HostConfig.CpuQuota) / float64(raw.HostConfig.CpuPeriod)
		result.CPUQuotaCores = &value
	}
	return result
}
func lifecycle(raw inspect, at time.Time) *model.ContainerLifecycle {
	result := &model.ContainerLifecycle{CreatedAt: parsedTime(raw.Created), RestartCount: raw.RestartCount, LastObservedAt: at}
	if raw.State == nil {
		return result
	}
	result.StartedAt = parsedTime(raw.State.StartedAt)
	result.FinishedAt = parsedTime(raw.State.FinishedAt)
	result.OOMKilled = raw.State.OOMKilled
	if raw.State.Status == "exited" || raw.State.Status == "dead" {
		value := raw.State.ExitCode
		result.ExitCode = &value
	}
	if raw.State.Health != nil {
		result.DockerHealth = raw.State.Health.Status
	}
	return result
}

// Probes are constrained to this host; configuration cannot turn the Agent into a network scanner.
func LocalTarget(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addresses {
		p, _, err := net.ParseCIDR(a.String())
		if err == nil && p.Equal(ip) {
			return true
		}
	}
	return false
}
