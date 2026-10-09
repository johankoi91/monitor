package identity

import (
	"context"
	"database/sql"
	"encoding/json"
)

// Role policy migration runs inside Bootstrap's transaction. The snapshot of
// previous assignments remains in the audit log; existing account IDs survive.
func fixedRoles(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(716017,2)"); err != nil {
		return err
	}
	roles := []Role{
		{ID: AgoraRole, Name: "Agora 人员", Description: "全部模块；负责台账、受控重启、审计、密钥和账号授权"},
		{ID: CustomerRole, Name: "客户运维", Description: "查看状态与资源、配置通知、使用独立 Key 受控重启并查看自己的结果"},
	}
	for _, r := range roles {
		if _, err := tx.ExecContext(ctx, "INSERT INTO avops.roles(role_id,name,description,builtin) VALUES($1,$2,$3,true) ON CONFLICT(role_id) DO UPDATE SET name=excluded.name,description=excluded.description,builtin=true", r.ID, r.Name, r.Description); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM avops.role_permissions WHERE role_id=$1", r.ID); err != nil {
			return err
		}
		for _, p := range RolePermissions(r.ID) {
			if _, err := tx.ExecContext(ctx, "INSERT INTO avops.role_permissions(role_id,permission_code) VALUES($1,$2)", r.ID, p); err != nil {
				return err
			}
		}
	}
	var migrated bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM avops.documents WHERE name='identity-two-roles-v1')").Scan(&migrated); err != nil {
		return err
	}
	if migrated {
		var updated bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM avops.documents WHERE name='customer-restart-v2')").Scan(&updated); err != nil {
			return err
		}
		if updated {
			return nil
		}
		if _, err := tx.ExecContext(ctx, "UPDATE avops.sessions SET revoked=true WHERE user_id IN (SELECT user_id FROM avops.user_roles WHERE role_id='customer_ops') AND NOT revoked"); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO avops.documents(name,payload) VALUES('customer-restart-v2','{"version":2}')`); err != nil {
			return err
		}
		return audit(ctx, tx, "bootstrap", "", "ROLE_POLICY_MIGRATION", "customer_ops", "SUCCESS", map[string]any{"added": []string{"restart.execute", "restart.result"}, "key_required": true})
	}
	var previous []byte
	if err := tx.QueryRowContext(ctx, `SELECT jsonb_build_object(
		'roles',COALESCE((SELECT jsonb_agg(to_jsonb(r)) FROM avops.roles r),'[]'),
		'permissions',COALESCE((SELECT jsonb_agg(to_jsonb(rp)) FROM avops.role_permissions rp),'[]'),
		'assignments',COALESCE((SELECT jsonb_agg(to_jsonb(ur)) FROM avops.user_roles ur),'[]'))`).Scan(&previous); err != nil {
		return err
	}
	// Only previous administrators migrate to Agora; all other accounts default
	// to customer operations and can be reassigned after identity verification.
	if _, err := tx.ExecContext(ctx, `INSERT INTO avops.user_roles(user_id,role_id)
		SELECT u.user_id,CASE WHEN EXISTS(SELECT 1 FROM avops.user_roles ur WHERE ur.user_id=u.user_id AND ur.role_id IN ('admin','agora')) THEN 'agora' ELSE 'customer_ops' END FROM avops.users u ON CONFLICT DO NOTHING`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM avops.user_roles ur WHERE role_id NOT IN ('agora','customer_ops') OR (role_id='customer_ops' AND EXISTS(SELECT 1 FROM avops.user_roles a WHERE a.user_id=ur.user_id AND a.role_id='agora'))`); err != nil {
		return err
	}
	for _, statement := range []string{
		"DELETE FROM avops.role_permissions WHERE role_id NOT IN ('agora','customer_ops')",
		"DELETE FROM avops.roles WHERE role_id NOT IN ('agora','customer_ops')",
		"UPDATE avops.sessions SET revoked=true WHERE NOT revoked",
		"UPDATE avops.users SET version=version+1,updated_at=now()",
		`INSERT INTO avops.documents(name,payload) VALUES('identity-two-roles-v1','{"version":1}')`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return audit(ctx, tx, "bootstrap", "", "ROLE_POLICY_MIGRATION", "agora/customer_ops", "SUCCESS", json.RawMessage(previous))
}
