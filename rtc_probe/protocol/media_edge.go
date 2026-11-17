package protocol

import (
	"encoding/json"
	"errors"
	"net"
	"regexp"
	"strconv"
	"strings"
)

const MaxMediaEdges = 64

type MediaEdgeRange struct {
	Enabled   *bool  `json:"enabled,omitempty"`
	Host      string `json:"host"`
	StartPort int    `json:"start_port"`
	EndPort   int    `json:"end_port"`
}

func (r *MediaEdgeRange) Normalize() error {
	r.Host = strings.TrimSpace(r.Host)
	if net.ParseIP(r.Host) == nil && (!regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$`).MatchString(r.Host) || strings.Contains(r.Host, "..")) {
		return errors.New("UDP edge 地址须为 IP 或主机名，不包含协议和端口")
	}
	if len(r.Host) > 253 || r.StartPort < 1 || r.EndPort > 65535 || r.EndPort < r.StartPort || r.EndPort-r.StartPort+1 > MaxMediaEdges {
		return errors.New("UDP edge 端口范围无效，端口为 1–65535，一轮最多 64 个")
	}
	return nil
}
func (r MediaEdgeRange) Endpoints() []string {
	out := make([]string, 0, r.EndPort-r.StartPort+1)
	for port := r.StartPort; port <= r.EndPort; port++ {
		out = append(out, net.JoinHostPort(r.Host, strconv.Itoa(port)))
	}
	return out
}
func (r MediaEdgeRange) Contains(endpoint string) bool {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil || host != r.Host {
		return false
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= r.StartPort && n <= r.EndPort && port == strconv.Itoa(n)
}
func VOSParameters(endpoint string) (string, error) {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		return "", errors.New("invalid selected UDP edge")
	}
	n, err := strconv.Atoi(port)
	r := MediaEdgeRange{Host: host, StartPort: n, EndPort: n}
	if err != nil || r.Normalize() != nil || !r.Contains(endpoint) {
		return "", errors.New("invalid selected UDP edge")
	}
	b, err := json.Marshal(map[string][]string{"rtc.vos_list": {endpoint}})
	return string(b), err
}
