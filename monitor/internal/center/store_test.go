package center

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/johankoi91/monitor/runtime/internal/model"
	"github.com/johankoi91/monitor/runtime/web"
	"gopkg.in/yaml.v3"
	"io/fs"
)

func config() Config {
	return Config{Site: model.Site{Code: "rtc-pilot", Name: "RTC"}, Agents: []AgentConfig{{ID: "rtc-a", Secret: strings.Repeat("a", 32), HostName: "shared-hostname", HostAddress: "10.0.0.1"}, {ID: "rtc-b", Secret: strings.Repeat("b", 32), HostName: "shared-hostname", HostAddress: "10.0.0.2"}}}
}
func setup(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir(), config())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}
func report(names ...string) model.Report {
	at := time.Now().UTC()
	r := model.Report{Type: "snapshot", Complete: true, Sequence: 1, CollectedAt: at, Containers: []model.Observation{}}
	for _, name := range names {
		r.Containers = append(r.Containers, model.Observation{Container: model.Container{ContainerID: "id-" + name, ContainerName: name, SnapshotID: "snapshot-" + name, CollectedAt: at, RuntimeStatus: "running", Image: "rtc/ap:1"}, Startup: &model.Startup{SnapshotID: "snapshot-" + name, CollectedAt: at, Config: model.StartupConfig{Image: "rtc/ap:1"}}})
	}
	return r
}
func connect(t *testing.T, s *Store, id string, names ...string) string {
	t.Helper()
	session := s.Connect(id)
	if err := s.Receive(id, session, report(names...)); err != nil {
		t.Fatal(err)
	}
	return session
}
func selection(revision string, agents ...string) model.Selection {
	v := model.Selection{ExpectedRevision: revision, Additions: []model.Addition{}, Removals: []string{}}
	for _, id := range agents {
		v.Additions = append(v.Additions, model.Addition{AgentID: id, ContainerID: "id-ap", SnapshotID: "snapshot-ap", ClusterCode: "rtc-pilot", ClusterName: "RTC", ServiceCode: "rtc-ap", ServiceName: "RTC AP"})
	}
	return v
}
func code(t *testing.T, err error, want string) {
	t.Helper()
	p, ok := err.(*Problem)
	if !ok || p.Code != want {
		t.Fatalf("got %v, want %s", err, want)
	}
}
func nodeCount(b model.Baseline) int {
	n := 0
	if b.Definition != nil {
		for _, c := range b.Definition.Clusters {
			for _, v := range c.Services {
				n += len(v.Nodes)
			}
		}
	}
	return n
}

