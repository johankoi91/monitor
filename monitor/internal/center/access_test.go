package center

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/johankoi91/monitor/runtime/internal/access"
)

func TestRegisteredRestartKeyRequiredAndNodeScopeEnforced(t *testing.T) {
	s, _, _ := operational(t)
	registry, err := access.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	handler := (&Server{Store: s, AdminID: "admin", AdminSecret: strings.Repeat("z", 32), Access: registry}).Handler()
	other, _ := registry.Issue(access.IssueRequest{Name: "other", Purpose: "RESTART", AllowedNodes: []string{"rtc-a/other"}, ApprovalReason: "test"}, "admin")
	restart, _ := registry.Issue(access.IssueRequest{Name: "restart", Purpose: "RESTART", AllowedNodes: []string{"rtc-a/ap"}, ApprovalReason: "test"}, "admin")
	post := func(id, secret string) *httptest.ResponseRecorder {
		data, _ := json.Marshal(request())
		r := httptest.NewRequest("POST", "http://center/api/v1/operations/restart", strings.NewReader(string(data)))
		r.SetBasicAuth(id, secret)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if post("admin", strings.Repeat("z", 32)).Code != 403 {
		t.Fatal("admin credential bypassed registered restart key")
	}
	if post(other.Key.ID, other.Secret).Code != 403 {
		t.Fatal("wrong node key bypassed node scope")
	}
	if post(restart.Key.ID, restart.Secret).Code != 202 {
		t.Fatal("approved node restart key refused")
	}
	registry.Revoke(restart.Key.ID, "admin")
	if post(restart.Key.ID, restart.Secret).Code != 401 {
		t.Fatal("revoked key accepted")
	}
}

func TestDirectKeyIssuanceAndPurposeBoundaries(t *testing.T) {
	s, _, _ := operational(t)
	registry, err := access.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	handler := (&Server{Store: s, AdminID: "admin", AdminSecret: strings.Repeat("z", 32), Access: registry}).Handler()
	call := func(method, path, body, id, secret string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://center"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if id != "" {
			r.SetBasicAuth(id, secret)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	input, _ := json.Marshal(access.IssueRequest{Name: "direct restart", Purpose: "RESTART", AllowedNodes: []string{"rtc-a/ap"}, ApprovalReason: "test verified"})
	w := call("POST", "/api/v1/access-keys", string(input), "admin", strings.Repeat("z", 32))
	var issued access.Issued
	if w.Code != http.StatusCreated || json.Unmarshal(w.Body.Bytes(), &issued) != nil || issued.Secret == "" {
		t.Fatalf("direct issue failed: %d", w.Code)
	}
	if w := call("POST", "/api/v1/access-key-requests", `{}`, "admin", strings.Repeat("z", 32)); w.Code == http.StatusCreated || w.Code == http.StatusAccepted {
		t.Fatal("application endpoint still accepted")
	}
	if w := call("GET", "/api/v1/access-keys", "", issued.Key.ID, issued.Secret); w.Code != http.StatusForbidden {
		t.Fatal("restart key gained key management")
	}
	if w := call("GET", "/api/v1/access-keys", "", "admin", strings.Repeat("z", 32)); strings.Contains(w.Body.String(), issued.Secret) || strings.Contains(w.Body.String(), "secret_hash") {
		t.Fatal("list leaked secret")
	}
	if w := call("POST", "/api/v1/access-keys/"+issued.Key.ID+"/revoke", `{}`, "admin", strings.Repeat("z", 32)); w.Code != http.StatusOK {
		t.Fatal("revoke failed")
	}
	if w := call("GET", "/version", "", issued.Key.ID, issued.Secret); w.Code != http.StatusUnauthorized {
		t.Fatal("revoked key accepted")
	}
}
