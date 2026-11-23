package center

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/johankoi91/monitor/runtime/center/frontend"
)

func TestWorkspaceDeepLinksServeOnlyLoginShell(t *testing.T) {
	assets, err := fs.Sub(frontend.Files, "static")
	if err != nil {
		t.Fatal(err)
	}
	handler := (&Server{Store: setup(t), AdminID: "admin", AdminSecret: strings.Repeat("z", 32), Assets: http.FileServer(http.FS(assets))}).Handler()
	for _, path := range []string{"/overview", "/monitor/containers", "/monitor/logs", "/monitor/discovery", "/rtc/settings/servers", "/rtc/settings/retention", "/rtc/tasks", "/rtc/targets/target", "/rtc/history", "/rtc/history/round", "/rtc/history/round/paths", "/rtc/history/round/paths/case", "/rtc/history/round/paths/case/logs", "/rtc/tasks/task/coverage/ap/runs/round/paths/case", "/inventory/baseline", "/accounts/roles"} {
		t.Run(path, func(t *testing.T) {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, httptest.NewRequest(method, "http://center"+path, nil))
				if w.Code != http.StatusOK || w.Header().Get("WWW-Authenticate") != "" {
					t.Fatalf("deep link requires native challenge or failed: %d", w.Code)
				}
				if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("deep link did not receive uncached shell")
				}
				if !strings.Contains(w.Header().Get("Content-Security-Policy"), "style-src 'self' 'nonce-") {
					t.Fatal("deep link lost style nonce policy")
				}
				if strings.Contains(w.Body.String(), strings.Repeat("z", 32)) {
					t.Fatal("credentials in shell")
				}
				if method == http.MethodHead && w.Body.Len() != 0 {
					t.Fatal("HEAD returned body")
				}
			}
		})
	}
	for _, path := range []string{"/api/v1/rtc/runs/round", "/api/v1/rtc/status", "/api/v1/users"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://center"+path, nil))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("API access changed for %s: %d", path, w.Code)
		}
	}
	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "http://center/assets/does-not-exist.js", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatal("missing asset became a login shell")
	}
}
