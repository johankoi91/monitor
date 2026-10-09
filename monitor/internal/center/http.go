package center

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/johankoi91/monitor/runtime/internal/access"
	"github.com/johankoi91/monitor/runtime/internal/identity"
	"github.com/johankoi91/monitor/runtime/internal/model"
	"github.com/johankoi91/monitor/runtime/internal/notify"
	"github.com/johankoi91/monitor/runtime/internal/postgres"
)

type Server struct {
	Store                *Store
	AdminID, AdminSecret string
	Assets               http.Handler
	AllowedIPs           []string
	Notifier             *notify.Notifier
	Access               *access.Registry
	Accounts             *identity.Service
	Database             *postgres.DB
	StorageWarningBytes  int64
	StorageDiskPath      string
}

func equal(a, b string) bool {
	x, y := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(x[:], y[:]) == 1
}
func send(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, status int, code, message string) {
	send(w, status, map[string]any{"code": code, "message": message, "request_id": ID()})
}
func writeProblem(w http.ResponseWriter, err error) {
	if p, ok := err.(*Problem); ok {
		failure(w, p.Status, p.Code, p.Message)
	} else {
		failure(w, 500, "INTERNAL_ERROR", "请求未完成")
	}
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		failure(w, 415, "CONTENT_TYPE", "请使用 application/json")
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil || d.Decode(&struct{}{}) != io.EOF {
		failure(w, 400, "INVALID_REQUEST", "请求字段不合法或含多余内容")
		return false
	}
	return true
}
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; object-src 'none'; frame-ancestors 'none'")
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			failure(w, 403, "SOURCE_DENIED", "来源不允许")
			return
		}
		allowed := len(s.AllowedIPs) == 0
		for _, value := range s.AllowedIPs {
			if value == ip {
				allowed = true
			}
		}
		if !allowed {
			failure(w, 403, "SOURCE_DENIED", "来源不允许")
			return
		}
		// Load the login shell without a native Basic challenge. The shell contains
		// no monitoring data or credentials; every API still requires Basic Auth.
		path := r.URL.Path
		if path == "/" || path == "/index.html" || path == "/app.js" || path == "/style.css" || path == "/favicon.ico" || strings.HasPrefix(path, "/assets/") {
			if r.Method != "GET" && r.Method != "HEAD" {
				method(w)
				return
			}
			if s.Assets == nil {
				http.NotFound(w, r)
				return
			}
			if path == "/" || path == "/index.html" {
				serveLoginShell(w, r)
				return
			}
			s.Assets.ServeHTTP(w, r)
			return
		}
		id, secret, ok := r.BasicAuth()
		if s.publicAuth(w, r, ip) {
			return
		}
		valid := ok && equal(id, s.AdminID) && equal(secret, s.AdminSecret)
		purpose := "ADMIN"
		var account *identity.User
		canManageKeys := valid
		if s.Accounts != nil && strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			u, e := s.Accounts.Authenticate(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			if e != nil {
				identityProblem(w, e)
				return
			}
			account = &u
			valid = true
			purpose = "ACCOUNT"
			id = "user:" + u.ID
			canManageKeys = identity.Has(u, "keys.manage")
		}
		var accessKey access.Key
		limitedKeyOwner := false
		if !valid && s.Access != nil && ok {
			accessKey, valid = s.Access.Authenticate(id, secret)
			purpose = accessKey.Purpose
		}
		if r.URL.Path == "/api/v1/agent/connect" {
			a, exists := s.Store.Agent(id)
			valid = ok && exists && equal(secret, a.Secret)
		}
		if !valid {
			w.Header().Set("WWW-Authenticate", `Basic realm="avops", charset="UTF-8"`)
			failure(w, 401, "UNAUTHORIZED", "需要有效的系统凭证")
			return
		}
		if s.Accounts != nil && path != "/api/v1/agent/connect" {
			if account != nil {
				p := routePermission(path, r.Method)
				if p != "" && !identity.Has(*account, p) && !(path == "/api/v1/roles" && r.Method == "GET" && identity.Has(*account, "users.manage")) {
					s.Accounts.Audit(id, ip, "ACCESS_DENIED", path, "DENIED", map[string]string{"permission": p})
					failure(w, 403, "PERMISSION_DENIED", "当前角色没有此操作权限")
					return
				}
			}
			if account == nil && purpose != "ADMIN" && (strings.HasPrefix(path, "/api/v1/users") || strings.HasPrefix(path, "/api/v1/roles") || path == "/api/v1/permissions" || strings.HasPrefix(path, "/api/v1/auth/")) {
				failure(w, 403, "ACCOUNT_MANAGEMENT_DENIED", "需要有权限的账号会话")
				return
			}
			if account == nil && purpose != "ADMIN" && accessKey.OwnerID == "" {
				p := routePermission(path, r.Method)
				customer := identity.User{Permissions: []string{"monitor.read", "notifications.write"}}
				if p != "" && !identity.Has(customer, p) {
					failure(w, 403, "KEY_OWNER_REQUIRED", "此模块仅 Agora 人员可访问；机器 Key 须绑定有权限的 Agora 账号")
					return
				}
			}
			if accessKey.OwnerID != "" {
				owner, e := s.Accounts.User(accessKey.OwnerID)
				p := routePermission(path, r.Method)
				if e != nil || owner.Status != "ACTIVE" || p != "" && !identity.Has(owner, p) {
					failure(w, 403, "KEY_OWNER_DENIED", "密钥所属账号已停用或权限不足")
					return
				}
				limitedKeyOwner = !identity.Has(owner, "operations.read")
			}
			// Customer operators can follow their own accepted restarts without
			// gaining access to the global operation or audit modules.
			if r.Method == "GET" && strings.HasPrefix(path, "/api/v1/operations/") && path != "/api/v1/operations/mine" {
				limited := account != nil && !identity.Has(*account, "operations.read")
				if limited {
					o, err := s.Store.Operation(strings.TrimPrefix(path, "/api/v1/operations/"))
					if err != nil {
						writeProblem(w, err)
						return
					}
					if !strings.HasPrefix(o.AuthenticatedSource, "user:"+account.ID+"|key:") {
						failure(w, 403, "OPERATION_OWNER_DENIED", "只能查看自己发起的重启结果")
						return
					}
				}
				if limitedKeyOwner {
					o, err := s.Store.Operation(strings.TrimPrefix(path, "/api/v1/operations/"))
					if err != nil {
						writeProblem(w, err)
						return
					}
					if o.AuthenticatedSource != accessKey.ID && o.AuthenticatedSource != "user:"+accessKey.OwnerID+"|key:"+accessKey.ID {
						failure(w, 403, "OPERATION_OWNER_DENIED", "此 Key 只能查询自己发起的操作")
						return
					}
				}
			}
		}
		if s.Access != nil && path != "/api/v1/agent/connect" {
			restartWrite := r.Method == "POST" && (path == "/api/v1/operations/restart" || strings.HasSuffix(path, "/resolve"))
			if restartWrite && account != nil {
				var keyValid bool
				accessKey, keyValid = s.Access.Authenticate(r.Header.Get("X-AVOPS-Restart-Key-ID"), r.Header.Get("X-AVOPS-Restart-Key-Secret"))
				if !keyValid || accessKey.Purpose != "RESTART" {
					failure(w, 403, "RESTART_KEY_REQUIRED", "角色权限之外仍须独立重启安全 Key")
					return
				}
				if accessKey.OwnerID != "" && accessKey.OwnerID != account.ID {
					failure(w, 403, "KEY_OWNER_DENIED", "请使用属于当前账号的重启 Key")
					return
				}
				if !identity.Has(*account, "operations.read") && accessKey.OwnerID != account.ID {
					failure(w, 403, "KEY_OWNER_REQUIRED", "客户重启须使用绑定本人账号的 Key")
					return
				}
				id += "|key:" + accessKey.ID
				purpose = "RESTART"
			}
			if restartWrite && purpose != "RESTART" {
				failure(w, 403, "RESTART_KEY_REQUIRED", "此操作需要已注册的重启安全 Key，普通机器凭证不具备重启权限")
				return
			}
			if strings.HasPrefix(path, "/api/v1/access-keys") && !canManageKeys {
				failure(w, 403, "KEY_MANAGEMENT_DENIED", "仅 Agora 人员账号可以签发或停用密钥")
				return
			}
			if purpose == "RESTART" && account == nil && !(path == "/version" || path == "/api/v1/operations/restart" || strings.HasPrefix(path, "/api/v1/operations/")) {
				failure(w, 403, "KEY_PURPOSE_DENIED", "重启安全 Key 仅用于其节点范围的操作接口")
				return
			}
			if purpose == "RESTART" && strings.HasPrefix(path, "/api/v1/operations/") && path != "/api/v1/operations/restart" {
				parts := strings.Split(strings.TrimPrefix(path, "/api/v1/operations/"), "/")
				o, err := s.Store.Operation(parts[0])
				if err != nil {
					writeProblem(w, err)
					return
				}
				if !s.Access.AllowsNode(accessKey, o.NodeID) {
					failure(w, 403, "KEY_NODE_DENIED", "重启安全 Key 未获准操作该节点")
					return
				}
			}
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			// A same-origin write requires the JS header. Native API tools without
			// Origin still work; cross-origin Basic-auth CSRF is rejected.
			origin := r.Header.Get("Origin")
			expected := "https://" + r.Host
			if r.TLS == nil {
				expected = "http://" + r.Host
			}
			if origin != "" && (origin != expected || r.Header.Get("X-AVOPS-Request") != "1") {
				failure(w, 403, "ORIGIN_DENIED", "跨站写入不允许")
				return
			}
		}
		if s.accountRoutes(w, r, account, id, ip) {
			return
		}
		switch {
		case path == "/api/v1/access-keys/owners":
			if r.Method != "GET" {
				method(w)
				return
			}
			if s.Accounts == nil {
				send(w, 200, map[string]any{"users": []any{}})
				return
			}
			v, e := s.Accounts.Users("ACTIVE", "", 100, 0)
			if e != nil {
				identityProblem(w, e)
			} else {
				send(w, 200, v)
			}
		case path == "/api/v1/baseline/history":
			if r.Method != "GET" {
				method(w)
				return
			}
			if s.Database == nil {
				failure(w, 503, "DATABASE_REQUIRED", "台账历史需要 PostgreSQL")
				return
			}
			limit, e := queryLimit(r, 20)
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			if e != nil || offset < 0 {
				failure(w, 400, "INVALID_PAGE", "分页参数无效")
				return
			}
			v, e := s.Database.Baselines(limit, offset)
			if e != nil {
				failure(w, 503, "QUERY_UNAVAILABLE", "历史查询暂不可用")
			} else {
				send(w, 200, v)
			}
		case path == "/api/v1/access-keys":
			if s.Access == nil {
				failure(w, 503, "KEY_REGISTRY_UNAVAILABLE", "密钥注册服务不可用")
				return
			}
			if r.Method == "GET" {
				send(w, 200, s.Access.List())
				return
			}
			if r.Method != "POST" {
				method(w)
				return
			}
			var input access.IssueRequest
			if !decode(w, r, &input) {
				return
			}
			if s.Accounts != nil && input.OwnerID != "" {
				owner, e := s.Accounts.User(input.OwnerID)
				if e != nil || owner.Status != "ACTIVE" || input.Purpose == "RESTART" && !identity.Has(owner, "restart.execute") {
					failure(w, 400, "INVALID_KEY_OWNER", "所属账号不可用或无重启权限")
					return
				}
			}
			for _, node := range input.AllowedNodes {
				if _, err := s.findNodeForKey(node); err != nil {
					writeProblem(w, err)
					return
				}
			}
			issued, err := s.Access.Issue(input, id+"/"+ip)
			if err != nil {
				failure(w, 400, "KEY_ISSUE_REJECTED", err.Error())
				return
			}
			send(w, 201, issued)
		case strings.HasPrefix(path, "/api/v1/access-keys/") && strings.HasSuffix(path, "/revoke"):
			if r.Method != "POST" {
				method(w)
				return
			}
			parts := strings.Split(strings.TrimPrefix(path, "/api/v1/access-keys/"), "/")
			if len(parts) != 2 || s.Access == nil {
				failure(w, 404, "NOT_FOUND", "密钥不存在")
				return
			}
			if err := s.Access.Revoke(parts[0], id+"/"+ip); err != nil {
				failure(w, 400, "KEY_REVOKE_FAILED", err.Error())
				return
			}
			send(w, 200, map[string]any{"revoked": true})
		case path == "/api/v1/operations/restart":
			if r.Method != "POST" {
				method(w)
				return
			}
			var request model.RestartRequest
			if !decode(w, r, &request) {
				return
			}
			if s.Access != nil && !s.Access.AllowsNode(accessKey, request.NodeID) {
				failure(w, 403, "KEY_NODE_DENIED", "重启安全 Key 未获准操作该节点")
				return
			}
			var o model.Operation
			var err error
			if account != nil && !identity.Has(*account, "operations.read") {
				o, err = s.Store.CreateCustomerRestart(request, id, ip, "user:"+account.ID+"|key:")
			} else if limitedKeyOwner {
				o, err = s.Store.CreateCustomerRestart(request, id, ip, accessKey.ID)
			} else {
				o, err = s.Store.CreateRestart(request, id, ip)
			}
			if err != nil {
				writeProblem(w, err)
				return
			}
			send(w, 202, map[string]any{"code": 0, "message": "OK", "request_id": ID(), "operation": o})
		case path == "/api/v1/operations/mine":
			if r.Method != "GET" {
				method(w)
				return
			}
			if account == nil {
				failure(w, 403, "ACCOUNT_REQUIRED", "需要账号会话")
				return
			}
			own := s.Store.OwnOperations("user:" + account.ID + "|key:")
			send(w, 200, map[string]any{"operations": own})
		case path == "/api/v1/operations":
			if r.Method != "GET" {
				method(w)
				return
			}
			limit, err := queryLimit(r, 50)
			if err != nil {
				writeProblem(w, err)
				return
			}
			if r.URL.Query().Get("active") == "true" {
				send(w, 200, map[string]any{"operations": s.Store.ActiveOperations()})
				return
			}
			if s.Database != nil {
				offset := 0
				if v := r.URL.Query().Get("offset"); v != "" {
					offset, err = strconv.Atoi(v)
				}
				if err != nil || offset < 0 {
					failure(w, 400, "INVALID_PAGE", "分页参数无效")
					return
				}
				result, e := s.Database.Operations(r.URL.Query().Get("status"), r.URL.Query().Get("node_id"), r.URL.Query().Get("source"), limit, offset)
				if e != nil {
					failure(w, 503, "QUERY_UNAVAILABLE", "操作查询暂不可用")
				} else {
					send(w, 200, result)
				}
				return
			}
			send(w, 200, map[string]any{"operations": s.Store.OperationList(limit)})
		case strings.HasPrefix(path, "/api/v1/operations/"):
			parts := strings.Split(strings.TrimPrefix(path, "/api/v1/operations/"), "/")
			if len(parts) == 2 && parts[1] == "resolve" {
				if r.Method != "POST" {
					method(w)
					return
				}
				var request model.ResolveRequest
				if !decode(w, r, &request) {
					return
				}
				o, err := s.Store.Resolve(parts[0], request, id, ip)
				if err != nil {
					writeProblem(w, err)
					return
				}
				send(w, 200, map[string]any{"code": 0, "message": "OK", "request_id": ID(), "operation": o})
				return
			}
			if len(parts) == 2 && parts[1] == "audit" {
				if r.Method != "GET" {
					method(w)
					return
				}
				records, err := s.Store.Audit(parts[0])
				if err != nil {
					writeProblem(w, err)
					return
				}
				send(w, 200, map[string]any{"records": records})
				return
			}
			if len(parts) != 1 {
				failure(w, 404, "NOT_FOUND", "路径不存在")
				return
			}
			if r.Method != "GET" {
				method(w)
				return
			}
			o, err := s.Store.Operation(parts[0])
			if err != nil {
				writeProblem(w, err)
				return
			}
			send(w, 200, map[string]any{"code": 0, "message": "OK", "request_id": ID(), "operation": o})
		case path == "/api/v1/notifications/test":
			if r.Method != "POST" {
				method(w)
				return
			}
			if s.Notifier == nil {
				failure(w, 503, "NOTIFIER_UNAVAILABLE", "通知服务不可用")
				return
			}
			result, err := s.Notifier.Probe(r.Context(), s.Store.config.Site.Code)
			if err != nil {
				failure(w, 400, "WEBHOOK_TEST_FAILED", err.Error())
				return
			}
			send(w, 200, result)
		case path == "/api/v1/notifications/config":
			if s.Notifier == nil {
				failure(w, 503, "NOTIFIER_UNAVAILABLE", "通知服务不可用")
				return
			}
			if r.Method == "GET" {
				send(w, 200, s.Notifier.Settings())
				return
			}
			if r.Method != "POST" {
				method(w)
				return
			}
			var request notify.SettingsRequest
			if !decode(w, r, &request) {
				return
			}
			forbidden := []string{s.AdminSecret}
			for _, a := range s.Store.config.Agents {
				forbidden = append(forbidden, a.Secret)
			}
			settings, err := s.Notifier.UpdateSettings(request, forbidden)
			if err != nil {
				switch {
				case errors.Is(err, notify.ErrSettingsConflict):
					failure(w, 409, "NOTIFICATION_CONFIG_CONFLICT", "配置已变化，请刷新后重新保存")
				case errors.Is(err, notify.ErrSettingsWrite):
					failure(w, 503, "NOTIFICATION_CONFIG_WRITE_FAILED", "通知配置保存失败，未完成变更")
				default:
					failure(w, 400, "INVALID_NOTIFICATION_CONFIG", err.Error())
				}
				return
			}
			send(w, 200, settings)
		case path == "/api/v1/notifications/status":
			if r.Method != "GET" {
				method(w)
				return
			}
			if s.Notifier == nil {
				send(w, 200, map[string]any{"enabled": false})
				return
			}
			send(w, 200, s.Notifier.Summary())
		case path == "/api/v1/agent/connect":
			if r.Method != "GET" {
				method(w)
				return
			}
			s.agent(w, r, id)
		case path == "/api/v1/baseline/selection":
			if r.Method != "POST" {
				method(w)
				return
			}
			var request struct {
				ExpectedRevision *string           `json:"expected_revision"`
				Additions        *[]model.Addition `json:"additions"`
				Removals         *[]string         `json:"removals"`
			}
			if !decode(w, r, &request) {
				return
			}
			if request.ExpectedRevision == nil || request.Additions == nil || request.Removals == nil {
				failure(w, 400, "REQUIRED_FIELDS", "expected_revision、additions、removals 必须填写")
				return
			}
			v := model.Selection{ExpectedRevision: *request.ExpectedRevision, Additions: *request.Additions, Removals: *request.Removals}
			baseline, err := s.Store.Save(v, id)
			if err != nil {
				writeProblem(w, err)
				return
			}
			send(w, 200, baseline)
		case path == "/api/v1/baseline/checks":
			if r.Method != "POST" {
				method(w)
				return
			}
			var request struct {
				ExpectedRevision *string        `json:"expected_revision"`
				NodeID           string         `json:"node_id"`
				Checks           *[]model.Check `json:"checks"`
			}
			if !decode(w, r, &request) {
				return
			}
			if request.ExpectedRevision == nil || request.NodeID == "" || request.Checks == nil {
				failure(w, 400, "REQUIRED_FIELDS", "请提交版本、节点和完整端口清单")
				return
			}
			baseline, err := s.Store.UpdateChecks(*request.ExpectedRevision, request.NodeID, id, *request.Checks)
			if err != nil {
				writeProblem(w, err)
				return
			}
			send(w, 200, baseline)
		case path == "/api/v1/baseline/restart-policy":
			if r.Method != "POST" {
				method(w)
				return
			}
			var request struct {
				ExpectedRevision *string `json:"expected_revision"`
				NodeID           string  `json:"node_id"`
				Enabled          *bool   `json:"enabled"`
			}
			if !decode(w, r, &request) {
				return
			}
			if request.ExpectedRevision == nil || request.NodeID == "" || request.Enabled == nil {
				failure(w, 400, "REQUIRED_FIELDS", "请提交版本、节点和重启权限")
				return
			}
			baseline, err := s.Store.UpdateRestartEnabled(*request.ExpectedRevision, request.NodeID, id, *request.Enabled)
			if err != nil {
				writeProblem(w, err)
				return
			}
			send(w, 200, baseline)
		case path == "/api/v1/baseline":
			if r.Method != "GET" {
				method(w)
				return
			}
			send(w, 200, s.Store.Baseline())
		case path == "/api/v1/baseline/yaml":
			if r.Method != "GET" {
				method(w)
				return
			}
			data, revision, err := s.Store.YAML(r.URL.Query().Get("revision"))
			if err != nil {
				writeProblem(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
			w.Header().Set("Content-Disposition", `attachment; filename="services.yaml"`)
			w.Header().Set("X-Baseline-Revision", revision)
			io.WriteString(w, data)
		case path == "/api/v1/containers":
			if r.Method != "GET" {
				method(w)
				return
			}
			s.list(w, r)
		case path == "/api/v1/agents":
			if r.Method != "GET" {
				method(w)
				return
			}
			send(w, 200, s.Store.AgentSummaries())
		case strings.HasPrefix(path, "/api/v1/containers/"):
			if r.Method != "GET" {
				method(w)
				return
			}
			parts := strings.Split(strings.TrimPrefix(path, "/api/v1/containers/"), "/")
			if len(parts) != 2 {
				failure(w, 404, "NOT_FOUND", "路径不存在")
				return
			}
			detail, err := s.Store.Detail(parts[0], parts[1])
			if err != nil {
				writeProblem(w, err)
				return
			}
			send(w, 200, detail)
		case path == "/api/v1/services/status":
			if r.Method != "GET" {
				method(w)
				return
			}
			status := r.URL.Query().Get("status")
			if status != "" && status != "HEALTHY" && status != "DEGRADED" && status != "UNHEALTHY" && status != "UNKNOWN" {
				failure(w, 400, "INVALID_FILTER", "状态筛选不合法")
				return
			}
			send(w, 200, s.Store.Statuses(r.URL.Query().Get("service_code"), status))
		case path == "/api/v1/storage/status":
			if r.Method != "GET" {
				method(w)
				return
			}
			if s.Database == nil {
				send(w, 200, map[string]any{"backend": "file-dev"})
				return
			}
			status, err := s.Database.StorageStatus(r.Context(), s.StorageWarningBytes, s.StorageDiskPath)
			if err != nil {
				failure(w, 503, "STORAGE_USAGE_UNAVAILABLE", "无法读取数据库容量")
				return
			}
			send(w, 200, status)
		case path == "/health/live" || path == "/health/ready":
			if r.Method != "GET" {
				method(w)
				return
			}
			if path == "/health/ready" && !s.Store.Ready() {
				failure(w, 503, "STORAGE_UNAVAILABLE", "存储不可用")
				return
			}
			state := "UP"
			if path == "/health/ready" {
				state = "READY"
			}
			send(w, 200, map[string]any{"status": state, "checked_at": time.Now().UTC()})
		case path == "/version":
			if r.Method != "GET" {
				method(w)
				return
			}
			permissions := []string{}
			if account != nil {
				permissions = account.Permissions
			} else if purpose == "ADMIN" {
				if s.Accounts != nil && accessKey.OwnerID != "" {
					owner, e := s.Accounts.User(accessKey.OwnerID)
					if e != nil || owner.Status != "ACTIVE" {
						failure(w, 403, "KEY_OWNER_DENIED", "密钥所属账号已停用或不可用")
						return
					}
					permissions = owner.Permissions
				} else if purpose == "ADMIN" || s.Accounts == nil {
					permissions = identity.RolePermissions(identity.AgoraRole)
				} else {
					permissions = identity.RolePermissions(identity.CustomerRole)
				}
				if purpose != "ADMIN" {
					visible := []string{}
					for _, p := range permissions {
						if p != "users.manage" && p != "roles.manage" && p != "keys.manage" {
							visible = append(visible, p)
						}
					}
					permissions = visible
				}
			}
			storageBackend := "file-dev"
			if s.Database != nil {
				storageBackend = "postgresql"
			}
			send(w, 200, map[string]any{"version": "1.0.0", "api_version": "v1", "storage_backend": storageBackend, "credential_purpose": purpose, "can_manage_keys": canManageKeys, "accounts_enabled": s.Accounts != nil, "permissions": permissions, "user": account, "capabilities": []string{"discovery", "startup", "baseline", "status", "restart", "audit", "notifications", "react_ui", "access_keys", "cadvisor_resources"}})
		case strings.HasPrefix(path, "/api/"):
			failure(w, 404, "NOT_FOUND", "接口不存在")
		default:
			if r.Method != "GET" && r.Method != "HEAD" {
				method(w)
				return
			}
			if s.Assets == nil {
				http.NotFound(w, r)
				return
			}
			s.Assets.ServeHTTP(w, r)
		}
	})
}
func method(w http.ResponseWriter) { failure(w, 405, "METHOD_NOT_ALLOWED", "请求方法不允许") }
func allowedOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	scheme := "https://"
	if r.TLS == nil {
		scheme = "http://"
	}
	return origin == "" || origin == scheme+r.Host && r.Header.Get("X-AVOPS-Request") == "1"
}
func (s *Server) findNodeForKey(id string) (model.Node, error) {
	s.Store.mu.RLock()
	defer s.Store.mu.RUnlock()
	n, _, _ := s.Store.findNodeLocked(id)
	if n.ID == "" {
		return n, problem(404, "NODE_NOT_FOUND", "密钥节点范围必须来自已保存台账")
	}
	return n, nil
}
func queryLimit(r *http.Request, fallback int) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 1 || v > 200 {
		return 0, problem(400, "INVALID_LIMIT", "limit 应为 1–200")
	}
	return v, nil
}
func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, size := 1, 50
	var err error
	if q.Get("page") != "" {
		page, err = strconv.Atoi(q.Get("page"))
		if err != nil || page < 1 || page > 100000 {
			failure(w, 400, "INVALID_PAGE", "页码不合法")
			return
		}
	}
	if q.Get("page_size") != "" {
		size, err = strconv.Atoi(q.Get("page_size"))
		if err != nil || size < 1 || size > 200 {
			failure(w, 400, "INVALID_PAGE", "每页数量须为 1–200")
			return
		}
	}
	runtime := q.Get("runtime_status")
	valid := map[string]bool{"": true, "running": true, "created": true, "exited": true, "restarting": true, "paused": true, "dead": true, "missing": true, "unknown": true, "removing": true}
	if !valid[runtime] {
		failure(w, 400, "INVALID_FILTER", "运行态不合法")
		return
	}
	filtered := []model.Container{}
	for _, c := range s.Store.Containers() {
		if q.Get("agent_id") != "" && q.Get("agent_id") != c.AgentID {
			continue
		}
		if !strings.Contains(strings.ToLower(c.ContainerName), strings.ToLower(q.Get("name"))) || !strings.Contains(strings.ToLower(c.Image), strings.ToLower(q.Get("image"))) || (runtime != "" && runtime != c.RuntimeStatus) {
			continue
		}
		filtered = append(filtered, c)
	}
	total := len(filtered)
	start := (page - 1) * size
	if start > total {
		start = total
	}
	end := start + size
	if end > total {
		end = total
	}
	send(w, 200, map[string]any{"generated_at": time.Now().UTC(), "page": page, "page_size": size, "total": total, "containers": filtered[start:end]})
}
func (s *Server) agent(w http.ResponseWriter, r *http.Request, id string) {
	// Agents aren't browsers. Do not permit a browser Origin for this channel.
	if r.Header.Get("Origin") != "" {
		failure(w, 403, "ORIGIN_DENIED", "Agent 通道不接受浏览器 Origin")
		return
	}
	conn, err := (&websocket.Upgrader{HandshakeTimeout: 5 * time.Second}).Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	session := s.Store.Connect(id)
	defer s.Store.Disconnect(id, session)
	conn.SetReadLimit(16 << 20)
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(60 * time.Second)) })
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		revision := "!"
		pingAt := time.Now()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				plan := s.Store.Plan(id)
				if plan.Revision != revision {
					conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
					if conn.WriteJSON(plan) != nil {
						conn.Close()
						return
					}
					revision = plan.Revision
				}
				tasks, acks := s.Store.Commands(id, session)
				for _, task := range tasks {
					conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
					if conn.WriteJSON(task) != nil {
						conn.Close()
						return
					}
				}
				for _, ack := range acks {
					conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
					if conn.WriteJSON(ack) != nil {
						conn.Close()
						return
					}
				}
				if time.Since(pingAt) > 20*time.Second {
					if conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)) != nil {
						conn.Close()
						return
					}
					pingAt = time.Now()
				}
			}
		}
	}()
	for {
		kind, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if kind != websocket.TextMessage {
			return
		}
		var envelope struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(data, &envelope) != nil {
			return
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		switch envelope.Type {
		case "snapshot":
			var report model.Report
			if dec.Decode(&report) != nil || dec.Decode(&struct{}{}) != io.EOF {
				return
			}
			if s.Store.Receive(id, session, report) != nil {
				return
			}
		case "task_result":
			var result model.TaskResult
			if dec.Decode(&result) != nil || dec.Decode(&struct{}{}) != io.EOF {
				return
			}
			if s.Store.ReceiveResult(id, session, result) != nil {
				return
			}
		default:
			return
		}
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	}
}
