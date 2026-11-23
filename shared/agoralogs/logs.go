// Package agoralogs implements collect_agora_logs.sh's UTC three-hour buckets.
package agoralogs

import (
	"errors"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/johankoi91/monitor/runtime/shared/model"
)

var filename = regexp.MustCompile(`^.+\.DEBUG\.([0-9]{8}-[0-9]{2})\..+$`)
var serviceFilename = regexp.MustCompile(`^(.+)\.log\.DEBUG\.[0-9]{8}-[0-9]{2}\.([0-9]+)$`)

func MatchesServices(name string, services []model.LogService) bool {
	if len(services) == 0 {
		return true
	}
	match := serviceFilename.FindStringSubmatch(name)
	if match == nil {
		return false
	}
	pid, err := strconv.Atoi(match[2])
	if err != nil {
		return false
	}
	for _, service := range services {
		for _, process := range service.Processes {
			if pid == process.PID && match[1] == process.Name {
				return true
			}
		}
	}
	return false
}

func Source(config model.StartupConfig) (string, error) {
	for _, mount := range config.Mounts {
		if filepath.Clean(mount.Target) == "/var/log/agora" {
			if !filepath.IsAbs(mount.Source) || filepath.Clean(mount.Source) != mount.Source {
				return "", errors.New("agora_local_ap 的日志挂载缺少有效宿主机目录")
			}
			return mount.Source, nil
		}
	}
	return "", errors.New("agora_local_ap 未挂载 /var/log/agora")
}

func ParsePoint(input string, local *time.Location) (time.Time, error) {
	input = strings.TrimSpace(input)
	if len(input) == 10 || len(input) == 13 {
		if n, err := strconv.ParseInt(input, 10, 64); err == nil && n >= 0 {
			if len(input) == 13 {
				return time.UnixMilli(n).UTC(), nil
			}
			return time.Unix(n, 0).UTC(), nil
		}
	}
	for _, layout := range []string{"20060102150405", time.RFC3339Nano, "2006-01-02 15:04:05Z07:00", "2006-01-02 15:04:05 -0700"} {
		if at, err := time.Parse(layout, input); err == nil {
			return at.UTC(), nil
		}
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if at, err := time.ParseInLocation(layout, input, local); err == nil {
			return at.UTC(), nil
		}
	}
	return time.Time{}, errors.New("时间格式无效，请使用时间戳、YYYYMMDDhhmmss 或带时区的标准时间")
}

func Buckets(mode, input string, local *time.Location) ([]string, string, error) {
	var start, end, target time.Time
	var err error
	switch mode {
	case "point":
		target, err = ParsePoint(input, local)
		start, end = target.Add(-30*time.Minute), target.Add(30*time.Minute)
	case "day":
		target, err = time.Parse("20060102", strings.TrimSpace(input))
		start, end = target, target.Add(24*time.Hour-time.Second)
	default:
		err = errors.New("收集模式须为 point 或 day")
	}
	if err != nil {
		return nil, "", err
	}
	tags := []string{}
	for bucket := start.UTC().Truncate(3 * time.Hour); !bucket.After(end); bucket = bucket.Add(3 * time.Hour) {
		tags = append(tags, bucket.Format("20060102-15"))
	}
	return tags, "agora_logs_" + target.UTC().Format("20060102T150405Z"), nil
}

func Matches(name string, tags []string) bool {
	match := filename.FindStringSubmatch(name)
	if match == nil {
		return false
	}
	for _, tag := range tags {
		if match[1] == tag {
			return true
		}
	}
	return false
}
