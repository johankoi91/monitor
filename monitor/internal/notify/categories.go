package notify

import (
	"errors"
	"strings"
)

const ContainerAbnormal = "CONTAINER_ABNORMAL"
const CollectionAbnormal = "COLLECTION_ABNORMAL"
const Recovered = "RECOVERED"
const Normal = "NORMAL"

// Routes are ordered and fixed-size so settings remain comparable and revisions
// protect pending deliveries when any destination changes.
type Route struct {
	Category      string `json:"category" yaml:"category"`
	Enabled       bool   `json:"enabled" yaml:"enabled"`
	URL           string `json:"url" yaml:"url"`
	TLSServerName string `json:"tls_server_name" yaml:"tls_server_name"`
}

func defaultRoutes() [3]Route {
	return [3]Route{{Category: ContainerAbnormal, Enabled: true}, {Category: CollectionAbnormal, Enabled: true}, {Category: Recovered, Enabled: true}}
}

func category(f Fact) string {
	if f.Stale || f.ReasonCode == "AGENT_OFFLINE" || strings.Contains(f.ReasonCode, "STALE") {
		return CollectionAbnormal
	}
	if f.Status == "HEALTHY" {
		return Recovered
	}
	return ContainerAbnormal
}

func routeConfig(c Config, category string) (Config, bool) {
	if !c.RoutesConfigured || category == "" {
		return c, true
	}
	for _, r := range c.Routes {
		if r.Category == category {
			if r.URL != "" {
				c.URL = r.URL
				c.TLSServerName = r.TLSServerName
			}
			return c, r.Enabled
		}
	}
	return c, false
}

func validateRoutes(c Config) error {
	if c.EndpointMode {
		return nil
	}
	if !c.RoutesConfigured {
		return nil
	}
	expected := defaultRoutes()
	for i, r := range c.Routes {
		if r.Category != expected[i].Category {
			return errors.New("通知分类必须依次为容器异常、采集异常、恢复正常")
		}
		target := c
		target.RoutesConfigured = false
		if r.URL != "" {
			target.URL = r.URL
			target.TLSServerName = r.TLSServerName
		}
		target.Enabled = c.Enabled && r.Enabled
		if err := validSettings(target); err != nil {
			return err
		}
	}
	return nil
}
