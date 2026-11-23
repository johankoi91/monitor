package docker

import (
	"context"
	"fmt"
	"github.com/johankoi91/monitor/runtime/shared/model"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDockerVersionRangeSelection(t *testing.T) {
	for _, c := range []struct{ max, min, want string }{{"1.39", "1.12", "1.39"}, {"1.56", "1.40", "1.40"}, {"1.24", "", "1.24"}, {"1.56", "1.44", "1.44"}} {
		got, err := chooseAPIVersion(c.max, c.min)
		if err != nil || got != c.want {
			t.Fatalf("%s/%s: %s %v", c.max, c.min, got, err)
		}
	}
	for _, c := range [][2]string{{"2.0", "1.40"}, {"1.20", "1.12"}, {"1.39", "1.40"}, {"invalid", "1.12"}} {
		if _, err := chooseAPIVersion(c[0], c[1]); err == nil {
			t.Fatal("unsupported API range accepted", c)
		}
	}
}

type versionTransport func(*http.Request) (*http.Response, error)

func (f versionTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func versionTestCollector(server *httptest.Server) *Collector {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &Collector{client: &http.Client{Transport: versionTransport(func(r *http.Request) (*http.Response, error) {
		clone := r.Clone(r.Context())
		u := *r.URL
		u.Host = strings.TrimPrefix(server.URL, "http://")
		clone.URL = &u
		return transport.RoundTrip(clone)
	})}}
}
func TestDockerDaemonUpgradeRenegotiatesReads(t *testing.T) {
	var min atomic.Int32
	min.Store(39)
	var versions atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("read performed mutation")
			w.WriteHeader(405)
			return
		}
		if r.URL.Path == "/version" {
			versions.Add(1)
			fmt.Fprintf(w, `{"ApiVersion":"1.56","MinAPIVersion":"1.%d"}`, min.Load())
			return
		}
		if min.Load() == 40 && strings.HasPrefix(r.URL.Path, "/v1.39/") {
			w.WriteHeader(400)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/containers/json") {
			w.Write([]byte(`[{"Id":"container-a"}]`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/containers/container-a/json") {
			w.Write([]byte(fixture))
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()
	c := versionTestCollector(server)
	if r := c.Collect(context.Background(), model.Plan{}); !r.Complete {
		t.Fatal(r.Error)
	}
	min.Store(40)
	if r := c.Collect(context.Background(), model.Plan{}); !r.Complete || len(r.Containers) != 1 {
		t.Fatal("daemon upgrade broke discovery", r.Error)
	}
	if versions.Load() != 2 {
		t.Fatal("API version cache/refetch incorrect", versions.Load())
	}
}
func TestRestartUsesNegotiatedAPIAndNeverRetriesMutation(t *testing.T) {
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			w.Write([]byte(`{"ApiVersion":"1.56","MinAPIVersion":"1.40"}`))
			return
		}
		if r.Method == "POST" && r.URL.Path == "/v1.40/containers/approved-id/restart" {
			posts.Add(1)
			w.WriteHeader(400)
			return
		}
		t.Error("unexpected Docker path", r.Method, r.URL.Path)
		w.WriteHeader(404)
	}))
	defer server.Close()
	c := versionTestCollector(server)
	if outcome, err := c.RestartOriginal(context.Background(), "approved-id"); err == nil || outcome != "FAILED" {
		t.Fatal("explicit rejection not final", outcome, err)
	}
	if posts.Load() != 1 {
		t.Fatal("restart was retried", posts.Load())
	}
}