func TestSaveCrossHostAndRestartRecovery(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, config())
	if err != nil {
		t.Fatal(err)
	}
	connect(t, s, "rtc-a", "ap")
	connect(t, s, "rtc-b", "ap")
	b, err := s.Save(selection("", "rtc-a", "rtc-b"), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if nodeCount(b) != 2 || !b.Active {
		t.Fatal("baseline not activated")
	}
	for _, n := range b.Definition.Clusters[0].Services[0].Nodes {
		if n.RestartEnabled {
			t.Fatal("selection enabled restart")
		}
		if n.ID != n.AgentID+"/ap" {
			t.Fatal("identity depends on repeated hostname")
		}
	}
	text, _, err := s.YAML(b.Revision)
	if err != nil {
		t.Fatal(err)
	}
	var d model.Definition
	if err = yaml.Unmarshal([]byte(text), &d); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "snapshot") || strings.Contains(text, "secret") {
		t.Fatal("startup configuration mixed into YAML")
	}
	s.Close()
	s, err = NewStore(dir, config())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Baseline().Revision != b.Revision {
		t.Fatal("version not recovered")
	}
	status, _ := json.Marshal(s.Statuses("", ""))
	if !strings.Contains(string(status), `"unknown_node_count":2`) {
		t.Fatal("cold start falsely healthy")
	}
}
func TestBaselineSurvivesMissingOfflineAndFilteredAdditions(t *testing.T) {
	s := setup(t)
	session := connect(t, s, "rtc-a", "ap")
	b, err := s.Save(selection("", "rtc-a"), "admin")
	if err != nil {
		t.Fatal(err)
	}
	r := report()
	r.Sequence = 2
	if err = s.Receive("rtc-a", session, r); err != nil {
		t.Fatal(err)
	}
	status, _ := json.Marshal(s.Statuses("", ""))
	if !strings.Contains(string(status), "CONTAINER_MISSING") || nodeCount(s.Baseline()) != 1 {
		t.Fatal("missing container disappeared from baseline")
	}
	s.Disconnect("rtc-a", session)
	connect(t, s, "rtc-b", "ap")
	b, err = s.Save(selection(b.Revision, "rtc-b"), "admin")
	if err != nil || nodeCount(b) != 2 {
		t.Fatalf("offline existing member prevented unrelated save: %v", err)
	}
	if len(s.Containers()) != 1 {
		t.Fatal("unexpected discovery list")
	}
}
func TestConflictsPersistenceFailureAndIdentityChanges(t *testing.T) {
	s := setup(t)
	session := connect(t, s, "rtc-a", "ap")
	connect(t, s, "rtc-b", "ap")
	v := selection("", "rtc-a")
	v.Additions[0].SnapshotID = "old"
	_, err := s.Save(v, "admin")
	code(t, err, "CANDIDATE_CHANGED")
	b, err := s.Save(selection("", "rtc-a"), "admin")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Save(selection("", "rtc-b"), "admin")
	code(t, err, "BASELINE_CONFLICT")
	s.write = func([]byte) error { return errors.New("disk full") }
	_, err = s.Save(selection(b.Revision, "rtc-b"), "admin")
	code(t, err, "BASELINE_WRITE_FAILED")
	if s.Baseline().Revision != b.Revision {
		t.Fatal("failed save changed active version")
	}
	s.write = s.atomicWrite
	s.Disconnect("rtc-a", session)
	v = selection(b.Revision, "rtc-a")
	v.Removals = []string{"rtc-a/ap"}
	_, err = s.Save(v, "admin")
	code(t, err, "CANDIDATE_STALE")
	_, _, err = s.YAML("stale")
	code(t, err, "BASELINE_CONFLICT")
}
func TestConcurrentSaveOnlyOneVersionWins(t *testing.T) {
	s := setup(t)
	connect(t, s, "rtc-a", "ap")
	connect(t, s, "rtc-b", "ap")
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []string{"rtc-a", "rtc-b"} {
		wg.Add(1)
		go func(id string) { defer wg.Done(); _, err := s.Save(selection("", id), "admin"); results <- err }(id)
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		} else {
			code(t, err, "BASELINE_CONFLICT")
		}
	}
	if wins != 1 || nodeCount(s.Baseline()) != 1 {
		t.Fatal("concurrent save overwrote baseline")
	}
}

