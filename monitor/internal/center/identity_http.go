package center

import (
	"errors"
	"github.com/johankoi91/monitor/runtime/internal/identity"
	"net/http"
	"strconv"
	"strings"
)

func identityProblem(w http.ResponseWriter, err error) {
	var p *identity.Problem
	if errors.As(err, &p) {
		failure(w, p.Status, p.Code, p.Message)
	} else {
		failure(w, 503, "ACCOUNT_STORAGE_UNAVAILABLE", "账号存储暂不可用")
	}
}
func (s *Server) publicAuth(w http.ResponseWriter, r *http.Request, ip string) bool {
	if s.Accounts == nil || (r.URL.Path != "/api/v1/auth/register" && r.URL.Path != "/api/v1/auth/login") {
		return false
	}
	if r.Method != "POST" {
		method(w)
		return true
	}
	if !allowedOrigin(r) {
		failure(w, 403, "ORIGIN_DENIED", "跨站登录或注册不允许")
		return true
	}
	if r.URL.Path == "/api/v1/auth/register" {
		var input identity.RegisterRequest
		if !decode(w, r, &input) {
			return true
		}
		u, err := s.Accounts.Register(input, ip)
		if err != nil {
			identityProblem(w, err)
			return true
		}
		send(w, 202, map[string]any{"user_id": u.ID, "status": u.Status, "message": "注册申请已提交，等待管理员开通"})
		return true
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &input) {
		return true
	}
	if len(input.Username) > 64 || len(input.Password) > 72 {
		failure(w, 400, "INVALID_LOGIN", "账号或密码格式无效")
		return true
	}
	token, u, expires, err := s.Accounts.Login(input.Username, input.Password, ip)
	if err != nil {
		identityProblem(w, err)
		return true
	}
	send(w, 200, map[string]any{"token": token, "expires_at": expires, "user": u})
	return true
}
func (s *Server) accountRoutes(w http.ResponseWriter, r *http.Request, user *identity.User, actor, ip string) bool {
	path := r.URL.Path
	if s.Accounts == nil || !(strings.HasPrefix(path, "/api/v1/auth/") || path == "/api/v1/users" || strings.HasPrefix(path, "/api/v1/users/") || path == "/api/v1/roles" || strings.HasPrefix(path, "/api/v1/roles/") || path == "/api/v1/permissions") {
		return false
	}
	if path == "/api/v1/auth/me" {
		if r.Method != "GET" {
			method(w)
		} else if user == nil {
			failure(w, 400, "ACCOUNT_SESSION_REQUIRED", "请使用账号登录")
		} else {
			send(w, 200, map[string]any{"user": user})
		}
		return true
	}
	if path == "/api/v1/auth/logout" {
		if r.Method != "POST" {
			method(w)
		} else if user == nil {
			failure(w, 400, "ACCOUNT_SESSION_REQUIRED", "请使用账号登录")
		} else if err := s.Accounts.Logout(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), ip); err != nil {
			identityProblem(w, err)
		} else {
			send(w, 200, map[string]bool{"logged_out": true})
		}
		return true
	}
	if path == "/api/v1/auth/password" {
		if r.Method != "POST" {
			method(w)
			return true
		}
		if user == nil {
			failure(w, 400, "ACCOUNT_SESSION_REQUIRED", "请使用账号登录")
			return true
		}
		var input struct {
			Old string `json:"old_password"`
			New string `json:"new_password"`
		}
		if !decode(w, r, &input) {
			return true
		}
		if err := s.Accounts.ChangePassword(user.ID, input.Old, input.New, ip); err != nil {
			identityProblem(w, err)
		} else {
			send(w, 200, map[string]any{"message": "密码已修改，请重新登录"})
		}
		return true
	}
	if path == "/api/v1/permissions" {
		if r.Method != "GET" {
			method(w)
		} else {
			send(w, 200, map[string]any{"permissions": identity.Catalog})
		}
		return true
	}
	if path == "/api/v1/roles" && r.Method == "GET" {
		roles, err := s.Accounts.Roles()
		if err != nil {
			identityProblem(w, err)
		} else {
			send(w, 200, map[string]any{"roles": roles})
		}
		return true
	}
	if path == "/api/v1/roles" || strings.HasPrefix(path, "/api/v1/roles/") {
		if r.Method != "POST" {
			method(w)
			return true
		}
		id := strings.TrimPrefix(path, "/api/v1/roles/")
		if path == "/api/v1/roles" {
			id = ""
		}
		var input identity.RoleUpdate
		if !decode(w, r, &input) {
			return true
		}
		role, err := s.Accounts.SaveRole(id, input, actor, ip)
		if err != nil {
			identityProblem(w, err)
		} else {
			send(w, 200, role)
		}
		return true
	}
	if strings.HasPrefix(path, "/api/v1/users/") {
		if r.Method != "POST" {
			method(w)
			return true
		}
		var input identity.UserUpdate
		if !decode(w, r, &input) {
			return true
		}
		u, err := s.Accounts.UpdateUser(strings.TrimPrefix(path, "/api/v1/users/"), input, actor, ip)
		if err != nil {
			identityProblem(w, err)
		} else {
			send(w, 200, u)
		}
		return true
	}
	if r.Method != "GET" {
		method(w)
		return true
	}
	limit, offset := 50, 0
	var err error
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, err = strconv.Atoi(v)
	}
	if err == nil {
		if v := r.URL.Query().Get("offset"); v != "" {
			offset, err = strconv.Atoi(v)
		}
	}
	if err != nil {
		failure(w, 400, "INVALID_PAGE", "分页参数无效")
		return true
	}
	var result any
	if path == "/api/v1/users" {
		result, err = s.Accounts.Users(r.URL.Query().Get("status"), r.URL.Query().Get("search"), limit, offset)
	} else {
		result, err = s.Accounts.AuditList(r.URL.Query().Get("action"), r.URL.Query().Get("actor"), limit, offset)
	}
	if err != nil {
		identityProblem(w, err)
	} else {
		send(w, 200, result)
	}
	return true
}
func routePermission(path, method string) string {
	switch {
	case path == "/version" || path == "/health/live" || path == "/health/ready" || strings.HasPrefix(path, "/api/v1/auth/"):
		return ""
	case path == "/api/v1/users" || strings.HasPrefix(path, "/api/v1/users/"):
		return "users.manage"
	case path == "/api/v1/roles" || strings.HasPrefix(path, "/api/v1/roles/") || path == "/api/v1/permissions":
		return "roles.manage"
	case strings.HasPrefix(path, "/api/v1/access-keys"):
		return "keys.manage"
	case path == "/api/v1/access-key-requests":
		return "keys.manage"
	case strings.HasPrefix(path, "/api/v1/containers"):
		return "inventory.read"
	case path == "/api/v1/operations/restart":
		return "restart.execute"
	case path == "/api/v1/operations/mine":
		return "restart.result"
	case strings.HasSuffix(path, "/resolve"):
		return "restart.resolve"
	case strings.HasSuffix(path, "/audit"):
		return "audit.read"
	case method == "GET" && strings.HasPrefix(path, "/api/v1/operations/") && !strings.Contains(strings.TrimPrefix(path, "/api/v1/operations/"), "/"):
		return "restart.result"
	case strings.HasPrefix(path, "/api/v1/operations"):
		return "operations.read"
	case strings.HasPrefix(path, "/api/v1/baseline") && method == "POST":
		return "baseline.write"
	case strings.HasPrefix(path, "/api/v1/baseline"):
		return "baseline.read"
	case (path == "/api/v1/notifications/config" || path == "/api/v1/notifications/test") && method == "POST":
		return "notifications.write"
	default:
		return "monitor.read"
	}
}
