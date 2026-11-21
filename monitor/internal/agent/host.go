package agent

import (
	"net"
	"os"
	"sort"
	"time"

	"github.com/johankoi91/monitor/runtime/internal/model"
)

// HostInfo collects the host-network interfaces. Docker-backed deployments use
// the Docker daemon's host name, rather than the Agent container's UTS name.
func HostInfo(dockerHostName, override, observed string) model.HostInfo {
	name := dockerHostName
	nameSource := "docker_info"
	if name == "" {
		name, _ = os.Hostname()
		nameSource = "os_hostname"
	}
	ips := map[string]bool{}
	interfaces, _ := net.InterfaceAddrs()
	for _, address := range interfaces {
		ip, _, err := net.ParseCIDR(address.String())
		if err == nil && !ip.IsLoopback() && !ip.IsUnspecified() && !ip.IsMulticast() && !ip.IsLinkLocalUnicast() {
			ips[ip.String()] = true
		}
	}
	address, source := override, "configured"
	if address == "" {
		address, source = observed, "center_observed"
	}
	if net.ParseIP(address) == nil {
		address, source = "", "interface"
		list := []string{}
		for ip := range ips {
			list = append(list, ip)
		}
		sort.Strings(list)
		for _, ip := range list {
			if net.ParseIP(ip).To4() != nil {
				address = ip
				break
			}
		}
		if address == "" && len(list) > 0 {
			address = list[0]
		}
	}
	if address == "" {
		address = "127.0.0.1"
	}
	if net.ParseIP(address) != nil {
		ips[address] = true
	}
	list := []string{}
	for ip := range ips {
		list = append(list, ip)
	}
	sort.Strings(list)
	if len(list) > 32 {
		list = list[:32]
	}
	return model.HostInfo{HostName: name, HostAddress: address, IPAddresses: list, HostnameSource: nameSource, AddressSource: source, CollectedAt: time.Now().UTC()}
}
