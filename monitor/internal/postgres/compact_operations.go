package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/johankoi91/monitor/runtime/internal/model"
)

// Rewrite existing results and stage audits atomically; legacy WAL imports
// still decode, while new records no longer repeat command envelopes.
func (d *DB) CompactOperations() error {
	return d.transact(30e9, func(ctx context.Context, tx *sql.Tx) error {
		var done bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM avops.documents WHERE name='compact-operations-v3')").Scan(&done); err != nil {
			return err
		}
		if done {
			return nil
		}
		rows, err := tx.QueryContext(ctx, "SELECT operation_id,payload FROM avops.operations")
		if err != nil {
			return err
		}
		operations := map[string]model.Operation{}
		for rows.Next() {
			var id string
			var b []byte
			if err = rows.Scan(&id, &b); err != nil {
				rows.Close()
				return err
			}
			var o model.Operation
			if err = json.Unmarshal(b, &o); err != nil {
				rows.Close()
				return err
			}
			operations[id] = o
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for id, o := range operations {
			if _, err = tx.ExecContext(ctx, "UPDATE avops.operations SET payload=$2 WHERE operation_id=$1", id, jsonText(o)); err != nil {
				return err
			}
		}
		rows, err = tx.QueryContext(ctx, "SELECT id,payload FROM avops.operation_audit")
		if err != nil {
			return err
		}
		steps := map[int64]model.OperationStep{}
		for rows.Next() {
			var id int64
			var b []byte
			if err = rows.Scan(&id, &b); err != nil {
				rows.Close()
				return err
			}
			var r model.AuditRecord
			if err = json.Unmarshal(b, &r); err != nil {
				rows.Close()
				return err
			}
			if r.Operation.OperationID != "" {
				steps[id] = r.Step()
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for id, step := range steps {
			if _, err = tx.ExecContext(ctx, "UPDATE avops.operation_audit SET payload=$2 WHERE id=$1", id, jsonText(step)); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, "ALTER TABLE avops.operations DROP COLUMN IF EXISTS phase, DROP COLUMN IF EXISTS result_known, DROP COLUMN IF EXISTS operator"); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO avops.documents(name,payload) VALUES('compact-operations-v3','{"version":3}')`)
		return err
	})
}
