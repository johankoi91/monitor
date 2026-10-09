package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/johankoi91/monitor/runtime/internal/model"
	"time"
)

func projectBaseline(ctx context.Context, tx *sql.Tx, b []byte) error {
	var r struct {
		Baseline model.Baseline  `json:"baseline"`
		YAML     string          `json:"yaml"`
		Source   string          `json:"source"`
		Change   model.Selection `json:"change"`
	}
	if json.Unmarshal(b, &r) != nil || r.Baseline.Revision == "" || r.Baseline.Definition == nil || r.Baseline.SavedAt == nil {
		return errors.New("invalid baseline record")
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO avops.baseline_versions(revision,saved_at,source,definition,change,yaml) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(revision) DO NOTHING", r.Baseline.Revision, r.Baseline.SavedAt, r.Source, jsonText(r.Baseline.Definition), jsonText(r.Change), r.YAML); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE avops.nodes SET active=false"); err != nil {
		return err
	}
	for _, cluster := range r.Baseline.Definition.Clusters {
		for _, service := range cluster.Services {
			for _, n := range service.Nodes {
				if _, err := tx.ExecContext(ctx, "INSERT INTO avops.nodes(node_id,agent_id,container_name,cluster_code,service_code,restart_enabled,active,baseline_revision,checks) VALUES($1,$2,$3,$4,$5,$6,true,$7,$8) ON CONFLICT(node_id) DO UPDATE SET agent_id=excluded.agent_id,container_name=excluded.container_name,cluster_code=excluded.cluster_code,service_code=excluded.service_code,restart_enabled=excluded.restart_enabled,active=true,baseline_revision=excluded.baseline_revision,checks=excluded.checks", n.ID, n.AgentID, n.ContainerName, cluster.Code, service.Code, n.RestartEnabled, r.Baseline.Revision, jsonText(n.Checks)); err != nil {
					return err
				}
			}
		}
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO avops.system_audit(actor,source_ip,action,target,outcome,details) VALUES($1,'','BASELINE_SAVE',$2,'SUCCESS',$3)", r.Source, r.Baseline.Revision, jsonText(r.Change))
	return err
}
func projectOperation(ctx context.Context, tx *sql.Tx, b []byte) error {
	var r model.AuditRecord
	if json.Unmarshal(b, &r) != nil || r.At.IsZero() || r.Operation.OperationID == "" || r.Operation.RequestKey == "" {
		return errors.New("invalid operation audit")
	}
	o := r.Operation
	_, err := tx.ExecContext(ctx, "INSERT INTO avops.operations(operation_id,request_key,node_id,agent_id,status,phase,node_locked,result_known,requested_at,finished_at,authenticated_source,source_ip,operator,reason,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) ON CONFLICT(operation_id) DO UPDATE SET status=excluded.status,phase=excluded.phase,node_locked=excluded.node_locked,result_known=excluded.result_known,finished_at=excluded.finished_at,payload=excluded.payload", o.OperationID, o.RequestKey, o.NodeID, o.Task.AgentID, o.Status, o.Phase, o.NodeLocked, o.ResultKnown, o.RequestedAt, o.FinishedAt, o.AuthenticatedSource, o.SourceIP, o.Operator, o.Reason, jsonText(o))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO avops.operation_audit(operation_id,at,kind,payload) VALUES($1,$2,$3,$4)", o.OperationID, r.At, r.Kind, string(b))
	return err
}
func projectAccess(ctx context.Context, tx *sql.Tx, b []byte) error {
	var r struct {
		At      time.Time       `json:"at"`
		Kind    string          `json:"kind"`
		Source  string          `json:"source"`
		Request json.RawMessage `json:"request"`
		Key     json.RawMessage `json:"key"`
		Hash    string          `json:"secret_hash"`
	}
	if json.Unmarshal(b, &r) != nil || r.At.IsZero() {
		return errors.New("invalid key record")
	}
	if len(r.Request) > 0 && string(r.Request) != "null" {
		var q struct {
			ID          string    `json:"request_id"`
			Name        string    `json:"name"`
			Purpose     string    `json:"purpose"`
			Reason      string    `json:"reason"`
			Status      string    `json:"status"`
			RequestedAt time.Time `json:"requested_at"`
			KeyID       string    `json:"key_id"`
		}
		if json.Unmarshal(r.Request, &q) != nil || q.ID == "" {
			return errors.New("invalid key request")
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO avops.key_requests(request_id,name,purpose,reason,status,requested_at,key_id,payload) VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8) ON CONFLICT(request_id) DO UPDATE SET status=excluded.status,key_id=excluded.key_id,payload=excluded.payload", q.ID, q.Name, q.Purpose, q.Reason, q.Status, q.RequestedAt, q.KeyID, string(r.Request)); err != nil {
			return err
		}
	}
	if len(r.Key) > 0 && string(r.Key) != "null" {
		var k struct {
			ID        string    `json:"key_id"`
			Name      string    `json:"name"`
			Purpose   string    `json:"purpose"`
			Enabled   bool      `json:"enabled"`
			CreatedAt time.Time `json:"created_at"`
			OwnerID   string    `json:"owner_id"`
			Nodes     []string  `json:"allowed_node_ids"`
		}
		if json.Unmarshal(r.Key, &k) != nil || k.ID == "" {
			return errors.New("invalid key")
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO avops.access_keys(key_id,name,purpose,secret_hash,enabled,created_at,owner_id,payload) VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8) ON CONFLICT(key_id) DO UPDATE SET enabled=excluded.enabled,payload=excluded.payload,owner_id=excluded.owner_id,secret_hash=COALESCE(NULLIF(excluded.secret_hash,''),avops.access_keys.secret_hash)", k.ID, k.Name, k.Purpose, r.Hash, k.Enabled, k.CreatedAt, k.OwnerID, string(r.Key)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM avops.key_node_scopes WHERE key_id=$1", k.ID); err != nil {
			return err
		}
		for _, n := range k.Nodes {
			if _, err := tx.ExecContext(ctx, "INSERT INTO avops.key_node_scopes(key_id,node_id) VALUES($1,$2)", k.ID, n); err != nil {
				return err
			}
		}
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO avops.access_audit(at,kind,source,payload) VALUES($1,$2,$3,$4)", r.At, r.Kind, r.Source, string(b))
	return err
}
func projectNotifications(ctx context.Context, tx *sql.Tx, b []byte) error {
	var r struct {
		Changes     []json.RawMessage `json:"changes"`
		Removed     []string          `json:"removed"`
		Deliveries  []json.RawMessage `json:"deliveries"`
		Revision    string            `json:"revision"`
		RevisionSet bool              `json:"revision_set"`
		Dropped     int               `json:"dropped"`
	}
	if json.Unmarshal(b, &r) != nil {
		return errors.New("invalid notification record")
	}
	for _, raw := range r.Changes {
		var f struct {
			Key string `json:"key"`
		}
		if json.Unmarshal(raw, &f) != nil || f.Key == "" {
			return errors.New("invalid notification fact")
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO avops.notification_facts(fact_key,payload) VALUES($1,$2) ON CONFLICT(fact_key) DO UPDATE SET payload=excluded.payload", f.Key, string(raw)); err != nil {
			return err
		}
	}
	for _, key := range r.Removed {
		if _, err := tx.ExecContext(ctx, "DELETE FROM avops.notification_facts WHERE fact_key=$1", key); err != nil {
			return err
		}
	}
	for _, raw := range r.Deliveries {
		var d struct {
			Event            model.Notification `json:"event"`
			Status           string             `json:"status"`
			Attempts         int                `json:"attempts"`
			NextAt           time.Time          `json:"next_at"`
			SettingsRevision string             `json:"settings_revision"`
		}
		if json.Unmarshal(raw, &d) != nil || d.Event.ID == "" {
			return errors.New("invalid notification delivery")
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO avops.notification_events(event_id,occurred_at,node_id,service_code,status,attempts,next_at,settings_revision,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(event_id) DO UPDATE SET status=excluded.status,attempts=excluded.attempts,next_at=excluded.next_at,payload=excluded.payload", d.Event.ID, d.Event.OccurredAt, d.Event.Node, d.Event.Service, d.Status, d.Attempts, d.NextAt, d.SettingsRevision, string(raw)); err != nil {
			return err
		}
	}
	if r.RevisionSet {
		if _, err := tx.ExecContext(ctx, "UPDATE avops.notification_state SET revision=$1 WHERE id=true", r.Revision); err != nil {
			return err
		}
	}
	if r.Dropped != 0 {
		if _, err := tx.ExecContext(ctx, "UPDATE avops.notification_state SET dropped=dropped+$1 WHERE id=true", r.Dropped); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO avops.notification_audit(payload) VALUES($1)", string(b))
	return err
}
func projectLog(ctx context.Context, tx *sql.Tx, name string, b []byte) error {
	switch name {
	case "operations":
		return projectOperation(ctx, tx, b)
	case "access":
		return projectAccess(ctx, tx, b)
	case "notifications":
		return projectNotifications(ctx, tx, b)
	}
	return errors.New("unknown PostgreSQL log")
}
func projectSnapshot(ctx context.Context, tx *sql.Tx, id string, b []byte) error {
	var report model.Report
	if json.Unmarshal(b, &report) != nil || !report.Complete {
		return errors.New("invalid agent snapshot")
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO avops.agent_snapshots(agent_id,collected_at,payload) VALUES($1,$2,$3) ON CONFLICT(agent_id) DO UPDATE SET collected_at=excluded.collected_at,received_at=now(),payload=excluded.payload", id, report.CollectedAt, string(b)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM avops.containers WHERE agent_id=$1", id); err != nil {
		return err
	}
	for _, o := range report.Containers {
		c := o.Container
		if _, err := tx.ExecContext(ctx, "INSERT INTO avops.containers(agent_id,container_id,container_name,runtime_status,image,collected_at,payload) VALUES($1,$2,$3,$4,$5,$6,$7)", id, c.ContainerID, c.ContainerName, c.RuntimeStatus, c.Image, c.CollectedAt, jsonText(o)); err != nil {
			return err
		}
	}
	return nil
}
