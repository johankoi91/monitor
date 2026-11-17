package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/johankoi91/monitor/rtc-probe/hub"
	"github.com/johankoi91/monitor/rtc-probe/protocol"
)

// RTCInit verifies migrations using the restricted runtime account.
func (d *DB) RTCInit() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var ready bool
	err := d.Pool.QueryRowContext(ctx, `SELECT to_regclass('avops.rtc_probe_runs') IS NOT NULL AND to_regclass('avops.rtc_probe_logs') IS NOT NULL`).Scan(&ready)
	if err != nil {
		return err
	}
	if !ready {
		return errors.New("RTC history schema migration required; run dbmigrate with database owner")
	}
	return nil
}
func (d *DB) RTCPutRun(v hub.Run, max int64) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return d.transaction(func(ctx context.Context, tx *sql.Tx) error {
		var total, old int64
		if err := tx.QueryRowContext(ctx, "SELECT COALESCE(sum(payload_bytes),0) FROM avops.rtc_probe_runs").Scan(&total); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, "SELECT payload_bytes FROM avops.rtc_probe_runs WHERE run_id=$1", v.ID).Scan(&old); err != nil && err != sql.ErrNoRows {
			return err
		}
		if total-old+int64(len(data)) > max {
			return errors.New("RTC 历史容量达到上限")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO avops.rtc_probe_runs(run_id,task_id,target_id,driver,created_at,finished_at,payload,payload_bytes) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(run_id) DO UPDATE SET finished_at=excluded.finished_at,payload=excluded.payload,payload_bytes=excluded.payload_bytes`, v.ID, v.TaskID, v.TargetID, v.Driver, v.CreatedAt, v.FinishedAt, string(data), len(data)); err != nil {
			return err
		}
		for id, item := range v.Logs {
			b, _ := json.Marshal(item)
			if _, err := tx.ExecContext(ctx, "INSERT INTO avops.rtc_probe_logs(request_id,run_id,payload) VALUES($1,$2,$3) ON CONFLICT(request_id) DO UPDATE SET payload=excluded.payload", id, v.ID, string(b)); err != nil {
				return err
			}
		}
		return nil
	})
}
func (d *DB) RTCGetRun(id string) (hub.Run, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var b []byte
	var r hub.Run
	err := d.Pool.QueryRowContext(ctx, "SELECT payload FROM avops.rtc_probe_runs WHERE run_id=$1", id).Scan(&b)
	if err == nil {
		err = json.Unmarshal(b, &r)
	}
	return r, err
}
func (d *DB) RTCGetLog(id string) (protocol.LogRequest, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out protocol.LogRequest
	var b []byte
	err := d.Pool.QueryRowContext(ctx, "SELECT payload FROM avops.rtc_probe_logs WHERE request_id=$1", id).Scan(&b)
	if err == nil {
		err = json.Unmarshal(b, &out)
	}
	return out, err
}
func (d *DB) RTCListRuns(task string, limit, offset int) (hub.HistoryPage, error) {
	out := hub.HistoryPage{Runs: []hub.Run{}, Limit: limit, Offset: offset}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Pool.QueryRowContext(ctx, "SELECT count(*) FROM avops.rtc_probe_runs WHERE COALESCE(payload->>'parent_run_id','')='' AND ($1='' OR task_id=$1)", task).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := d.Pool.QueryContext(ctx, "SELECT payload FROM avops.rtc_probe_runs WHERE COALESCE(payload->>'parent_run_id','')='' AND ($1='' OR task_id=$1) ORDER BY created_at DESC,run_id LIMIT $2 OFFSET $3", task, limit, offset)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var b []byte
		var r hub.Run
		if err = rows.Scan(&b); err != nil {
			return out, err
		}
		if err = json.Unmarshal(b, &r); err != nil {
			return out, err
		}
		out.Runs = append(out.Runs, r)
	}
	return out, rows.Err()
}
func (d *DB) RTCUsage() (hub.HistoryUsage, error) {
	var out hub.HistoryUsage
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := d.Pool.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM avops.rtc_probe_runs WHERE COALESCE(payload->>'parent_run_id','')=''),COALESCE(sum(payload_bytes),0) FROM avops.rtc_probe_runs`).Scan(&out.Runs, &out.Bytes)
	return out, err
}
func (d *DB) RTCCleanup(cutoff time.Time, batch int) error {
	return d.transaction(func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "DELETE FROM avops.rtc_probe_runs WHERE run_id IN (SELECT c.run_id FROM avops.rtc_probe_runs c WHERE c.finished_at<$1 AND NOT EXISTS (SELECT 1 FROM avops.rtc_probe_runs p WHERE p.run_id=c.payload->>'parent_run_id' AND (p.finished_at IS NULL OR p.finished_at>=$1)) ORDER BY c.finished_at LIMIT $2)", cutoff, batch)
		return err
	})
}
