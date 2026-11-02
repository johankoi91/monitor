CREATE TABLE IF NOT EXISTS avops.resource_samples(
 node_id text NOT NULL,
 bucket_at timestamptz NOT NULL,
 container_id text NOT NULL,
 started_at text NOT NULL,
 sampled_at timestamptz NOT NULL,
 payload jsonb NOT NULL,
 PRIMARY KEY(node_id,bucket_at,container_id,started_at)
);
CREATE INDEX IF NOT EXISTS resource_samples_time ON avops.resource_samples(sampled_at);
