package identity

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/lib/pq"
	"strings"
)

func ensureAdmin(tx *sql.Tx, ctx context.Context) error {
	var count int
	err := tx.QueryRowContext(ctx, "SELECT count(*) FROM avops.users u WHERE u.status='ACTIVE' AND EXISTS(SELECT 1 FROM avops.user_roles ur JOIN avops.role_permissions rp USING(role_id) WHERE ur.user_id=u.user_id AND rp.permission_code='users.manage') AND EXISTS(SELECT 1 FROM avops.user_roles ur JOIN avops.role_permissions rp USING(role_id) WHERE ur.user_id=u.user_id AND rp.permission_code='roles.manage')").Scan(&count)
	if err != nil {
		return err
	}
	if count == 0 {
		return problem(409, "LAST_ADMIN", "当前只有一个 Agora 人员账号，需先开通另一名 Agora 人员后才能改为客户运维")
	}
	return nil
}

func (s *Service) Users(status, search string, limit, offset int) (any, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, problem(400, "INVALID_PAGE", "分页参数无效")
	}
	context, cancel := ctx()
	defer cancel()
	var total int
	where := " WHERE ($1='' OR status=$1) AND ($2='' OR username ILIKE '%'||$2||'%' OR display_name ILIKE '%'||$2||'%')"
	if err := s.db.QueryRowContext(context, "SELECT count(*) FROM avops.users"+where, status, search).Scan(&total); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(context, "SELECT user_id FROM avops.users"+where+" ORDER BY created_at DESC,user_id LIMIT $3 OFFSET $4", status, search, limit, offset)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	users := []User{}
	for _, id := range ids {
		u, e := s.User(id)
		if e != nil {
			return nil, e
		}
		users = append(users, u)
	}
	return map[string]any{"total": total, "users": users}, nil
}
func (s *Service) Roles() ([]Role, error) {
	context, cancel := ctx()
	defer cancel()
	rows, err := s.db.QueryContext(context, "SELECT r.role_id,r.name,r.description,r.builtin,r.version,COALESCE(array_agg(p.permission_code ORDER BY p.permission_code) FILTER(WHERE p.permission_code IS NOT NULL),'{}') FROM avops.roles r LEFT JOIN avops.role_permissions p USING(role_id) GROUP BY r.role_id ORDER BY r.builtin DESC,r.role_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roles := []Role{}
	for rows.Next() {
		var r Role
		var perms []string
		if err = rows.Scan(&r.ID, &r.Name, &r.Description, &r.Builtin, &r.Version, pq.Array(&perms)); err != nil {
			return nil, err
		}
		r.Permissions = perms
		roles = append(roles, r)
	}
	return roles, rows.Err()
}

type UserUpdate struct {
	Status          string   `json:"status"`
	RoleIDs         []string `json:"role_ids"`
	ExpectedVersion int      `json:"expected_version"`
	Reason          string   `json:"reason"`
}

func (s *Service) UpdateUser(uid string, r UserUpdate, actor, ip string) (User, error) {
	if (r.Status != "ACTIVE" && r.Status != "DISABLED" && r.Status != "PENDING") || len(r.RoleIDs) != 1 || (r.RoleIDs[0] != AgoraRole && r.RoleIDs[0] != CustomerRole) || len(strings.TrimSpace(r.Reason)) < 2 || len(r.Reason) > 1000 {
		return User{}, problem(400, "INVALID_USER_UPDATE", "请选择 Agora 人员或客户运维中的一个角色，并填写状态及变更依据")
	}
	context, cancel := ctx()
	defer cancel()
	tx, err := s.db.BeginTx(context, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(context, "SELECT pg_advisory_xact_lock(716017,2)"); err != nil {
		return User{}, err
	}
	current, err := loadUser(context, tx, uid)
	if err != nil {
		return User{}, err
	}
	if current.Version != r.ExpectedVersion {
		return User{}, problem(409, "USER_VERSION_CONFLICT", "账号已变化，请刷新")
	}
	seen := map[string]bool{}
	for _, role := range r.RoleIDs {
		if seen[role] {
			return User{}, problem(400, "DUPLICATE_ROLE", "角色不能重复")
		}
		seen[role] = true
		var exists bool
		if err = tx.QueryRowContext(context, "SELECT EXISTS(SELECT 1 FROM avops.roles WHERE role_id=$1)", role).Scan(&exists); err != nil {
			return User{}, err
		}
		if !exists {
			return User{}, problem(400, "ROLE_NOT_FOUND", "角色不存在")
		}
	}
	if strings.HasPrefix(actor, "user:") {
		grantor, e := loadUser(context, tx, strings.TrimPrefix(actor, "user:"))
		if e != nil {
			return User{}, e
		}
		for _, role := range r.RoleIDs {
			rows, e := tx.QueryContext(context, "SELECT permission_code FROM avops.role_permissions WHERE role_id=$1", role)
			if e != nil {
				return User{}, e
			}
			for rows.Next() {
				var p string
				if e = rows.Scan(&p); e != nil {
					rows.Close()
					return User{}, e
				}
				if !Has(grantor, p) {
					rows.Close()
					return User{}, problem(403, "GRANT_EXCEEDS_PERMISSION", "不能授予超出自身范围的权限")
				}
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return User{}, e
			}
		}
	}
	if _, err = tx.ExecContext(context, "UPDATE avops.users SET status=$1,version=version+1,updated_at=now() WHERE user_id=$2", r.Status, uid); err != nil {
		return User{}, err
	}
	if _, err = tx.ExecContext(context, "DELETE FROM avops.user_roles WHERE user_id=$1", uid); err != nil {
		return User{}, err
	}
	for _, role := range r.RoleIDs {
		if _, err = tx.ExecContext(context, "INSERT INTO avops.user_roles(user_id,role_id) VALUES($1,$2)", uid, role); err != nil {
			return User{}, err
		}
	}
	if err = ensureAdmin(tx, context); err != nil {
		return User{}, err
	}
	// Role changes invalidate all existing sessions; new sessions load fresh grants.
	if _, err = tx.ExecContext(context, "UPDATE avops.sessions SET revoked=true WHERE user_id=$1", uid); err != nil {
		return User{}, err
	}
	if err = audit(context, tx, actor, ip, "USER_UPDATE", uid, "SUCCESS", map[string]any{"status": r.Status, "role_ids": r.RoleIDs, "reason": r.Reason}); err != nil {
		return User{}, err
	}
	if err = tx.Commit(); err != nil {
		return User{}, err
	}
	return s.User(uid)
}

type RoleUpdate struct {
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Permissions     []string `json:"permissions"`
	ExpectedVersion int      `json:"expected_version"`
	Reason          string   `json:"reason"`
}

func (s *Service) SaveRole(roleID string, r RoleUpdate, actor, ip string) (Role, error) {
	return Role{}, problem(409, "FIXED_ROLES", "角色固定为 Agora 人员和客户运维，不支持新增或修改角色权限")
}
func (s *Service) AuditList(action, actor string, limit, offset int) (any, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, problem(400, "INVALID_PAGE", "分页参数无效")
	}
	context, cancel := ctx()
	defer cancel()
	var total int
	where := " WHERE ($1='' OR action=$1) AND ($2='' OR actor=$2)"
	if err := s.db.QueryRowContext(context, "SELECT count(*) FROM avops.system_audit"+where, action, actor).Scan(&total); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(context, "SELECT jsonb_build_object('audit_id',id,'at',at,'actor',actor,'source_ip',source_ip,'action',action,'target',target,'outcome',outcome,'details',details) FROM avops.system_audit"+where+" ORDER BY id DESC LIMIT $3 OFFSET $4", action, actor, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []json.RawMessage{}
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return map[string]any{"total": total, "records": result}, rows.Err()
}