func TestReclassificationCannotInheritRestartPermission(t *testing.T) {
	s := setup(t)
	connect(t, s, "rtc-a", "ap")
	b, err := s.Save(selection("", "rtc-a"), "admin")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a previously approved binding; selection itself cannot grant it.
	s.mu.Lock()
	s.record.Baseline.Definition.Clusters[0].Services[0].Nodes[0].RestartEnabled = true
	s.mu.Unlock()
	v := selection(b.Revision, "rtc-a")
	v.Removals = []string{"rtc-a/ap"}
	b, err = s.Save(v, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !b.Definition.Clusters[0].Services[0].Nodes[0].RestartEnabled {
		t.Fatal("unchanged approved binding lost permission")
	}
	v = selection(b.Revision, "rtc-a")
	v.Removals = []string{"rtc-a/ap"}
	v.Additions[0].ServiceCode = "rtc-web-edge"
	v.Additions[0].ServiceName = "RTC Web Edge"
	b, err = s.Save(v, "admin")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range b.Definition.Clusters {
		for _, service := range c.Services {
			for _, n := range service.Nodes {
				if n.RestartEnabled {
					t.Fatal("reclassification inherited restart permission")
				}
			}
		}
	}
}
func TestNoFreshnessOnIncompleteOrOldSession(t *testing.T) {
	s := setup(t)
	session := connect(t, s, "rtc-a", "ap")
	r := report()
	r.Sequence = 2
	r.Complete = false
	r.Containers = nil
	s.mu.Lock()
	s.agents["rtc-a"].ReceivedAt = time.Now().Add(-time.Minute)
	s.mu.Unlock()
	if s.Receive("rtc-a", session, r) != nil {
		t.Fatal("error snapshot rejected")
	}
	if !s.Containers()[0].Stale {
		t.Fatal("incomplete collection refreshed stale data")
	}
	newSession := s.Connect("rtc-a")
	if newSession == session {
		t.Fatal("session reused")
	}
	if s.Receive("rtc-a", session, report()) == nil {
		t.Fatal("old session accepted")
	}
	if !s.Containers()[0].Stale {
		t.Fatal("connection alone refreshed data")
	}
}
func TestWriterLockAndCorruptRecovery(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, config())
	if err != nil {
		t.Fatal(err)
	}
	if second, err := NewStore(dir, config()); err == nil {
		second.Close()
		t.Fatal("second writer accepted")
	}
	s.Close()
	os.WriteFile(filepath.Join(dir, "current-baseline.json"), []byte(`{"baseline":`), 0600)
	if broken, err := NewStore(dir, config()); err == nil {
		broken.Close()
		t.Fatal("corrupt record accepted")
	}
}
func TestTCPDebounceAndStalePlan(t *testing.T) {
	s := setup(t)
	session := connect(t, s, "rtc-a", "ap")
	v := selection("", "rtc-a")
	check := model.Check{Type: "tcp", Host: "127.0.0.1", Port: 443, TimeoutMS: 2000}
	v.Additions[0].Checks = []model.Check{check}
	b, err := s.Save(v, "admin")
	if err != nil {
		t.Fatal(err)
	}
	seq := uint64(1)
	sendResult := func(ok bool, revision string) {
		seq++
		r := report("ap")
		r.Sequence = seq
		r.BaselineRevision = revision
		r.Containers[0].Checks = []model.CheckResult{{Check: check, OK: ok, CollectedAt: r.CollectedAt}}
		if err := s.Receive("rtc-a", session, r); err != nil {
			t.Fatal(err)
		}
	}
	status := func(want string) {
		data, _ := json.Marshal(s.Statuses("", ""))
		if !strings.Contains(string(data), `"status":"`+want+`"`) {
			t.Fatalf("wanted %s: %s", want, data)
		}
	}
	sendResult(true, b.Revision)
	status("UNKNOWN")
	sendResult(true, b.Revision)
	status("HEALTHY")
	sendResult(false, b.Revision)
	sendResult(false, b.Revision)
	status("HEALTHY")
	sendResult(false, b.Revision)
	status("UNHEALTHY")
	sendResult(true, b.Revision)
	status("UNHEALTHY")
	sendResult(true, b.Revision)
	status("HEALTHY")
	sendResult(true, "old")
	status("UNKNOWN")
}

func TestHTTPAuthUnknownFieldsAndCSRF(t *testing.T) {
	s := setup(t)
	handler := (&Server{Store: s, AdminID: "admin", AdminSecret: strings.Repeat("z", 32)}).Handler()
	request := func(body string, origin string, auth bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://center/api/v1/baseline/selection", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if auth {
			r.SetBasicAuth("admin", strings.Repeat("z", 32))
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if request(`{}`, "", false).Code != 401 {
		t.Fatal("missing auth accepted")
	}
	if request(`{}`, "http://evil.example", true).Code != 403 {
		t.Fatal("cross-origin write accepted")
	}
	if request(`{"expected_revision":"","additions":[],"removals":[],"restart_enabled":true}`, "", true).Code != 400 {
		t.Fatal("client restart permission accepted")
	}
	if request(`{} {}`, "", true).Code != 400 {
		t.Fatal("multiple JSON objects accepted")
	}
	r := httptest.NewRequest("GET", "http://center/api/v1/containers?page_size=999", nil)
	r.SetBasicAuth("admin", strings.Repeat("z", 32))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("invalid pagination accepted")
	}
}

