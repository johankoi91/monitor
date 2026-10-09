package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/johankoi91/monitor/runtime/internal/identity"
)

// Only the loopback simulated fixture can enable this adapter. No production
// identity storage, credentials, permissions or Docker writes are involved.
func accountFixture(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		write := func(status int, v any) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(v)
		}
		user := identity.User{ID: "ui-fixture", Username: "fixture", DisplayName: "模拟验证账号", Status: "ACTIVE", Roles: []string{identity.AgoraRole}, Permissions: identity.RolePermissions(identity.AgoraRole)}
		if r.URL.Path == "/api/v1/auth/login" {
			var input struct{ Username, Password string }
			if json.NewDecoder(r.Body).Decode(&input) != nil || input.Username != "fixture" || input.Password != "fixture123" {
				write(401, map[string]string{"message": "模拟账号为 fixture / fixture123"})
				return
			}
			write(200, map[string]any{"token": "ui-fixture-token", "user": user})
			return
		}
		if r.Header.Get("Authorization") != "Bearer ui-fixture-token" {
			next.ServeHTTP(w, r)
			return
		}
		switch r.URL.Path {
		case "/version":
			write(200, map[string]any{"credential_purpose": "ACCOUNT", "can_manage_keys": true, "user": user, "permissions": user.Permissions})
		case "/api/v1/auth/logout":
			write(200, map[string]bool{"logged_out": true})
		case "/api/v1/access-keys/owners", "/api/v1/users":
			write(200, map[string]any{"users": []identity.User{user}, "total": 1})
		case "/api/v1/roles":
			write(200, map[string]any{"roles": []identity.Role{{ID: identity.AgoraRole, Name: "Agora 人员", Builtin: true}, {ID: identity.CustomerRole, Name: "客户运维", Builtin: true}}})
		case "/api/v1/audit/system":
			write(200, map[string]any{"records": []any{}, "total": 0})
		default:
			request := r.Clone(r.Context())
			request.Header = r.Header.Clone()
			request.SetBasicAuth("fixture", strings.Repeat("z", 32))
			next.ServeHTTP(w, request)
		}
	})
}
