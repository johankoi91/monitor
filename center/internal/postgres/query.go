package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

func (d *DB) Operations(status, node, source string, limit, offset int) (any, error) {
	if limit < 1 || limit > 200 || offset < 0 {
		return nil, errors.New("invalid query page")
	}
	ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
	defer c()
	var total int
	where := " WHERE ($1='' OR status=$1) AND ($2='' OR node_id=$2) AND ($3='' OR authenticated_source=$3)"
	if err := d.Pool.QueryRowContext(ctx, "SELECT count(*) FROM avops.operations"+where, status, node, source).Scan(&total); err != nil {
		return nil, err
	}
	rows, err := d.Pool.QueryContext(ctx, "SELECT payload FROM avops.operations"+where+" ORDER BY requested_at DESC,operation_id LIMIT $4 OFFSET $5", status, node, source, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ops := []json.RawMessage{}
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		ops = append(ops, b)
	}
	return map[string]any{"operations": ops, "total": total, "limit": limit, "offset": offset}, rows.Err()
}
func (d *DB) Baselines(limit, offset int) (any, error) {
	if limit < 1 || limit > 200 || offset < 0 {
		return nil, errors.New("invalid query page")
	}
	ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
	defer c()
	var total int
	if err := d.Pool.QueryRowContext(ctx, "SELECT count(*) FROM avops.baseline_versions").Scan(&total); err != nil {
		return nil, err
	}
	rows, err := d.Pool.QueryContext(ctx, "SELECT jsonb_build_object('revision',revision,'saved_at',saved_at,'source',source,'change',change,'definition',definition) FROM avops.baseline_versions ORDER BY saved_at DESC LIMIT $1 OFFSET $2", limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []json.RawMessage{}
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return map[string]any{"versions": result, "total": total}, rows.Err()
}
