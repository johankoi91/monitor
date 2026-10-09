// Package identity implements human accounts, revocable sessions and RBAC in PG.
package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
	"regexp"
	"strings"
	"time"
)

type Service struct {
	db    *sql.DB
	dummy []byte
}
type User struct {
	ID          string    `json:"user_id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Reason      string    `json:"reason"`
	Status      string    `json:"status"`
	Version     int       `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	Roles       []string  `json:"role_ids"`
	Permissions []string  `json:"permissions"`
}
type Role struct {
	ID          string   `json:"role_id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Builtin     bool     `json:"builtin"`
	Version     int      `json:"version"`
	Permissions []string `json:"permissions"`
}
type Permission struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

const AgoraRole = "agora"
const CustomerRole = "customer_ops"

var Catalog = []Permission{{"monitor.read", "查看服务状态与资源"}, {"inventory.read", "查看容器发现及启动配置"}, {"baseline.read", "查看及导出台账"}, {"baseline.write", "维护基准台账"}, {"notifications.write", "配置通知"}, {"operations.read", "查看重启操作"}, {"restart.result", "查看自己的重启结果"}, {"restart.execute", "执行受控重启"}, {"restart.resolve", "核实未知操作"}, {"audit.read", "查询审计"}, {"keys.manage", "签发及停用 Key"}, {"users.manage", "开通账号及分配两类角色"}, {"roles.manage", "查看固定角色权限"}}

func RolePermissions(role string) []string {
	permissions := []string{}
	for _, p := range Catalog {
		if role == AgoraRole || role == CustomerRole && (p.Code == "monitor.read" || p.Code == "notifications.write" || p.Code == "restart.execute" || p.Code == "restart.result") {
			permissions = append(permissions, p.Code)
		}
	}
	return permissions
}

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{2,63}$`)

type Problem struct {
	Status        int
	Code, Message string
}

func (p *Problem) Error() string                     { return p.Message }
func problem(status int, code, message string) error { return &Problem{status, code, message} }
func id() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func tokenHash(v string) string { b := sha256.Sum256([]byte(v)); return hex.EncodeToString(b[:]) }
func New(db *sql.DB) (*Service, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte("dummy-password-for-constant-work"), bcrypt.DefaultCost)
	return &Service{db: db, dummy: hash}, err
}
func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}
func Has(u User, permission string) bool {
	for _, p := range u.Permissions {
		if p == permission {
			return true
		}
	}
	return false
}
func (s *Service) User(userID string) (User, error) {
	context, cancel := ctx()
	defer cancel()
	return loadUser(context, s.db, userID)
}

type querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadUser(ctx context.Context, db querier, userID string) (User, error) {
	var u User
	u.Roles = []string{}
	u.Permissions = []string{}
	if err := db.QueryRowContext(ctx, "SELECT user_id,username,display_name,status,version,created_at,reason FROM avops.users WHERE user_id=$1", userID).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Status, &u.Version, &u.CreatedAt, &u.Reason); err != nil {
		if err == sql.ErrNoRows {
			return u, problem(404, "USER_NOT_FOUND", "账号不存在")
		}
		return u, err
	}
	rows, err := db.QueryContext(ctx, "SELECT role_id FROM avops.user_roles WHERE user_id=$1 ORDER BY role_id", u.ID)
	if err != nil {
		return u, err
	}
	for rows.Next() {
		var role string
		if err = rows.Scan(&role); err != nil {
			rows.Close()
			return u, err
		}
		u.Roles = append(u.Roles, role)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return u, err
	}
	rows, err = db.QueryContext(ctx, "SELECT DISTINCT rp.permission_code FROM avops.user_roles ur JOIN avops.role_permissions rp USING(role_id) WHERE ur.user_id=$1 ORDER BY rp.permission_code", u.ID)
	if err != nil {
		return u, err
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		if err = rows.Scan(&p); err != nil {
			return u, err
		}
		u.Permissions = append(u.Permissions, p)
	}
	return u, rows.Err()
}
func audit(ctx context.Context, tx *sql.Tx, actor, ip, action, target, outcome string, details any) error {
	b, _ := json.Marshal(details)
	_, err := tx.ExecContext(ctx, "INSERT INTO avops.system_audit(actor,source_ip,action,target,outcome,details) VALUES($1,$2,$3,$4,$5,$6)", actor, ip, action, target, outcome, string(b))
	return err
}
func (s *Service) Audit(actor, ip, action, target, outcome string, details any) error {
	context, cancel := ctx()
	defer cancel()
	tx, err := s.db.BeginTx(context, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = audit(context, tx, actor, ip, action, target, outcome, details); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) Bootstrap(username, password string) error {
	if !usernamePattern.MatchString(username) || len(password) < 6 || len(password) > 72 {
		return errors.New("valid bootstrap account credentials required")
	}
	context, cancel := ctx()
	defer cancel()
	tx, err := s.db.BeginTx(context, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range Catalog {
		if _, err = tx.ExecContext(context, "INSERT INTO avops.permissions(code,name) VALUES($1,$2) ON CONFLICT(code) DO UPDATE SET name=excluded.name", p.Code, p.Name); err != nil {
			return err
		}
	}
	if err = fixedRoles(context, tx); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRowContext(context, "SELECT count(*) FROM avops.users").Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		hash, e := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if e != nil {
			return e
		}
		uid := id()
		if _, err = tx.ExecContext(context, "INSERT INTO avops.users(user_id,username,display_name,password_hash,status) VALUES($1,$2,'系统管理员',$3,'ACTIVE')", uid, username, string(hash)); err != nil {
			return err
		}
		if _, err = tx.ExecContext(context, "INSERT INTO avops.user_roles(user_id,role_id) VALUES($1,$2)", uid, AgoraRole); err != nil {
			return err
		}
		if err = audit(context, tx, "bootstrap", "", "USER_BOOTSTRAP", uid, "SUCCESS", map[string]string{"username": username}); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Service) rate(ip, action string, limit int) error {
	context, cancel := ctx()
	defer cancel()
	var count int
	if err := s.db.QueryRowContext(context, "SELECT count(*) FROM avops.system_audit WHERE source_ip=$1 AND action=$2 AND at>now()-interval '15 minutes'", ip, action).Scan(&count); err != nil {
		return err
	}
	if count >= limit {
		return problem(429, "TOO_MANY_ATTEMPTS", "尝试次数过多，请稍后再试")
	}
	return nil
}

type RegisterRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Service) Register(r RegisterRequest, ip string) (User, error) {
	r.Username = strings.TrimSpace(r.Username)
	if !usernamePattern.MatchString(r.Username) || len(r.Password) < 6 || len(r.Password) > 72 {
		return User{}, problem(400, "INVALID_REGISTRATION", "请填写有效账号及 6–72 字节密码")
	}
	displayName := r.Username
	if err := s.rate(ip, "REGISTER", 10); err != nil {
		return User{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(r.Password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	context, cancel := ctx()
	defer cancel()
	tx, err := s.db.BeginTx(context, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	uid := id()
	_, err = tx.ExecContext(context, "INSERT INTO avops.users(user_id,username,display_name,password_hash,status,reason) VALUES($1,$2,$3,$4,'PENDING','')", uid, r.Username, displayName, string(hash))
	if pg, ok := err.(*pq.Error); ok && pg.Code == "23505" {
		return User{}, problem(409, "USERNAME_EXISTS", "账号名已被使用")
	}
	if err != nil {
		return User{}, err
	}
	if _, err = tx.ExecContext(context, "INSERT INTO avops.user_roles(user_id,role_id) VALUES($1,$2)", uid, CustomerRole); err != nil {
		return User{}, err
	}
	if err = audit(context, tx, uid, ip, "REGISTER", uid, "PENDING", map[string]string{}); err != nil {
		return User{}, err
	}
	if err = tx.Commit(); err != nil {
		return User{}, err
	}
	return s.User(uid)
}
func (s *Service) Login(username, password, ip string) (string, User, time.Time, error) {
	if err := s.rate(ip, "LOGIN_FAILED", 10); err != nil {
		return "", User{}, time.Time{}, err
	}
	context, cancel := ctx()
	defer cancel()
	var uid, hash, status string
	err := s.db.QueryRowContext(context, "SELECT user_id,password_hash,status FROM avops.users WHERE username=$1", strings.TrimSpace(username)).Scan(&uid, &hash, &status)
	if err != nil && err != sql.ErrNoRows {
		return "", User{}, time.Time{}, err
	}
	if err == sql.ErrNoRows {
		hash = string(s.dummy)
	}
	valid := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	if !valid || uid == "" {
		if e := s.Audit("anonymous", ip, "LOGIN_FAILED", strings.TrimSpace(username), "DENIED", map[string]string{}); e != nil {
			return "", User{}, time.Time{}, e
		}
		return "", User{}, time.Time{}, problem(401, "LOGIN_DENIED", "账号或密码不正确")
	}
	if status != "ACTIVE" {
		return "", User{}, time.Time{}, problem(403, "ACCOUNT_INACTIVE", "账号待开通或已停用")
	}
	user, e := s.User(uid)
	if e != nil {
		return "", User{}, time.Time{}, e
	}
	token := id()
	expires := time.Now().UTC().Add(8 * time.Hour)
	tx, e := s.db.BeginTx(context, nil)
	if e != nil {
		return "", User{}, time.Time{}, e
	}
	defer tx.Rollback()
	result, e := tx.ExecContext(context, "INSERT INTO avops.sessions(token_hash,user_id,expires_at) SELECT $1,user_id,$2 FROM avops.users WHERE user_id=$3 AND status='ACTIVE'", tokenHash(token), expires, uid)
	if e != nil {
		return "", User{}, time.Time{}, e
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return "", User{}, time.Time{}, problem(403, "ACCOUNT_INACTIVE", "账号状态已变化")
	}
	if e = audit(context, tx, uid, ip, "LOGIN", uid, "SUCCESS", map[string]string{}); e != nil {
		return "", User{}, time.Time{}, e
	}
	if e = tx.Commit(); e != nil {
		return "", User{}, time.Time{}, e
	}
	return token, user, expires, nil
}
func (s *Service) Authenticate(token string) (User, error) {
	context, cancel := ctx()
	defer cancel()
	var uid string
	err := s.db.QueryRowContext(context, "SELECT s.user_id FROM avops.sessions s JOIN avops.users u USING(user_id) WHERE s.token_hash=$1 AND NOT s.revoked AND s.expires_at>now() AND u.status='ACTIVE'", tokenHash(token)).Scan(&uid)
	if err == sql.ErrNoRows {
		return User{}, problem(401, "SESSION_EXPIRED", "会话失效，请重新登录")
	}
	if err != nil {
		return User{}, err
	}
	return s.User(uid)
}
func (s *Service) Logout(token, ip string) error {
	context, cancel := ctx()
	defer cancel()
	tx, err := s.db.BeginTx(context, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var uid string
	if err = tx.QueryRowContext(context, "UPDATE avops.sessions SET revoked=true WHERE token_hash=$1 RETURNING user_id", tokenHash(token)).Scan(&uid); err != nil {
		return err
	}
	if err = audit(context, tx, uid, ip, "LOGOUT", uid, "SUCCESS", map[string]string{}); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) ChangePassword(uid, oldPassword, newPassword, ip string) error {
	if len(newPassword) < 6 || len(newPassword) > 72 {
		return problem(400, "INVALID_PASSWORD", "密码需为 6–72 字节")
	}
	context, cancel := ctx()
	defer cancel()
	var hash string
	if err := s.db.QueryRowContext(context, "SELECT password_hash FROM avops.users WHERE user_id=$1", uid).Scan(&hash); err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(oldPassword)) != nil {
		return problem(400, "PASSWORD_MISMATCH", "当前密码不正确")
	}
	b, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(context, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(context, "UPDATE avops.users SET password_hash=$1,version=version+1,updated_at=now() WHERE user_id=$2 AND password_hash=$3", string(b), uid, hash)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return problem(409, "PASSWORD_CONFLICT", "密码已变化，请重新登录")
	}
	if _, err = tx.ExecContext(context, "UPDATE avops.sessions SET revoked=true WHERE user_id=$1", uid); err != nil {
		return err
	}
	if err = audit(context, tx, uid, ip, "PASSWORD_CHANGE", uid, "SUCCESS", map[string]string{}); err != nil {
		return err
	}
	return tx.Commit()
}
