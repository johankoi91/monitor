CREATE TABLE IF NOT EXISTS avops.rtc_probe_runs(run_id text PRIMARY KEY,task_id text NOT NULL,target_id text NOT NULL,driver text NOT NULL,created_at timestamptz NOT NULL,finished_at timestamptz,payload jsonb NOT NULL,payload_bytes bigint NOT NULL);
CREATE INDEX IF NOT EXISTS rtc_probe_runs_task_time ON avops.rtc_probe_runs(task_id,created_at DESC,run_id);
CREATE INDEX IF NOT EXISTS rtc_probe_runs_time ON avops.rtc_probe_runs(created_at DESC,run_id);
CREATE INDEX IF NOT EXISTS rtc_probe_runs_retention ON avops.rtc_probe_runs(finished_at);
CREATE TABLE IF NOT EXISTS avops.rtc_probe_logs(request_id text PRIMARY KEY,run_id text NOT NULL REFERENCES avops.rtc_probe_runs(run_id) ON DELETE CASCADE,payload jsonb NOT NULL);
-- Retire the RTC state/operation audit data from existing installations.
DROP TABLE IF EXISTS avops.rtc_probe_audit;
DELETE FROM avops.system_audit WHERE target LIKE '/api/v1/rtc/%';
