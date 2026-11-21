package model

import (
	"errors"
	"net"
	"strings"
	"time"
)

type HostInfo struct {
	HostName       string    `json:"host_name"`
	HostAddress    string    `json:"host_address"`
	IPAddresses    []string  `json:"ip_addresses"`
	HostnameSource string    `json:"hostname_source"`
	AddressSource  string    `json:"address_source"`
	CollectedAt    time.Time `json:"collected_at"`
}

func (h HostInfo) Validate() error {
	if strings.TrimSpace(h.HostName) == "" || len(h.HostName) > 255 || strings.ContainsAny(h.HostName, "\x00\r\n") || net.ParseIP(h.HostAddress) == nil || len(h.IPAddresses) > 32 || h.CollectedAt.IsZero() {
		return errors.New("invalid reported host identity")
	}
	for _, ip := range h.IPAddresses {
		if net.ParseIP(ip) == nil {
			return errors.New("invalid reported host IP")
		}
	}
	return nil
}