func TestLoginShellLoadsWithoutExposingAuthenticatedData(t *testing.T) {
	s := setup(t)
	assets, err := fs.Sub(web.Files, "static")
	if err != nil {
		t.Fatal(err)
	}
	handler := (&Server{Store: s, AdminID: "admin", AdminSecret: strings.Repeat("z", 32), Assets: http.FileServer(http.FS(assets))}).Handler()
	assetsList := []string{"/", "/index.html"}
	entries, err := fs.ReadDir(assets, "assets")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		assetsList = append(assetsList, "/assets/"+entry.Name())
	}
	for _, path := range assetsList {
		r := httptest.NewRequest("GET", "http://center"+path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if (w.Code != 200 && !(path == "/index.html" && w.Code == 301)) || w.Header().Get("WWW-Authenticate") != "" {
			t.Fatalf("login asset %s blocked: %d", path, w.Code)
		}
		if strings.Contains(w.Body.String(), strings.Repeat("z", 32)) {
			t.Fatal("credential embedded in login shell")
		}
		if path == "/" || path == "/index.html" {
			csp := w.Header().Get("Content-Security-Policy")
			if !strings.Contains(csp, "script-src 'self';") || !strings.Contains(csp, "style-src 'self' 'nonce-") || strings.Contains(w.Body.String(), "__AVOPS_STYLE_NONCE__") {
				t.Fatal("login shell missing scoped Ant Design style authorization")
			}
			prefix := "style-src 'self' 'nonce-"
			at := strings.Index(csp, prefix) + len(prefix)
			nonce := strings.Split(csp[at:], "'")[0]
			if len(nonce) != 32 || !strings.Contains(w.Body.String(), `content="`+nonce+`"`) {
				t.Fatal("style nonce in HTML did not match CSP")
			}
			second := httptest.NewRecorder()
			handler.ServeHTTP(second, httptest.NewRequest("GET", "http://center"+path, nil))
			if second.Header().Get("Content-Security-Policy") == csp {
				t.Fatal("style nonce reused on reload")
			}
		}
	}
	for _, path := range []string{"/health/ready", "/api/v1/agents", "/api/v1/containers", "/api/v1/baseline", "/api/v1/baseline/yaml", "/api/v1/services/status"} {
		r := httptest.NewRequest("GET", "http://center"+path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("data endpoint %s exposed: %d", path, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "http://center/health/ready", nil)
	r.SetBasicAuth("admin", strings.Repeat("z", 32))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("explicit Basic login failed")
	}
	blocked := (&Server{Store: s, AdminID: "admin", AdminSecret: strings.Repeat("z", 32), Assets: http.FileServer(http.FS(assets)), AllowedIPs: []string{"127.0.0.1"}}).Handler()
	r = httptest.NewRequest("GET", "http://center/", nil)
	w = httptest.NewRecorder()
	blocked.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("login shell bypassed source access policy")
	}
}
func TestWebsocketAgentIsolationAndBaselineAPI(t *testing.T) {
	s := setup(t)
	server := httptest.NewServer((&Server{Store: s, AdminID: "admin", AdminSecret: strings.Repeat("z", 32)}).Handler())
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/agent/connect"
	headers := http.Header{}
	request := httptest.NewRequest("GET", server.URL, nil)
	request.SetBasicAuth("rtc-a", strings.Repeat("a", 32))
	headers.Set("Authorization", request.Header.Get("Authorization"))
	conn, _, err := websocket.DefaultDialer.Dial(url, headers)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	r := report("ap")
	r.Containers[0].Container.AgentID = "rtc-b"
	r.Containers[0].Container.HostAddress = "evil"
	if conn.WriteJSON(r) != nil {
		t.Fatal("report send failed")
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(s.Containers()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(s.Containers()) != 1 || s.Containers()[0].AgentID != "rtc-a" || s.Containers()[0].HostAddress != "10.0.0.1" {
		t.Fatal("Agent forged identity")
	}
	v := selection("", "rtc-a")
	data, _ := json.Marshal(v)
	req, _ := http.NewRequest("POST", server.URL+"/api/v1/baseline/selection", bytes.NewReader(data))
	req.SetBasicAuth("admin", strings.Repeat("z", 32))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("baseline HTTP save: %d", resp.StatusCode)
	}
	req, _ = http.NewRequest("GET", server.URL+"/api/v1/containers", nil)
	req.SetBasicAuth("rtc-a", strings.Repeat("a", 32))
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != 401 {
		t.Fatal("Agent credential accessed admin API")
	}
}
