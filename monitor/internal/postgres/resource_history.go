package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/johankoi91/monitor/runtime/internal/history"
)

func (d *DB) SaveResources(ctx context.Context, samples []history.Sample) error {
	// Use the fenced center writer, but keep the telemetry transaction short.
	return d.transact(3*time.Second, func(txctx context.Context, tx *sql.Tx) error {
		for _, s := range samples {
			body, err := json.Marshal(s)
			if err != nil {
				return err
			}
			if _, err = tx.ExecContext(txctx, `INSERT INTO avops.resource_samples(node_id,bucket_at,container_id,started_at,sampled_at,payload)
    VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(node_id,bucket_at,container_id,started_at)
    DO UPDATE SET sampled_at=excluded.sampled_at,payload=excluded.payload WHERE excluded.sampled_at>avops.resource_samples.sampled_at`, s.NodeID, s.At.Truncate(history.Interval), s.ContainerID, s.StartedAt, s.At, string(body)); err != nil {
				return err
			}
		}
		return nil
	})
}
func (d *DB) ResourceSamples(ctx context.Context, id string, from, to time.Time, step time.Duration) ([]history.Sample, error) {
	queryctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := d.Pool.QueryContext(queryctx, `SELECT payload FROM (
  SELECT DISTINCT ON (floor(extract(epoch FROM sampled_at)/$4::integer),container_id,started_at) payload,sampled_at
  FROM avops.resource_samples WHERE node_id=$1 AND bucket_at>=$2::timestamptz-interval '30 seconds' AND sampled_at>=$2 AND sampled_at<=$3
  ORDER BY floor(extract(epoch FROM sampled_at)/$4::integer),container_id,started_at,sampled_at DESC
 ) samples ORDER BY sampled_at LIMIT 12000`, id, from, to, int(step/time.Second))
	if err != nil {
		return nil, errors.New("历史指标查询暂不可用")
	}
	defer rows.Close()
	result := []history.Sample{}
	for rows.Next() {
		var raw []byte
		var s history.Sample
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		s.NodeID = id
		result = append(result, s)
	}
	return result, rows.Err()
}
func (d *DB) CleanupResourceHistory() error {
	return d.transact(3*time.Second, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM avops.resource_samples WHERE (node_id,bucket_at,container_id,started_at) IN
   (SELECT node_id,bucket_at,container_id,started_at FROM avops.resource_samples WHERE sampled_at<now()-interval '7 days' ORDER BY sampled_at LIMIT 10000)`)
		return err
	})
}
