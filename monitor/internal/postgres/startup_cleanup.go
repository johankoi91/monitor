package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/johankoi91/monitor/runtime/internal/model"
)

func (d *DB) PurgeStartupSnapshots() error {
	snapshots, err := d.LoadSnapshots()
	if err != nil {
		return err
	}
	for id, b := range snapshots {
		var report model.Report
		if err = json.Unmarshal(b, &report); err != nil {
			return err
		}
		changed := false
		for i := range report.Containers {
			o := &report.Containers[i]
			if o.Startup != nil {
				changed = true
				o.StartupValid = !o.Startup.Incomplete && !o.Startup.Truncated
			}
			o.Startup = nil
		}
		if !changed {
			continue
		}
		b, err = json.Marshal(report)
		if err != nil {
			return err
		}
		if err = d.SaveSnapshot(id, b); err != nil {
			return err
		}
	}
	return d.transaction(func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE avops.containers SET payload=payload-'startup' WHERE payload ? 'startup'")
		return err
	})
}
