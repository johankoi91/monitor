package postgres

import (
	"context"
	"database/sql"
)

// Run with the owner while the center is stopped. Credentials and their
// enabled/owner state survive; only obsolete node restrictions are removed.
func (d *DB) RemoveKeyNodeScopes() error {
	return d.transaction(func(ctx context.Context, tx *sql.Tx) error {
		for _, q := range []string{
			"UPDATE avops.access_keys SET payload=payload-'allowed_node_ids' WHERE payload ? 'allowed_node_ids'",
			"UPDATE avops.access_audit SET payload=jsonb_set(payload,'{key}',(payload->'key')-'allowed_node_ids') WHERE jsonb_typeof(payload->'key')='object' AND (payload->'key') ? 'allowed_node_ids'",
			"DROP TABLE IF EXISTS avops.key_node_scopes",
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	})
}
