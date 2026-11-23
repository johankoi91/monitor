package notify

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestCategoriesAndDedicatedDelivery(t *testing.T) {
	var mu sync.Mutex
	got := map[string][]string{}
	receiver := func(name string) *httptest.Server {
		return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var e struct {
				Category string `json:"event_category"`
			}
			json.NewDecoder(r.Body).Decode(&e)
			mu.Lock()
			got[name] = append(got[name], e.Category)
			mu.Unlock()
			w.Write([]byte(`{}`))
		}))
	}
	primary := receiver("default")
	defer primary.Close()
	dedicated := receiver("collection")
	defer dedicated.Close()
	routes := defaultRoutes()
	routes[1].URL = dedicated.URL
	routes[2].Enabled = false
	n, err := New(t.TempDir(), Config{Enabled: true, URL: primary.URL, Secret: "test", RoutesConfigured: true, Routes: routes})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	roots := x509.NewCertPool()
	roots.AddCert(primary.Certificate())
	roots.AddCert(dedicated.Certificate())
	n.newClient = func(c Config) *http.Client {
		client := webhookClient(c)
		client.Transport.(*http.Transport).TLSClientConfig.RootCAs = roots
		return client
	}
	n.client = n.newClient(n.config)
	n.Observe("1", []Fact{fact("HEALTHY")})
	n.Observe("1", []Fact{fact("UNHEALTHY")})
	n.SendOne(context.Background())
	offline := fact("UNKNOWN")
	offline.ReasonCode = "AGENT_OFFLINE"
	n.Observe("1", []Fact{offline})
	n.SendOne(context.Background())
	// Same UNKNOWN status, but the reason becomes a container-side problem.
	container := fact("UNKNOWN")
	container.ReasonCode = "CONTAINER_MISSING"
	if category(container) != ContainerAbnormal {
		t.Fatal("container problem classified as collection")
	}
	n.Observe("1", []Fact{fact("HEALTHY")})
	n.SendOne(context.Background())
	mu.Lock()
	defer mu.Unlock()
	if len(got["default"]) != 1 || got["default"][0] != ContainerAbnormal || len(got["collection"]) != 1 || got["collection"][0] != CollectionAbnormal {
		t.Fatalf("unexpected routing: %v", got)
	}
}

func TestCategoryChangeEmitsWithoutStatusChange(t *testing.T) {
	n, err := New(t.TempDir(), Config{Enabled: true, URL: "https://example.com", Secret: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	f := fact("UNKNOWN")
	f.ReasonCode = "AGENT_OFFLINE"
	n.Observe("1", []Fact{f})
	f.ReasonCode = "CONTAINER_MISSING"
	n.Observe("1", []Fact{f})
	if len(n.deliveries) != 1 {
		t.Fatal("reason category transition lost")
	}
}
