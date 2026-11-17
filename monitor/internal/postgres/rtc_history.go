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
	err := d.Pool.QueryRowContext(ctx, `SELECT to_regclass('avops.rtc_probe_runs') IS NOT NULL AND to_regclass('avops.rtc_probe_audit') IS NOT NULL AND to_regclass('avops.rtc_probe_logs') IS NOT NULL`).Scan(&ready)
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
		if err := tx.QueryRowContext(ctx, "SELECT COALESCE(sum(payload_bytes),0)+(SELECT COALESCE(sum(pg_column_size(payload)),0) FROM avops.rtc_probe_audit) FROM avops.rtc_probe_runs").Scan(&total); err != nil {
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
func (d *DB) RTCPutAudit(v hub.Audit, max int64) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return d.transaction(func(ctx context.Context, tx *sql.Tx) error {
		if err := checkRTCAuditBudget(ctx, tx, []hub.Audit{v}, max); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO avops.rtc_probe_audit(audit_id,at,payload) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", v.ID, v.At, string(b))
		return err
	})
}
func (d *DB) RTCUsage() (hub.HistoryUsage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out hub.HistoryUsage
	err := d.Pool.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM avops.rtc_probe_runs WHERE COALESCE(payload->>'parent_run_id','')=''),(SELECT count(*) FROM avops.rtc_probe_audit),(SELECT COALESCE(sum(payload_bytes),0) FROM avops.rtc_probe_runs)+COALESCE((SELECT sum(pg_column_size(payload)) FROM avops.rtc_probe_audit),0)`).Scan(&out.Runs, &out.Audits, &out.Bytes)
	return out, err
}
func (d *DB) RTCCleanup(runCutoff, auditCutoff time.Time, batch int) error {
	return d.transaction(func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM avops.rtc_probe_runs WHERE run_id IN (SELECT c.run_id FROM avops.rtc_probe_runs c WHERE c.finished_at<$1 AND NOT EXISTS (SELECT 1 FROM avops.rtc_probe_runs p WHERE p.run_id=c.payload->>'parent_run_id' AND (p.finished_at IS NULL OR p.finished_at>=$1)) ORDER BY c.finished_at LIMIT $2)", runCutoff, batch); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM avops.rtc_probe_audit WHERE audit_id IN (SELECT audit_id FROM avops.rtc_probe_audit WHERE at<$1 ORDER BY at LIMIT $2)", auditCutoff, batch)
		return err
	})
}

// Commit the health transition audit with the encrypted control state before ACK.
func (d *DB) RTCCommitState(state []byte, audits []hub.Audit) error {
	var config struct {
		Settings struct {
			HistoryMB int `json:"history_mb"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(state, &config); err != nil {
		return err
	}
	return d.transaction(func(ctx context.Context, tx *sql.Tx) error {
		if len(audits) > 0 {
			if err := checkRTCAuditBudget(ctx, tx, audits, int64(config.Settings.HistoryMB)<<20); err != nil {
				return err
			}
		}
		if err := d.saveDocument(ctx, tx, "rtc-probe/state-v1", state); err != nil {
			return err
		}
		for _, audit := range audits {
			data, err := json.Marshal(audit)
			if err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO avops.rtc_probe_audit(audit_id,at,payload) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", audit.ID, audit.At, string(data)); err != nil {
				return err
			}
		}
		return nil
	})
}
func (d *DB) RTCListAudits(task string, limit, offset int) (hub.AuditPage, error) {
	page := hub.AuditPage{Audits: []hub.Audit{}, Limit: limit, Offset: offset}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Pool.QueryRowContext(ctx, "SELECT count(*) FROM avops.rtc_probe_audit WHERE ($1='' OR payload->>'task_id'=$1)", task).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := d.Pool.QueryContext(ctx, "SELECT payload FROM avops.rtc_probe_audit WHERE ($1='' OR payload->>'task_id'=$1) ORDER BY at DESC,audit_id LIMIT $2 OFFSET $3", task, limit, offset)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var b []byte
		var a hub.Audit
		if err := rows.Scan(&b); err != nil {
			return page, err
		}
		if err := json.Unmarshal(b, &a); err != nil {
			return page, err
		}
		page.Audits = append(page.Audits, a)
	}
	return page, rows.Err()
}

func checkRTCAuditBudget(ctx context.Context, tx *sql.Tx, audits []hub.Audit, max int64) error {
	var total int64
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT COALESCE(sum(payload_bytes),0) FROM avops.rtc_probe_runs)+(SELECT COALESCE(sum(pg_column_size(payload)),0) FROM avops.rtc_probe_audit)`).Scan(&total); err != nil {
		return err
	}
	for _, a := range audits {
		var exists bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM avops.rtc_probe_audit WHERE audit_id=$1)", a.ID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			b, err := json.Marshal(a)
			if err != nil {
				return err
			}
			total += int64(len(b))
		}
	}
	if total > max {
		return errors.New("RTC history/audit capacity exhausted")
	}
	return nil
}
