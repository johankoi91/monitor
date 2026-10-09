package postgres_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/johankoi91/monitor/runtime/internal/access"
	"github.com/johankoi91/monitor/runtime/internal/center"
	"github.com/johankoi91/monitor/runtime/internal/identity"
	"github.com/johankoi91/monitor/runtime/internal/model"
	"github.com/johankoi91/monitor/runtime/internal/notify"
	"github.com/johankoi91/monitor/runtime/internal/postgres"
)

func TestPostgreSQLMigrationAccountsPermissionsAndRecovery(t *testing.T) {
	dsn := os.Getenv("AVOPS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL fixture required")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Path != "/avops_test" {
		t.Fatal("test refuses any database except separately provisioned avops_test")
	}
	pool, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec("DROP SCHEMA IF EXISTS avops CASCADE"); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{42}, 32)
	db, err := postgres.Open(dsn, key, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if db != nil {
			db.Close()
		}
	}()
	if duplicate, e := postgres.Open(dsn, key, false); e == nil {
		duplicate.Close()
		t.Fatal("second center writer accepted")
	}
	dir := t.TempDir()
	cfg := center.Config{Site: model.Site{Code: "pgqa", Name: "PG Test"}, Agents: []center.AgentConfig{{ID: "qa", Secret: strings.Repeat("a", 32), HostName: "qa", HostAddress: "127.0.0.1"}}, RestartRules: []model.RestartRule{{AgentID: "qa", ContainerName: "canary", ServiceCode: "canary", Image: "canary:test", Approved: true, ApprovalRef: "isolated PG fixture"}}}
	legacy, e := center.NewStore(dir, cfg)
	if e != nil {
		t.Fatal(e)
	}
	at := time.Now().UTC()
	report := model.Report{Type: "snapshot", Complete: true, Sequence: 1, CollectedAt: at, Containers: []model.Observation{{Container: model.Container{ContainerID: strings.Repeat("a", 64), ContainerName: "canary", SnapshotID: "snapshot", Image: "canary:test", RuntimeStatus: "running", CollectedAt: at}, Startup: &model.Startup{SnapshotID: "snapshot", CollectedAt: at, Config: model.StartupConfig{Image: "canary:test"}}, StartedAt: "2020-01-01T00:00:00Z"}}}
	session := legacy.Connect("qa")
	if e = legacy.Receive("qa", session, report); e != nil {
		t.Fatal(e)
	}
	baseline, e := legacy.Save(model.Selection{Additions: []model.Addition{{AgentID: "qa", ContainerID: strings.Repeat("a", 64), SnapshotID: "snapshot", ClusterCode: "pgqa", ServiceCode: "canary", ServiceName: "Canary"}}, Removals: []string{}}, "legacy")
	if e != nil {
		t.Fatal(e)
	}
	if e = legacy.ApplyRestartRules(); e != nil {
		t.Fatal(e)
	}
	request := model.RestartRequest{NodeID: "qa/canary", Operator: "test", Reason: "isolated migration", RequestKey: "old-idempotency"}
	op, e := legacy.CreateRestart(request, "old-key", "127.0.0.1")
	if e != nil {
		t.Fatal(e)
	}
	baseline = legacy.Baseline()
	legacy.Close()
	keys, e := access.New(dir)
	if e != nil {
		t.Fatal(e)
	}
	issued, e := keys.Issue(access.IssueRequest{Name: "legacy restart", Purpose: "RESTART", AllowedNodes: []string{"qa/canary"}, ApprovalReason: "fixture"}, "legacy")
	if e != nil {
		t.Fatal(e)
	}
	keys.Close()
	n, e := notify.New(dir, notify.Config{})
	if e != nil {
		t.Fatal(e)
	}
	settings := n.Settings()
	_, e = n.UpdateSettings(notify.SettingsRequest{ExpectedRevision: settings.Revision, URL: "https://example.com/events", Enabled: false, AuthMode: "set", Secret: "notification-private-secret"}, nil)
	if e != nil {
		t.Fatal(e)
	}
	n.Close()
	counts, e := db.ImportDirectory(dir)
	if e != nil {
		t.Fatal(e)
	}
	if counts["current-baseline.json"] != 1 || counts["operations.jsonl"] < 1 {
		t.Fatal("incomplete import")
	}
	if _, e = db.ImportDirectory(dir); e != nil {
		t.Fatal("identical import not idempotent")
	}
	var plaintext bool
	if e = pool.QueryRow("SELECT payload IS NOT NULL FROM avops.documents WHERE name='notification_settings'").Scan(&plaintext); e != nil || plaintext {
		t.Fatal("notification credentials persisted in JSONB plaintext")
	}
	store, e := center.NewStore(t.TempDir(), cfg, db)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	if store.Baseline().Revision != baseline.Revision {
		t.Fatal("baseline version changed during migration")
	}
	replay, e := store.CreateRestart(request, "old-key", "127.0.0.1")
	if e != nil || replay.OperationID != op.OperationID {
		t.Fatal("old idempotency lost")
	}
	audit, e := store.Audit(op.OperationID)
	if e != nil || len(audit) == 0 {
		t.Fatal("operation audit lost")
	}
	registry, e := access.New(t.TempDir(), db)
	if e != nil {
		t.Fatal(e)
	}
	defer registry.Close()
	if _, ok := registry.Authenticate(issued.Key.ID, issued.Secret); !ok {
		t.Fatal("legacy key lost")
	}
	notifier, e := notify.New(t.TempDir(), notify.Config{}, db)
	if e != nil {
		t.Fatal(e)
	}
	defer notifier.Close()
	if !notifier.Settings().AuthConfigured {
		t.Fatal("encrypted notification configuration not restored")
	}
	accounts, e := identity.New(db.Pool)
	if e != nil {
		t.Fatal(e)
	}
	if e = accounts.Bootstrap("admin", "strong-admin-password"); e != nil {
		t.Fatal(e)
	}
	_, admin, _, e := accounts.Login("admin", "strong-admin-password", "fixture")
	if e != nil {
		t.Fatal(e)
	}
	user, e := accounts.Register(identity.RegisterRequest{Username: "viewer", Password: "strong-viewer-password"}, "fixture")
	if e != nil {
		t.Fatal(e)
	}
	if _, _, _, e = accounts.Login("viewer", "strong-viewer-password", "fixture"); e == nil {
		t.Fatal("pending user logged in")
	}

	// Simulate the former three-role release and verify atomic policy migration.
	legacyToken, _, _, e := accounts.Login("admin", "strong-admin-password", "fixture")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(`INSERT INTO avops.roles(role_id,name,builtin) VALUES('admin','旧管理员',true),('viewer','旧只读',true);
		DELETE FROM avops.user_roles;
		INSERT INTO avops.user_roles(user_id,role_id) SELECT user_id,CASE WHEN username='admin' THEN 'admin' ELSE 'viewer' END FROM avops.users;
		DELETE FROM avops.documents WHERE name='identity-two-roles-v1';`); e != nil {
		t.Fatal(e)
	}
	if e = accounts.Bootstrap("admin", "strong-admin-password"); e != nil {
		t.Fatal(e)
	}
	if _, e = accounts.Authenticate(legacyToken); e == nil {
		t.Fatal("pre-migration session survived")
	}
	admin, e = accounts.User(admin.ID)
	if e != nil || len(admin.Roles) != 1 || admin.Roles[0] != identity.AgoraRole {
		t.Fatal("legacy admin not migrated to Agora")
	}
	user, e = accounts.User(user.ID)
	if e != nil || len(user.Roles) != 1 || user.Roles[0] != identity.CustomerRole {
		t.Fatal("legacy viewer not migrated to customer")
	}
	before := user.Version
	if e = accounts.Bootstrap("admin", "strong-admin-password"); e != nil {
		t.Fatal(e)
	}
	user, e = accounts.User(user.ID)
	if e != nil || user.Version != before {
		t.Fatal("role migration repeated on startup")
	}
	roles, e := accounts.Roles()
	if e != nil || len(roles) != 2 {
		t.Fatal("expected exactly two fixed roles")
	}
	user, e = accounts.UpdateUser(user.ID, identity.UserUpdate{Status: "ACTIVE", RoleIDs: []string{identity.CustomerRole}, ExpectedVersion: user.Version, Reason: "fixture approval"}, admin.ID, "fixture")
	if e != nil {
		t.Fatal(e)
	}
	token, _, _, e := accounts.Login("viewer", "strong-viewer-password", "fixture")
	if e != nil {
		t.Fatal(e)
	}
	handler := (&center.Server{Store: store, Access: registry, Accounts: accounts, Database: db, Notifier: notifier, AdminID: "bootstrap", AdminSecret: strings.Repeat("b", 32)}).Handler()
	post := func(path string, body any, auth string, k access.Issued) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "http://center"+path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		if auth != "" {
			r.Header.Set("Authorization", "Bearer "+auth)
		}
		if k.Key.ID != "" {
			r.Header.Set("X-AVOPS-Restart-Key-ID", k.Key.ID)
			r.Header.Set("X-AVOPS-Restart-Key-Secret", k.Secret)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}

	get := func(path, auth string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "http://center"+path, nil)
		r.Header.Set("Authorization", "Bearer "+auth)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/api/v1/baseline", "/api/v1/baseline/yaml", "/api/v1/baseline/history", "/api/v1/containers", "/api/v1/containers/qa/canary", "/api/v1/operations", "/api/v1/operations/" + op.OperationID, "/api/v1/operations/" + op.OperationID + "/audit", "/api/v1/access-keys", "/api/v1/access-keys/owners", "/api/v1/users", "/api/v1/roles", "/api/v1/permissions"} {
		if w := get(path, token); w.Code != 403 {
			t.Fatalf("customer restricted endpoint %s: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/version", "/api/v1/auth/me", "/api/v1/agents", "/api/v1/services/status", "/api/v1/notifications/config", "/api/v1/notifications/status", "/api/v1/operations/mine", "/api/v1/storage/status"} {
		if w := get(path, token); w.Code != 200 {
			t.Fatalf("customer allowed endpoint %s: %d %s", path, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/api/v1/access-key-requests", "/api/v1/users/" + user.ID, "/api/v1/roles"} {
		if w := post(path, map[string]any{}, token, access.Issued{}); w.Code != 403 {
			t.Fatalf("customer permission escalation %s: %d", path, w.Code)
		}
	}
	if w := post("/api/v1/access-key-requests", map[string]any{}, "", access.Issued{}); w.Code != 401 {
		t.Fatalf("anonymous removed key-application endpoint must require authentication: %d", w.Code)
	}
	if w := post("/api/v1/notifications/config", map[string]any{"enabled": false, "url": "", "auth_mode": "clear", "expected_revision": notifier.Settings().Revision}, token, access.Issued{}); w.Code != 200 {
		t.Fatalf("customer notification configuration denied: %d %s", w.Code, w.Body.String())
	}
	if _, e = accounts.UpdateUser(user.ID, identity.UserUpdate{Status: "ACTIVE", RoleIDs: []string{identity.AgoraRole, identity.CustomerRole}, ExpectedVersion: user.Version, Reason: "dual roles"}, admin.ID, "fixture"); e == nil {
		t.Fatal("multiple roles assigned")
	}
	if w := post("/api/v1/operations/restart", request, token, issued); w.Code != 403 {
		t.Fatalf("viewer restart bypass: %d", w.Code)
	}
	customerKey, e := registry.Issue(access.IssueRequest{OwnerID: user.ID, Name: "customer key", Purpose: "RESTART", AllowedNodes: []string{request.NodeID}, ApprovalReason: "fixture"}, "fixture")
	if e != nil {
		t.Fatal(e)
	}
	if w := post("/api/v1/operations/restart", request, token, customerKey); w.Code != 403 {
		t.Fatalf("customer replay exposed other operation: %d %s", w.Code, w.Body.String())
	}
	otherKey, e := registry.Issue(access.IssueRequest{OwnerID: admin.ID, Name: "other owner", Purpose: "RESTART", AllowedNodes: []string{request.NodeID}, ApprovalReason: "fixture"}, "fixture")
	if e != nil {
		t.Fatal(e)
	}
	if w := post("/api/v1/operations/restart", request, token, otherKey); w.Code != 403 {
		t.Fatal("customer used another account key")
	}
	// Use an isolated file-backed store to exercise customer acceptance and
	// own-result isolation without touching the legacy migration operation.
	customerStore, e := center.NewStore(t.TempDir(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer customerStore.Close()
	customerSession := customerStore.Connect("qa")
	if e = customerStore.Receive("qa", customerSession, report); e != nil {
		t.Fatal(e)
	}
	if _, e = customerStore.Save(model.Selection{Additions: []model.Addition{{AgentID: "qa", ContainerID: strings.Repeat("a", 64), SnapshotID: "snapshot", ClusterCode: "pgqa", ServiceCode: "canary", ServiceName: "Canary"}}, Removals: []string{}}, "fixture"); e != nil {
		t.Fatal(e)
	}
	if e = customerStore.ApplyRestartRules(); e != nil {
		t.Fatal(e)
	}
	originalHandler := handler
	handler = (&center.Server{Store: customerStore, Access: registry, Accounts: accounts, Database: db, Notifier: notifier}).Handler()
	acceptedResponse := post("/api/v1/operations/restart", request, token, customerKey)
	if acceptedResponse.Code != 202 {
		t.Fatalf("customer restart rejected: %d %s", acceptedResponse.Code, acceptedResponse.Body.String())
	}
	var accepted struct {
		Operation model.Operation `json:"operation"`
	}
	if json.Unmarshal(acceptedResponse.Body.Bytes(), &accepted) != nil {
		t.Fatal("invalid acceptance")
	}
	if w := get("/api/v1/operations/"+accepted.Operation.OperationID, token); w.Code != 200 {
		t.Fatal("own result denied")
	}
	if w := get("/api/v1/operations/mine", token); w.Code != 200 || !strings.Contains(w.Body.String(), accepted.Operation.OperationID) {
		t.Fatal("own restart missing")
	}
	if w := get("/api/v1/operations/"+accepted.Operation.OperationID+"/audit", token); w.Code != 403 {
		t.Fatal("customer audit access")
	}
	repeated := post("/api/v1/operations/restart", request, token, customerKey)
	if repeated.Code != 202 || !strings.Contains(repeated.Body.String(), accepted.Operation.OperationID) {
		t.Fatal("customer idempotency failed")
	}
	second, e := accounts.Register(identity.RegisterRequest{Username: "customer2", Password: "123456"}, "fixture")
	if e != nil {
		t.Fatal(e)
	}
	second, e = accounts.UpdateUser(second.ID, identity.UserUpdate{Status: "ACTIVE", RoleIDs: []string{identity.CustomerRole}, ExpectedVersion: second.Version, Reason: "fixture"}, admin.ID, "fixture")
	if e != nil {
		t.Fatal(e)
	}
	secondToken, _, _, e := accounts.Login("customer2", "123456", "fixture")
	if e != nil {
		t.Fatal(e)
	}
	if w := get("/api/v1/operations/"+accepted.Operation.OperationID, secondToken); w.Code != 403 {
		t.Fatal("customer read other customer result")
	}
	if w := get("/api/v1/operations/mine", secondToken); w.Code != 200 || strings.Contains(w.Body.String(), accepted.Operation.OperationID) {
		t.Fatal("own list leaked another customer")
	}
	handler = originalHandler
	if w := post("/api/v1/baseline/selection", map[string]any{}, token, access.Issued{}); w.Code != 403 {
		t.Fatal("viewer baseline write bypass")
	}
	if _, e = accounts.UpdateUser(admin.ID, identity.UserUpdate{Status: "DISABLED", RoleIDs: []string{identity.AgoraRole}, ExpectedVersion: admin.Version, Reason: "test protection"}, admin.ID, "fixture"); e == nil {
		t.Fatal("last admin disabled")
	}
	operator, e := accounts.Register(identity.RegisterRequest{Username: "operator", Password: "strong-operator-password"}, "fixture")
	if e != nil {
		t.Fatal(e)
	}
	operator, e = accounts.UpdateUser(operator.ID, identity.UserUpdate{Status: "ACTIVE", RoleIDs: []string{identity.AgoraRole}, ExpectedVersion: operator.Version, Reason: "approval"}, admin.ID, "fixture")
	if e != nil {
		t.Fatal(e)
	}
	short, e := accounts.Register(identity.RegisterRequest{Username: "shortpass", Password: "abc123"}, "fixture")
	if e != nil || short.Status != "PENDING" {
		t.Fatal("six-character registration password rejected")
	}
	admin, e = accounts.UpdateUser(admin.ID, identity.UserUpdate{Status: "ACTIVE", RoleIDs: []string{identity.CustomerRole}, ExpectedVersion: admin.Version, Reason: "handoff"}, admin.ID, "fixture")
	if e != nil {
		t.Fatal("admin could not be changed to customer after another Agora account was opened")
	}
	admin, e = accounts.UpdateUser(admin.ID, identity.UserUpdate{Status: "ACTIVE", RoleIDs: []string{identity.AgoraRole}, ExpectedVersion: admin.Version, Reason: "restore"}, operator.ID, "fixture")
	if e != nil {
		t.Fatal(e)
	}
	_, e = accounts.SaveRole("", identity.RoleUpdate{Name: "配置查看", Description: "只看监控", Permissions: []string{"monitor.read"}, Reason: "custom role test"}, admin.ID, "fixture")
	if e == nil {
		t.Fatal("custom role created despite fixed role policy")
	}
	if _, e = accounts.SaveRole(identity.AgoraRole, identity.RoleUpdate{Name: "bad", Permissions: []string{"monitor.read"}, ExpectedVersion: 1, Reason: "protect built in"}, admin.ID, "fixture"); e == nil {
		t.Fatal("builtin role overwritten")
	}
	opToken, _, _, e := accounts.Login("operator", "strong-operator-password", "fixture")
	if e != nil {
		t.Fatal(e)
	}
	if w := post("/api/v1/operations/restart", request, opToken, access.Issued{}); w.Code != 403 {
		t.Fatal("role alone bypassed restart key")
	}
	if w := post("/api/v1/operations/restart", request, opToken, issued); w.Code != 202 {
		t.Fatalf("registered restart key + role rejected: %d %s", w.Code, w.Body.String())
	}
	user, e = accounts.UpdateUser(user.ID, identity.UserUpdate{Status: "DISABLED", RoleIDs: []string{identity.CustomerRole}, ExpectedVersion: user.Version, Reason: "stop access"}, admin.ID, "fixture")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = accounts.Authenticate(token); e == nil {
		t.Fatal("disabled user session accepted")
	}
	if e = accounts.ChangePassword(operator.ID, "strong-operator-password", "changed-operator-password", "fixture"); e != nil {
		t.Fatal(e)
	}
	owned, e := registry.Issue(access.IssueRequest{Name: "owned", Purpose: "RESTART", OwnerID: operator.ID, AllowedNodes: []string{"qa/canary"}, ApprovalReason: "owned permission"}, admin.ID)
	if e != nil {
		t.Fatal(e)
	}
	operator, e = accounts.User(operator.ID)
	if e != nil {
		t.Fatal(e)
	}
	operator, e = accounts.UpdateUser(operator.ID, identity.UserUpdate{Status: "DISABLED", RoleIDs: []string{identity.AgoraRole}, ExpectedVersion: operator.Version, Reason: "disable owned key"}, admin.ID, "fixture")
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("POST", "http://center/api/v1/operations/restart", strings.NewReader(`{}`))
	r.SetBasicAuth(owned.Key.ID, owned.Secret)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("disabled owner key retained access")
	}
	export := t.TempDir() + "/export"
	if e = db.ExportLegacy(export); e != nil {
		t.Fatal(e)
	}
	if _, e = accounts.Authenticate(opToken); e == nil {
		t.Fatal("password change retained old sessions")
	}
	var rows int
	if e = pool.QueryRow("SELECT count(*) FROM avops.operation_audit").Scan(&rows); e != nil || rows == 0 {
		t.Fatal("relational audit empty")
	}
	var hash string
	if e = pool.QueryRow("SELECT password_hash FROM avops.users WHERE user_id=$1", operator.ID).Scan(&hash); e != nil || strings.Contains(hash, "changed-operator-password") {
		t.Fatal("plaintext password")
	}
	// A killed writer connection must fail closed and cannot silently acquire a new lease.
	if _, e = pool.Exec(`INSERT INTO avops.sessions(token_hash,user_id,expires_at,revoked) VALUES
		('expired-old',$1,now()-interval '25 hours',false),
		('expired-recent',$1,now()-interval '1 hour',false),
		('active-kept',$1,now()+interval '8 hours',false),
		('revoked-kept',$1,now()+interval '8 hours',true)`, admin.ID); e != nil {
		t.Fatal(e)
	}
	var auditBefore int
	if e = pool.QueryRow("SELECT count(*) FROM avops.operation_audit").Scan(&auditBefore); e != nil {
		t.Fatal(e)
	}
	if e = db.CleanupSessions(); e != nil {
		t.Fatal(e)
	}
	var kept int
	if e = pool.QueryRow("SELECT count(*) FROM avops.sessions WHERE token_hash IN ('expired-old','expired-recent','active-kept','revoked-kept')").Scan(&kept); e != nil || kept != 3 {
		t.Fatal("session cleanup removed live or recently expired tokens")
	}
	if e = pool.QueryRow("SELECT count(*) FROM avops.operation_audit").Scan(&rows); e != nil || rows != auditBefore {
		t.Fatal("session cleanup touched operation audit")
	}
	usage, e := db.StorageStatus(context.Background(), 1, t.TempDir())
	if e != nil || usage.DatabaseBytes <= 0 || len(usage.Warnings) == 0 || usage.Sessions.DeletedSessions != 1 {
		t.Fatalf("capacity or cleanup status invalid: %v", e)
	}
	var pid int
	if e = pool.QueryRow("SELECT pid FROM pg_locks WHERE locktype='advisory' AND classid=716017 AND objid=1 AND granted AND database=(SELECT oid FROM pg_database WHERE datname=current_database())").Scan(&pid); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec("SELECT pg_terminate_backend($1)", pid); e != nil {
		t.Fatal(e)
	}
	if db.Healthy() {
		t.Fatal("lost writer lease stayed healthy")
	}
	if e = db.SaveDocument("test", []byte(`{}`)); e == nil {
		t.Fatal("fenced writer wrote again")
	}
	store.Close()
	db.Close()
	db = nil
	recovered, e := postgres.Open(dsn, key, false)
	if e != nil {
		t.Fatal(e)
	}
	defer recovered.Close()
	copy, e := center.NewStore(t.TempDir(), cfg, recovered)
	if e != nil {
		t.Fatal(e)
	}
	defer copy.Close()
	result, e := copy.Operation(op.OperationID)
	if e != nil || !result.NodeLocked {
		t.Fatal("unknown/active operation lock not recovered")
	}
}
