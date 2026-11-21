package center

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/johankoi91/monitor/runtime/internal/identity"
)

func TestUnknownAccountRoutesDoNotQuerySecurityAudit(t *testing.T) {
	// No database is attached: an unknown auth path must be rejected before any
	// query, including the retired security-audit fallback, can run.
	secret := strings.Repeat("z", 32)
	handler := (&Server{
		Store: setup(t), Accounts: &identity.Service{},
		AdminID: "admin", AdminSecret: secret,
	}).Handler()
	for _, path := range []string{
		"/api/v1/auth/unknown", "/api/v1/auth/audit", "/api/v1/auth/me/extra",
		"/api/v1/auth/unknown?limit=invalid",
	} {
		t.Run(path, func(t *testing.T) {
			for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
				r := httptest.NewRequest(method, "http://center"+path, nil)
				r.SetBasicAuth("admin", secret)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				if w.Code != http.StatusNotFound {
					t.Fatalf("%s %s: got %d, want 404", method, path, w.Code)
				}
			}
		})
	}
}
