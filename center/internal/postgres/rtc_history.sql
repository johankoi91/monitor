CREATE TABLE IF NOT EXISTS avops.probe_runs_v2(run_id text PRIMARY KEY,task_id text NOT NULL,target_id text NOT NULL,driver text NOT NULL,created_at timestamptz NOT NULL,finished_at timestamptz,payload jsonb NOT NULL,payload_bytes bigint NOT NULL);
CREATE INDEX IF NOT EXISTS probe_runs_v2_task_time ON avops.probe_runs_v2(task_id,created_at DESC,run_id);
CREATE INDEX IF NOT EXISTS probe_runs_v2_time ON avops.probe_runs_v2(created_at DESC,run_id);
-- History lists parent rounds only. Keep that predicate in a small ordered
-- index so counting/paging never scans and detoasts every SDK event payload.
CREATE INDEX IF NOT EXISTS probe_runs_v2_public_time ON avops.probe_runs_v2(created_at DESC,run_id) INCLUDE(task_id,driver) WHERE COALESCE(payload->>'parent_run_id','')='';
CREATE INDEX IF NOT EXISTS probe_runs_v2_retention ON avops.probe_runs_v2(finished_at);
CREATE TABLE IF NOT EXISTS avops.probe_logs_v2(request_id text PRIMARY KEY,run_id text NOT NULL REFERENCES avops.probe_runs_v2(run_id) ON DELETE CASCADE,payload jsonb NOT NULL);
